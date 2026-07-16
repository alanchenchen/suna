package memory

import (
	"context"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/model"
)

func TestCommitQueueCompactionIsAtomicWhenQueueItemIsMissing(t *testing.T) {
	ctx := context.Background()
	store, err := NewStore(t.TempDir() + "/memory.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	memories := NewMemoryStore(store.DB())
	if err := memories.ReplaceAll(ctx, DefaultUserID, []UserMemory{{Kind: MemoryKindPreference, Content: "old profile", Confidence: .9}}); err != nil {
		t.Fatal(err)
	}
	q := NewExtractQueue(store.DB())
	if !q.Push(ctx, DefaultUserID, "test/model", Candidate{Kind: MemoryKindPreference, Content: "candidate", Confidence: .9}) {
		t.Fatal("Push failed")
	}
	items, err := LoadDueQueue(ctx, store.DB(), DefaultUserID, 1)
	if err != nil || len(items) != 1 {
		t.Fatalf("LoadDueQueue = %d, %v", len(items), err)
	}
	if err := memories.CommitQueueCompaction(ctx, DefaultUserID, []string{items[0].ID, "missing"}, []UserMemory{{Kind: MemoryKindPreference, Content: "new profile", Confidence: .9}}); err == nil {
		t.Fatal("CommitQueueCompaction() error = nil, want missing queue item error")
	}
	got, err := memories.List(ctx, DefaultUserID, MaxActiveMemories)
	if err != nil || len(got) != 1 || got[0].Content != "old profile" {
		t.Fatalf("profile after rolled-back commit = %#v, %v", got, err)
	}
	if QueueCount(ctx, store.DB(), DefaultUserID) != 1 {
		t.Fatal("queue item must remain when atomic commit rolls back")
	}
}

func TestUnresolvedModelUsesBackoff(t *testing.T) {
	store, err := NewStore(t.TempDir() + "/memory.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	q := NewExtractQueue(store.DB())
	if !q.Push(context.Background(), DefaultUserID, "missing/model", Candidate{Kind: MemoryKindPreference, Content: "needs resolver", Confidence: .9}) {
		t.Fatal("Push failed")
	}
	w := NewWorker(q, NewMemoryStore(store.DB()), store.DB(), func(string) (*model.ModelBinding, error) { return nil, nil })
	w.processPending()
	var attempts int
	var next time.Time
	if err := store.DB().QueryRow(`SELECT attempts, next_attempt_at FROM memory_queue`).Scan(&attempts, &next); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 || !next.After(time.Now().Add(4*time.Minute)) {
		t.Fatalf("unresolved model retry = attempts %d next %v, want attempt 1 with backoff", attempts, next)
	}
}

func TestMaterializePendingMemoryQueueModelRefUpdatesOnlyLegacyPendingRows(t *testing.T) {
	ctx := context.Background()
	store, err := NewStore(t.TempDir() + "/memory.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	db := store.DB()
	rows := []struct {
		id        string
		modelRef  any
		processed any
	}{
		{id: "legacy-empty", modelRef: ""},
		{id: "legacy-space", modelRef: " \t "},
		{id: "already-bound", modelRef: "other/model"},
		{id: "processed-empty", modelRef: "", processed: time.Now()},
	}
	for _, row := range rows {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO memory_queue (id, user_id, model_ref, kind, content, created_at, next_attempt_at, processed_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, row.id, DefaultUserID, row.modelRef, MemoryKindPreference, "candidate", time.Now(), time.Now(), row.processed); err != nil {
			t.Fatalf("insert %s: %v", row.id, err)
		}
	}

	updated, err := store.MaterializePendingMemoryQueueModelRef(ctx, "  openai/gpt-test  ")
	if err != nil {
		t.Fatalf("MaterializePendingMemoryQueueModelRef() error = %v", err)
	}
	if updated != 2 {
		t.Fatalf("updated rows = %d, want 2", updated)
	}
	for _, id := range []string{"legacy-empty", "legacy-space"} {
		var modelRef string
		if err := db.QueryRowContext(ctx, `SELECT model_ref FROM memory_queue WHERE id = ?`, id).Scan(&modelRef); err != nil {
			t.Fatal(err)
		}
		if modelRef != "openai/gpt-test" {
			t.Fatalf("%s model_ref = %q, want openai/gpt-test", id, modelRef)
		}
	}
	for id, want := range map[string]string{"already-bound": "other/model", "processed-empty": ""} {
		var modelRef string
		if err := db.QueryRowContext(ctx, `SELECT model_ref FROM memory_queue WHERE id = ?`, id).Scan(&modelRef); err != nil {
			t.Fatal(err)
		}
		if modelRef != want {
			t.Fatalf("%s model_ref = %q, want %q", id, modelRef, want)
		}
	}

	updated, err = store.MaterializePendingMemoryQueueModelRef(ctx, "openai/gpt-test")
	if err != nil {
		t.Fatalf("repeat materialization error = %v", err)
	}
	if updated != 0 {
		t.Fatalf("repeat updated rows = %d, want 0", updated)
	}
	if _, err := store.MaterializePendingMemoryQueueModelRef(ctx, " \t "); err == nil {
		t.Fatal("empty model_ref materialization error = nil, want error")
	}
}
