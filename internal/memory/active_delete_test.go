package memory

import (
	"context"
	"testing"
)

func TestQueuePushPersistsModelRef(t *testing.T) {
	store, err := NewStore(t.TempDir() + "/memory.db")
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer store.Close()

	queue := NewExtractQueue(store.DB())
	if !queue.Push(context.Background(), DefaultUserID, "openai/gpt-test", Candidate{Kind: MemoryKindPreference, Content: "prefer tests", Confidence: 0.9, Significance: SignificanceHigh}) {
		t.Fatal("Push() = false, want true")
	}
	items, err := LoadDueQueue(context.Background(), store.DB(), DefaultUserID, 1)
	if err != nil {
		t.Fatalf("LoadDueQueue() error = %v", err)
	}
	if len(items) != 1 || items[0].ModelRef != "openai/gpt-test" {
		t.Fatalf("queue model_ref = %#v, want openai/gpt-test", items)
	}
}

func TestMemoryStoreDeleteRemovesActiveMemory(t *testing.T) {
	store, err := NewStore(t.TempDir() + "/memory.db")
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	memories := NewMemoryStore(store.DB())
	if err := memories.ReplaceAll(ctx, DefaultUserID, []UserMemory{{Kind: MemoryKindPreference, Content: "prefer concise answers", Confidence: 0.9, Priority: 80}}); err != nil {
		t.Fatalf("ReplaceAll() error = %v", err)
	}
	listed, err := memories.List(ctx, DefaultUserID, MaxActiveMemories)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("len(List()) = %d, want 1", len(listed))
	}

	deleted, err := memories.Delete(ctx, DefaultUserID, listed[0].ID)
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if !deleted {
		t.Fatal("Delete() = false, want true")
	}
	listed, err = memories.List(ctx, DefaultUserID, MaxActiveMemories)
	if err != nil {
		t.Fatalf("List() after delete error = %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("len(List()) after delete = %d, want 0", len(listed))
	}
}

func TestMemoryStoreClearDoesNotDeleteQueue(t *testing.T) {
	store, err := NewStore(t.TempDir() + "/memory.db")
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	memories := NewMemoryStore(store.DB())
	if err := memories.ReplaceAll(ctx, DefaultUserID, []UserMemory{
		{Kind: MemoryKindPreference, Content: "prefer concise answers", Confidence: 0.9, Priority: 80},
		{Kind: MemoryKindWorkflow, Content: "review code before editing", Confidence: 0.9, Priority: 70},
	}); err != nil {
		t.Fatalf("ReplaceAll() error = %v", err)
	}
	queue := NewExtractQueue(store.DB())
	if !queue.Push(ctx, DefaultUserID, "openai/test", Candidate{Kind: MemoryKindPreference, Content: "prefer tests", Confidence: 0.9, Significance: SignificanceHigh}) {
		t.Fatal("Push() = false, want true")
	}

	deleted, err := memories.Clear(ctx, DefaultUserID)
	if err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	if deleted != 2 {
		t.Fatalf("Clear() = %d, want 2", deleted)
	}
	listed, err := memories.List(ctx, DefaultUserID, MaxActiveMemories)
	if err != nil {
		t.Fatalf("List() after clear error = %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("len(List()) after clear = %d, want 0", len(listed))
	}
	if got := QueueCount(ctx, store.DB(), DefaultUserID); got != 1 {
		t.Fatalf("QueueCount() = %d, want 1", got)
	}
}
