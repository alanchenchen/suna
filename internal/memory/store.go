package memory

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alanchenchen/suna/internal/logging"
	_ "modernc.org/sqlite"
)

// autoVacuumFull 对应 SQLite 的 PRAGMA auto_vacuum=FULL。
const autoVacuumFull = 1

type Store struct {
	db *sql.DB
}

func NewStore(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+dsnParams)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	// 老库在 DSN 里声明 auto_vacuum 不会生效，必须 VACUUM 重建一次才能切换页回收模式。
	if err := s.enableAutoVacuum(); err != nil {
		// 空间回收属于可选维护：磁盘不足等失败只保留诊断，不能阻止 daemon 启动。
		logging.Error("memory", "auto_vacuum_enable_failed", err, nil)
	}
	return s, nil
}

// dsnParams 集中声明连接级 pragma。auto_vacuum(FULL) 只在建库时生效，
// 之后由 SQLite 在每次提交时自行把空闲页归还文件，无需再手动 VACUUM。
const dsnParams = "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=auto_vacuum(1)"

// enableAutoVacuum 把老库一次性切换到 auto_vacuum=FULL。
// 已切换的库直接返回，新库在 DSN 建库时就是 FULL，因此这里只对升级用户执行一次。
func (s *Store) enableAutoVacuum() error {
	var mode int
	if err := s.db.QueryRow(`PRAGMA auto_vacuum`).Scan(&mode); err != nil {
		return fmt.Errorf("read auto_vacuum: %w", err)
	}
	if mode == autoVacuumFull {
		return nil
	}
	// VACUUM 需要临时空间约等于库大小，且必须独占；调用点在建 daemon 之前，没有并发连接。
	if _, err := s.db.Exec(`VACUUM`); err != nil {
		return fmt.Errorf("vacuum to enable auto_vacuum: %w", err)
	}
	return nil
}

func (s *Store) DB() *sql.DB {
	return s.db
}

