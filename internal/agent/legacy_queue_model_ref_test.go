package agent

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/protocol"
)

func TestNewAgentMaterializesLegacyPendingMemoryQueueModelRef(t *testing.T) {
	dir := t.TempDir()
	cfg := newAgentConfig(dir, []config.ModelConfig{openAIModel("gpt-test")}, "openai/gpt-test")
	cfg.Models[0].APIKey = "test-key"
	insertLegacyPendingMemoryQueueRow(t, cfg.DBPath(), "legacy-on-start")

	a, err := NewAgent(cfg)
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}
	defer a.Close()

	if got := pendingMemoryQueueModelRef(t, a.store.DB(), "legacy-on-start"); got != "openai/gpt-test" {
		t.Fatalf("legacy queue model_ref after initialization = %q, want openai/gpt-test", got)
	}
}

func TestUpdateConfigMaterializesRetainedLegacyPendingMemoryQueueWhenActiveModelAppears(t *testing.T) {
	dir := t.TempDir()
	cfg := newAgentConfig(dir, nil, "")
	a, err := NewAgent(cfg)
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}
	defer a.Close()
	insertLegacyPendingMemoryQueueRow(t, cfg.DBPath(), "legacy-after-setup")

	if _, err := a.UpdateConfig(ConfigSetParams{
		Action: protocol.ConfigActionUpsertModel,
		Model: ConfigModel{
			Provider:        "openai",
			Model:           "gpt-test",
			BaseURL:         "https://api.openai.com/v1",
			ContextWindow:   128000,
			MaxOutputTokens: 8192,
		},
		APIKey: "test-key",
	}); err != nil {
		t.Fatalf("UpdateConfig() error = %v", err)
	}

	if got := pendingMemoryQueueModelRef(t, a.store.DB(), "legacy-after-setup"); got != "openai/gpt-test" {
		t.Fatalf("legacy queue model_ref after config update = %q, want openai/gpt-test", got)
	}
}

func insertLegacyPendingMemoryQueueRow(t *testing.T, dbPath, id string) {
	t.Helper()
	store, err := memory.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer store.Close()
	if _, err := store.DB().ExecContext(context.Background(), `
		INSERT INTO memory_queue (id, user_id, model_ref, kind, content, created_at, next_attempt_at)
		VALUES (?, ?, '', ?, ?, ?, ?)`, id, memory.DefaultUserID, memory.MemoryKindPreference, "legacy candidate", time.Now(), time.Now()); err != nil {
		t.Fatalf("insert legacy queue row: %v", err)
	}
}

func pendingMemoryQueueModelRef(t *testing.T, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string) string {
	t.Helper()
	var modelRef string
	if err := db.QueryRowContext(context.Background(), `SELECT model_ref FROM memory_queue WHERE id = ?`, id).Scan(&modelRef); err != nil {
		t.Fatalf("read queue model_ref: %v", err)
	}
	return modelRef
}
