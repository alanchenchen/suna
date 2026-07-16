package memory

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSessionStoreModelRefRoundTrip(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewStore error = %v", err)
	}
	defer store.Close()

	sessions := NewSessionStore(store.DB())
	ctx := context.Background()
	if err := sessions.Create(ctx, SessionMeta{ID: "session-1", CWD: "/tmp", ModelRef: "openai/gpt-4.1"}); err != nil {
		t.Fatalf("Create error = %v", err)
	}

	got, err := sessions.Get(ctx, "session-1")
	if err != nil {
		t.Fatalf("Get error = %v", err)
	}
	if got == nil || got.ModelRef != "openai/gpt-4.1" {
		t.Fatalf("Get ModelRef = %#v, want openai/gpt-4.1", got)
	}

	listed, err := sessions.List(ctx)
	if err != nil {
		t.Fatalf("List error = %v", err)
	}
	if len(listed) != 1 || listed[0].ModelRef != "openai/gpt-4.1" {
		t.Fatalf("List = %#v, want persisted model ref", listed)
	}

	if err := sessions.UpdateMetadata(ctx, "session-1", "", false, "anthropic/claude-sonnet-4", true); err != nil {
		t.Fatalf("UpdateMetadata error = %v", err)
	}
	got, err = sessions.Get(ctx, "session-1")
	if err != nil {
		t.Fatalf("Get after update error = %v", err)
	}
	if got.ModelRef != "anthropic/claude-sonnet-4" {
		t.Fatalf("updated ModelRef = %q", got.ModelRef)
	}

	if err := sessions.UpdateMetadata(ctx, "session-1", "", false, "", true); err != nil {
		t.Fatalf("clear UpdateMetadata error = %v", err)
	}
	got, err = sessions.Get(ctx, "session-1")
	if err != nil {
		t.Fatalf("Get after clear error = %v", err)
	}
	if got.ModelRef != "" {
		t.Fatalf("cleared ModelRef = %q, want empty", got.ModelRef)
	}
}