// MaterializePendingMemoryQueueModelRef 为早于 model_ref 持久化的遗留 pending 队列项写入已校验的默认模型。
// Store 不依赖配置或路由；调用方必须先校验 modelRef。
func (s *Store) MaterializePendingMemoryQueueModelRef(ctx context.Context, modelRef string) (int, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("materialize pending memory queue model_ref: nil store")
	}
	modelRef = strings.TrimSpace(modelRef)
	if modelRef == "" {
		return 0, fmt.Errorf("materialize pending memory queue model_ref: model_ref is required")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE memory_queue
		SET model_ref = ?
		WHERE processed_at IS NULL
		  AND (model_ref IS NULL OR TRIM(model_ref, CHAR(9) || CHAR(10) || CHAR(13) || ' ') = '')`, modelRef)
	if err != nil {
		return 0, fmt.Errorf("materialize pending memory queue model_ref: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("materialize pending memory queue model_ref rows affected: %w", err)
	}
	return int(updated), nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	if err := s.checkUpgradeBaseline(); err != nil {
		return err
	}
	migrations := []string{
		// ── User profile memory tables ──

		`CREATE TABLE IF NOT EXISTS user_profile_memory (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			content TEXT NOT NULL,
			tags TEXT NOT NULL DEFAULT '[]',
			source TEXT NOT NULL DEFAULT 'inferred',
			confidence REAL NOT NULL DEFAULT 0.7,
			priority INTEGER NOT NULL DEFAULT 50,
			is_core INTEGER NOT NULL DEFAULT 0,
			use_count INTEGER NOT NULL DEFAULT 0,
			last_used_at DATETIME,
			evidence TEXT NOT NULL DEFAULT '',
			expires_at DATETIME,
			created_at DATETIME NOT NULL DEFAULT (` + dbTimeDefault + `),
			updated_at DATETIME NOT NULL DEFAULT (` + dbTimeDefault + `)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_user_profile_memory_core ON user_profile_memory(user_id, is_core, priority)`,
		`CREATE INDEX IF NOT EXISTS idx_user_profile_memory_kind ON user_profile_memory(user_id, kind)`,
		`CREATE INDEX IF NOT EXISTS idx_user_profile_memory_updated ON user_profile_memory(user_id, updated_at)`,

		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL DEFAULT '',
			cwd TEXT NOT NULL DEFAULT '',
			model_ref TEXT,
			message_count INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			last_attached_at DATETIME
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_updated ON sessions(updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_cwd ON sessions(cwd, updated_at)`,

		`CREATE TABLE IF NOT EXISTS session_state (
			session_id TEXT PRIMARY KEY,
			summary TEXT NOT NULL DEFAULT '',
			messages TEXT NOT NULL DEFAULT '[]',
			updated_at DATETIME NOT NULL,
			FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
		)`,

		`CREATE TABLE IF NOT EXISTS memory_queue (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			model_ref TEXT NOT NULL DEFAULT '',
			kind TEXT NOT NULL,
			content TEXT NOT NULL,
			tags TEXT NOT NULL DEFAULT '[]',
			source TEXT NOT NULL DEFAULT 'inferred',
			confidence REAL NOT NULL DEFAULT 0.7,
			evidence TEXT NOT NULL DEFAULT '',
			significance TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT (` + dbTimeDefault + `),
			next_attempt_at DATETIME NOT NULL DEFAULT (` + dbTimeDefault + `),
			attempts INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT '',
			processed_at DATETIME
		)`,
		`CREATE INDEX IF NOT EXISTS idx_memory_queue_pending ON memory_queue(processed_at, next_attempt_at, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_memory_queue_user ON memory_queue(user_id, processed_at)`,

		// ── 运维表 ──

		// Token 用量日志
		`CREATE TABLE IF NOT EXISTS usage_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT (` + dbTimeDefault + `)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_usage_log_created_at ON usage_log(created_at)`,

		// Guard 审计日志
		`CREATE TABLE IF NOT EXISTS audit_log (
			id TEXT PRIMARY KEY,
			timestamp DATETIME NOT NULL DEFAULT (` + dbTimeDefault + `),
			session_id TEXT NOT NULL DEFAULT '',
			tool TEXT NOT NULL,
			params TEXT NOT NULL DEFAULT '{}',
			guard_decision TEXT NOT NULL DEFAULT '',
			guard_reason TEXT NOT NULL DEFAULT '',
			result TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_log_timestamp ON audit_log(timestamp)`,

		// ── Phase 3+ 表（预留）──

		`CREATE TABLE IF NOT EXISTS failure_records (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			pattern TEXT NOT NULL,
			operation TEXT NOT NULL,
			reason TEXT NOT NULL,
			context TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT (` + dbTimeDefault + `)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_failure_pattern ON failure_records(pattern)`,

		`CREATE TABLE IF NOT EXISTS trust_rules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			pattern TEXT NOT NULL,
			tool TEXT NOT NULL DEFAULT '',
			reason TEXT NOT NULL DEFAULT '',
			learned_from TEXT NOT NULL DEFAULT ''
		)`,

		`CREATE TABLE IF NOT EXISTS triggers (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			config_json TEXT NOT NULL DEFAULT '{}',
			signal_template TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			last_fire DATETIME,
			created_at DATETIME NOT NULL DEFAULT (` + dbTimeDefault + `)
		)`,
	}

	for _, m := range migrations {
		if _, err := s.db.Exec(m); err != nil {
			return fmt.Errorf("exec migration: %w\nquery: %s", err, m)
		}
	}
	return s.migrateState(migrations)
}

func (s *Store) tableExists(name string) (bool, error) {
	row := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, name)
	var got string
	if err := row.Scan(&got); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return got == name, nil
}

func (s *Store) columnExists(table, column string) (bool, error) {
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
