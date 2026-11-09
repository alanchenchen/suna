package memory

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestSessionColumnsUpgradeTransactionAndPreserveData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.DB().Exec(`ALTER TABLE session_state RENAME COLUMN summary TO compacted_state; ALTER TABLE session_state RENAME COLUMN messages TO last_messages; ALTER TABLE session_state ADD COLUMN tool_summary TEXT NOT NULL DEFAULT '[]'; INSERT INTO sessions(id,created_at,updated_at) VALUES ('s',datetime('now'),datetime('now')); INSERT INTO session_state(session_id,compacted_state,last_messages,tool_summary,updated_at) VALUES ('s','old memory','[{"role":"user","content":[{"type":"text","text":"original"}]}]','[]','2026-01-01')`)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var summary, messages, updated string
	if err = store.DB().QueryRow(`SELECT summary,messages,updated_at FROM session_state WHERE session_id='s'`).Scan(&summary, &messages, &updated); err != nil {
		t.Fatal(err)
	}
	if summary != "old memory" || messages == "" || updated == "" {
		t.Fatal("migration lost data")
	}
	if ok, err := store.columnExists("session_state", "tool_summary"); err != nil || ok {
		t.Fatalf("tool_summary exists=%v err=%v", ok, err)
	}
	if err = store.migrate(); err != nil {
		t.Fatal(err)
	}
}
func TestUnsupportedOldSchemaFailsWithoutDroppingLegacyData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE conversation_state(value TEXT);INSERT INTO conversation_state VALUES ('preserved')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if store, err := NewStore(path); err == nil {
		store.Close()
		t.Fatal("unsupported old schema opened")
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var value string
	if err = db.QueryRow(`SELECT value FROM conversation_state`).Scan(&value); err != nil || value != "preserved" {
		t.Fatalf("value=%q err=%v", value, err)
	}
}

func TestSessionMigrationRollsBackAllColumnChangesOnInvalidMessages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.DB().Exec(`ALTER TABLE session_state RENAME COLUMN summary TO compacted_state; ALTER TABLE session_state RENAME COLUMN messages TO last_messages; ALTER TABLE session_state ADD COLUMN tool_summary TEXT NOT NULL DEFAULT '[]'; INSERT INTO sessions(id,created_at,updated_at) VALUES ('s',datetime('now'),datetime('now')); INSERT INTO session_state(session_id,compacted_state,last_messages,updated_at) VALUES ('s','retained','broken-json',datetime('now'))`)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	if store, err = NewStore(path); err == nil {
		store.Close()
		t.Fatal("malformed history was accepted")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var summary, messages, tools string
	if err = db.QueryRow(`SELECT compacted_state,last_messages,tool_summary FROM session_state WHERE session_id='s'`).Scan(&summary, &messages, &tools); err != nil {
		t.Fatal(err)
	}
	if summary != "retained" || messages != "broken-json" || tools != "[]" {
		t.Fatal("failed migration altered original data")
	}
}
