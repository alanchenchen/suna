package memory

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

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
		// ── 核心表 ──

		// 会话：一个 session 代表一次连续对话
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
			summary TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active'
		)`,

		// 会话消息：对话的原始记录（唯一的完整对话存储点）
		`CREATE TABLE IF NOT EXISTS session_messages (
			session_id TEXT NOT NULL,
			turn INTEGER NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL DEFAULT '',
			tool_call TEXT NOT NULL DEFAULT '',
			tool_result TEXT NOT NULL DEFAULT '',
			significance TEXT NOT NULL DEFAULT '',
			memory_extracted INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			PRIMARY KEY (session_id, turn, role, created_at)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_session_msgs ON session_messages(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_session_msgs_extract ON session_messages(memory_extracted)`,

		// ── 记忆表 ──

		// 情景记忆：LLM 提取的结构化摘要（不存原始对话原文，原文在 session_messages）
		// embedding 用于语义搜索，source 标记提取方式
		`CREATE TABLE IF NOT EXISTS episodic_memories (
			id TEXT PRIMARY KEY,
			content TEXT NOT NULL,
			type TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT '',
			entities TEXT NOT NULL DEFAULT '[]',
			embedding BLOB,
			ts DATETIME NOT NULL DEFAULT (datetime('now')),
			session_id TEXT NOT NULL DEFAULT '',
			metadata TEXT NOT NULL DEFAULT '{}'
		)`,
		`CREATE INDEX IF NOT EXISTS idx_episodic_ts ON episodic_memories(ts)`,
		`CREATE INDEX IF NOT EXISTS idx_episodic_source ON episodic_memories(source)`,

		// FTS 索引（CJK 降级 LIKE，英文走 FTS）
		`CREATE VIRTUAL TABLE IF NOT EXISTS episodic_fts USING fts5(
			content,
			content='episodic_memories',
			content_rowid='rowid'
		)`,

		// 语义记忆：用户偏好、事实、习惯。
		// UPSERT 语义：同一 type+key 只保留最新值，避免膨胀。
		// 但保留更新历史（ts 字段），可追溯变化。
		`CREATE TABLE IF NOT EXISTS semantic_facts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			type TEXT NOT NULL,
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT '',
			ts DATETIME NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_semantic_type_key_ts ON semantic_facts(type, key, ts)`,

		// 实体：从记忆中提取的命名实体（Vue3、Vite 等）
		`CREATE TABLE IF NOT EXISTS entities (
			name TEXT PRIMARY KEY,
			memory_ids TEXT NOT NULL DEFAULT '[]',
			embedding BLOB,
			updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
		)`,

		// ── 运维表 ──

		// Token 用量日志
		`CREATE TABLE IF NOT EXISTS usage_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			cost REAL NOT NULL DEFAULT 0,
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

	s.db.Exec(`DROP INDEX IF EXISTS idx_semantic_unique`)

	return nil
}
