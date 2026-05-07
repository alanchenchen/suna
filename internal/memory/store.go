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
		`CREATE INDEX IF NOT EXISTS idx_episodic_type ON episodic_memories(type)`,
		`CREATE INDEX IF NOT EXISTS idx_episodic_source ON episodic_memories(source)`,

		`CREATE VIRTUAL TABLE IF NOT EXISTS episodic_fts USING fts5(
			content,
			content='episodic_memories',
			content_rowid='rowid'
		)`,

		`CREATE TABLE IF NOT EXISTS entities (
			name TEXT PRIMARY KEY,
			memory_ids TEXT NOT NULL DEFAULT '[]',
			embedding BLOB,
			updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
		)`,

		`CREATE TABLE IF NOT EXISTS semantic_facts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			type TEXT NOT NULL,
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT '',
			ts DATETIME NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_semantic_type_key_ts ON semantic_facts(type, key, ts)`,

		`CREATE TABLE IF NOT EXISTS failure_records (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			pattern TEXT NOT NULL,
			operation TEXT NOT NULL,
			reason TEXT NOT NULL,
			context TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_failure_pattern ON failure_records(pattern)`,

		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
			summary TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active'
		)`,

		`CREATE TABLE IF NOT EXISTS session_messages (
			session_id TEXT NOT NULL,
			turn INTEGER NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL DEFAULT '',
			tool_call TEXT NOT NULL DEFAULT '',
			tool_result TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT (datetime('now')),
			PRIMARY KEY (session_id, turn, role, created_at)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_session_msgs ON session_messages(session_id)`,

		`CREATE TABLE IF NOT EXISTS usage_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			cost REAL NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT (datetime('now'))
		)`,

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
