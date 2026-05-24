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
		// ── Active memory tables ──

		`CREATE TABLE IF NOT EXISTS user_memory (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			content TEXT NOT NULL,
			tags TEXT NOT NULL DEFAULT '[]',
			priority INTEGER NOT NULL DEFAULT 50,
			is_core INTEGER NOT NULL DEFAULT 0,
			use_count INTEGER NOT NULL DEFAULT 0,
			last_used_at DATETIME,
			refreshed_at DATETIME NOT NULL DEFAULT (datetime('now')),
			expires_at DATETIME,
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_user_memory_core ON user_memory(user_id, is_core, priority)`,
		`CREATE INDEX IF NOT EXISTS idx_user_memory_kind ON user_memory(user_id, kind)`,
		`CREATE INDEX IF NOT EXISTS idx_user_memory_updated ON user_memory(user_id, updated_at)`,

		`CREATE TABLE IF NOT EXISTS conversation_state (
			user_id TEXT PRIMARY KEY,
			resume_summary TEXT NOT NULL DEFAULT '',
			last_messages TEXT NOT NULL DEFAULT '[]',
			tool_summary TEXT NOT NULL DEFAULT '[]',
			memory_processed_at DATETIME,
			updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
		)`,

		`CREATE TABLE IF NOT EXISTS memory_queue (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
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
	return nil
}
