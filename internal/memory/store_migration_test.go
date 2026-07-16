package memory

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMigrateLegacyConversationStateWithoutSessionStateColumn(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite error = %v", err)
	}
	_, err = db.Exec(`CREATE TABLE conversation_state (
		last_messages TEXT NOT NULL DEFAULT '[]',
		tool_summary TEXT NOT NULL DEFAULT '[]',
		updated_at DATETIME
	)`)
	if err != nil {
		t.Fatalf("create legacy table error = %v", err)
	}
	_, err = db.Exec(`INSERT INTO conversation_state (last_messages, tool_summary, updated_at) VALUES (?, ?, ?)`, `[{"role":"user","content":"hello"}]`, `[]`, `2025-01-02T03:04:05Z`)
	if err != nil {
		t.Fatalf("insert legacy row error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db error = %v", err)
	}

	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer store.Close()

	exists, err := store.tableExists("conversation_state")
	if err != nil {
		t.Fatalf("tableExists error = %v", err)
	}
	if exists {
		t.Fatal("conversation_state still exists after migration")
	}

	var sessionID string
	var messageCount int
	if err := store.DB().QueryRow(`SELECT id, message_count FROM sessions`).Scan(&sessionID, &messageCount); err != nil {
		t.Fatalf("read migrated session error = %v", err)
	}
	if sessionID == "" {
		t.Fatal("migrated session id is empty")
	}
	if messageCount != 1 {
		t.Fatalf("message_count = %d, want 1", messageCount)
	}

	var compacted string
	if err := store.DB().QueryRow(`SELECT compacted_state FROM session_state WHERE session_id = ?`, sessionID).Scan(&compacted); err != nil {
		t.Fatalf("read migrated session_state error = %v", err)
	}
	if compacted != "" {
		t.Fatalf("compacted_state = %q, want empty", compacted)
	}
}

func TestMigrateMemoryQueueOwnershipForExistingDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite error = %v", err)
	}
	_, err = db.Exec(`CREATE TABLE memory_queue (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		kind TEXT NOT NULL,
		content TEXT NOT NULL,
		tags TEXT NOT NULL DEFAULT '[]',
		source TEXT NOT NULL DEFAULT 'inferred',
		confidence REAL NOT NULL DEFAULT 0.7,
		evidence TEXT NOT NULL DEFAULT '',
		significance TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL,
		next_attempt_at DATETIME NOT NULL,
		attempts INTEGER NOT NULL DEFAULT 0,
		last_error TEXT NOT NULL DEFAULT '',
		processed_at DATETIME
	)`)
	if err != nil {
		t.Fatalf("create legacy memory_queue error = %v", err)
	}
	if _, err := db.Exec(`INSERT INTO memory_queue (id, user_id, kind, content, created_at, next_attempt_at) VALUES ('queue-1', 'default', 'preference', 'legacy event', datetime('now'), datetime('now'))`); err != nil {
		t.Fatalf("insert legacy queue row error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db error = %v", err)
	}

	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer store.Close()
	exists, err := store.columnExists("memory_queue", "model_ref")
	if err != nil {
		t.Fatalf("columnExists(model_ref) error = %v", err)
	}
	if !exists {
		t.Fatal("memory_queue.model_ref was not migrated")
	}
	var modelRef string
	if err := store.DB().QueryRow(`SELECT model_ref FROM memory_queue WHERE id = 'queue-1'`).Scan(&modelRef); err != nil {
		t.Fatalf("read migrated queue model_ref error = %v", err)
	}
	if modelRef != "" {
		t.Fatalf("legacy model_ref = %q, want empty", modelRef)
	}
	if err := store.migrateMemoryQueueModelRef(); err != nil {
		t.Fatalf("repeat migration error = %v", err)
	}
}

func TestMigrateSessionModelRefForExistingDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite error = %v", err)
	}
	_, err = db.Exec(`CREATE TABLE sessions (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL DEFAULT '',
		cwd TEXT NOT NULL DEFAULT '',
		message_count INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL,
		last_attached_at DATETIME
	)`)
	if err != nil {
		t.Fatalf("create legacy sessions error = %v", err)
	}
	if _, err := db.Exec(`INSERT INTO sessions (id, cwd, created_at, updated_at) VALUES ('session-1', '/tmp', datetime('now'), datetime('now'))`); err != nil {
		t.Fatalf("insert legacy session error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db error = %v", err)
	}

	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer store.Close()

	hasModelRef, err := store.columnExists("sessions", "model_ref")
	if err != nil {
		t.Fatalf("columnExists error = %v", err)
	}
	if !hasModelRef {
		t.Fatal("sessions.model_ref was not migrated")
	}
	var modelRef sql.NullString
	if err := store.DB().QueryRow(`SELECT model_ref FROM sessions WHERE id = 'session-1'`).Scan(&modelRef); err != nil {
		t.Fatalf("read migrated model_ref error = %v", err)
	}
	if modelRef.Valid {
		t.Fatalf("migrated model_ref = %q, want NULL", modelRef.String)
	}

	if err := store.migrateSessionModelRef(); err != nil {
		t.Fatalf("repeat migration error = %v", err)
	}
}
