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
