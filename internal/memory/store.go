package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func NewStore(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
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
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_user_profile_memory_core ON user_profile_memory(user_id, is_core, priority)`,
		`CREATE INDEX IF NOT EXISTS idx_user_profile_memory_kind ON user_profile_memory(user_id, kind)`,
		`CREATE INDEX IF NOT EXISTS idx_user_profile_memory_updated ON user_profile_memory(user_id, updated_at)`,

		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL DEFAULT '',
			cwd TEXT NOT NULL DEFAULT '',
			message_count INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			last_attached_at DATETIME
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_updated ON sessions(updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_cwd ON sessions(cwd, updated_at)`,

		`CREATE TABLE IF NOT EXISTS session_state (
			session_id TEXT PRIMARY KEY,
			compacted_state TEXT NOT NULL DEFAULT '',
			last_messages TEXT NOT NULL DEFAULT '[]',
			tool_summary TEXT NOT NULL DEFAULT '[]',
			updated_at DATETIME NOT NULL,
			FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
		)`,

		`CREATE TABLE IF NOT EXISTS memory_queue (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			content TEXT NOT NULL,
			tags TEXT NOT NULL DEFAULT '[]',
			source TEXT NOT NULL DEFAULT 'inferred',
			confidence REAL NOT NULL DEFAULT 0.7,
			evidence TEXT NOT NULL DEFAULT '',
			significance TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			next_attempt_at DATETIME NOT NULL DEFAULT (datetime('now')),
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
			created_at DATETIME NOT NULL DEFAULT (datetime('now'))
		)`,

		// Guard 审计日志
		`CREATE TABLE IF NOT EXISTS audit_log (
			id TEXT PRIMARY KEY,
			timestamp DATETIME NOT NULL DEFAULT (datetime('now')),
			session_id TEXT NOT NULL DEFAULT '',
			tool TEXT NOT NULL,
			params TEXT NOT NULL DEFAULT '{}',
			risk_level TEXT NOT NULL DEFAULT '',
			guard_decision TEXT NOT NULL DEFAULT '',
			guard_reason TEXT NOT NULL DEFAULT '',
			result TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT ''
		)`,

		// ── Phase 3+ 表（预留）──

		`CREATE TABLE IF NOT EXISTS failure_records (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			pattern TEXT NOT NULL,
			operation TEXT NOT NULL,
			reason TEXT NOT NULL,
			context TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_failure_pattern ON failure_records(pattern)`,

		`CREATE TABLE IF NOT EXISTS trust_rules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			pattern TEXT NOT NULL,
			tool TEXT NOT NULL DEFAULT '',
			risk_adjustment TEXT NOT NULL DEFAULT '',
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
			created_at DATETIME NOT NULL DEFAULT (datetime('now'))
		)`,
	}

	for _, m := range migrations {
		if _, err := s.db.Exec(m); err != nil {
			return fmt.Errorf("exec migration: %w\nquery: %s", err, m)
		}
	}
	if err := s.migrateLegacyConversationState(); err != nil {
		return err
	}
	return nil
}

func (s *Store) migrateLegacyConversationState() error {
	exists, err := s.tableExists("conversation_state")
	if err != nil || !exists {
		return err
	}
	selectState := "''"
	if ok, err := s.columnExists("conversation_state", "session_state"); err != nil {
		return err
	} else if ok {
		selectState = "session_state"
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// session_state 列只存在于较新的单会话库；更旧库迁移时把 compacted state 当空串处理。
	rows, err := tx.Query(fmt.Sprintf(`SELECT %s, last_messages, tool_summary, updated_at FROM conversation_state`, selectState))
	if err != nil {
		return fmt.Errorf("read legacy conversation_state: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var compacted, lastMessages, toolSummary string
		var updated sql.NullString
		if err := rows.Scan(&compacted, &lastMessages, &toolSummary, &updated); err != nil {
			return err
		}
		if strings.TrimSpace(compacted) == "" && emptyJSONMessages(lastMessages) {
			continue
		}
		id := uuid.New().String()
		when := parseDBTime(updated.String)
		if when.IsZero() {
			when = time.Now()
		}
		count := countLegacyMessages(lastMessages)
		cwd := ""
		if wd, err := os.Getwd(); err == nil {
			cwd = wd
		}
		if _, err := tx.Exec(`INSERT INTO sessions (id, title, cwd, message_count, created_at, updated_at) VALUES (?, '', ?, ?, ?, ?)`, id, cwd, count, when, when); err != nil {
			return fmt.Errorf("create migrated session: %w", err)
		}
		if _, err := tx.Exec(`INSERT INTO session_state (session_id, compacted_state, last_messages, tool_summary, updated_at) VALUES (?, ?, ?, ?, ?)`, id, strings.TrimSpace(compacted), normalizeLegacyJSON(lastMessages, "[]"), normalizeLegacyJSON(toolSummary, "[]"), when); err != nil {
			return fmt.Errorf("create migrated session state: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE conversation_state`); err != nil {
		return fmt.Errorf("drop legacy conversation_state: %w", err)
	}
	return tx.Commit()
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

func emptyJSONMessages(raw string) bool {
	raw = strings.TrimSpace(raw)
	return raw == "" || raw == "[]" || raw == "null"
}

func normalizeLegacyJSON(raw, fallback string) string {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	return raw
}

func countLegacyMessages(raw string) int {
	var arr []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &arr); err != nil {
		return 0
	}
	return len(arr)
}
