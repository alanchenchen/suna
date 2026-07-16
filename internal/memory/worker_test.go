package memory

import (
	"context"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/model"
)

func TestWorkerGroupsPendingEventsByModelRef(t *testing.T) {
	store, err := NewStore(t.TempDir() + "/memory.db")
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer store.Close()

	queue := NewExtractQueue(store.DB())
	for _, event := range []struct {
		modelRef string
	}{
		{"openai/a"},
		{"openai/b"},
		{"openai/a"},
	} {
		if !queue.Push(context.Background(), DefaultUserID, event.modelRef, Candidate{Kind: MemoryKindPreference, Content: "prefer concise responses", Confidence: 0.9, Significance: SignificanceHigh}) {
			t.Fatal("Push() = false, want true")
		}
	}

	calls := make(map[string]int)
	worker := NewWorker(queue, NewMemoryStore(store.DB()), store.DB(), func(modelRef string) (*model.ModelBinding, error) {
		calls[modelRef]++
		return newTestModelBinding(t, `{"memories":[]}`, nil), nil
	})
	worker.processPending()

	if calls["openai/a"] != 1 || calls["openai/b"] != 1 || len(calls) != 2 {
		t.Fatalf("resolver calls = %#v, want one call per model ref", calls)
	}
	if got := QueueCount(context.Background(), store.DB(), DefaultUserID); got != 0 {
		t.Fatalf("QueueCount() = %d, want 0", got)
	}
}

func TestWorkerModelGroupContextsAreIndependent(t *testing.T) {
	store, err := NewStore(t.TempDir() + "/memory.db")
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer store.Close()

	queue := NewExtractQueue(store.DB())
	for _, modelRef := range []string{"openai/a", "openai/b"} {
		if !queue.Push(context.Background(), DefaultUserID, modelRef, Candidate{Kind: MemoryKindPreference, Content: "prefer concise responses", Confidence: 0.9, Significance: SignificanceHigh}) {
			t.Fatalf("Push(%q) = false, want true", modelRef)
		}
	}
	items, err := LoadDueQueue(context.Background(), store.DB(), DefaultUserID, 50)
	if err != nil {
		t.Fatalf("LoadDueQueue() error = %v", err)
	}
	groups := make(map[string][]QueueItem)
	refs := make([]string, 0, len(items))
	for _, item := range items {
		if _, exists := groups[item.ModelRef]; !exists {
			refs = append(refs, item.ModelRef)
		}
		groups[item.ModelRef] = append(groups[item.ModelRef], item)
	}

	worker := NewWorker(queue, NewMemoryStore(store.DB()), store.DB(), func(string) (*model.ModelBinding, error) {
		return newTestModelBinding(t, `{"memories":[]}`, nil), nil
	})
	contexts := make([]context.Context, 0, len(refs))
	worker.processModelGroups(refs, groups, func() (context.Context, context.CancelFunc) {
		if len(contexts) == 0 {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			contexts = append(contexts, ctx)
			return ctx, func() {}
		}
		ctx := context.Background()
		contexts = append(contexts, ctx)
		return ctx, func() {}
	})

	if len(contexts) != 2 {
		t.Fatalf("context count = %d, want 2", len(contexts))
	}
	if err := contexts[0].Err(); err != context.Canceled {
		t.Fatalf("first group context error = %v, want %v", err, context.Canceled)
	}
	if err := contexts[1].Err(); err != nil {
		t.Fatalf("second group context error = %v, want nil", err)
	}
	if got := QueueCount(context.Background(), store.DB(), DefaultUserID); got != 1 {
		t.Fatalf("QueueCount() = %d, want 1 retained item from canceled first group", got)
	}
}

func TestWorkerRetainsLegacyOrUnresolvedModelQueueItems(t *testing.T) {
	store, err := NewStore(t.TempDir() + "/memory.db")
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer store.Close()

	queue := NewExtractQueue(store.DB())
	if !queue.Push(context.Background(), DefaultUserID, "", Candidate{Kind: MemoryKindPreference, Content: "legacy event", Confidence: 0.9, Significance: SignificanceHigh}) {
		t.Fatal("Push() legacy = false, want true")
	}
	if !queue.Push(context.Background(), DefaultUserID, "openai/missing", Candidate{Kind: MemoryKindPreference, Content: "unresolved event", Confidence: 0.9, Significance: SignificanceHigh}) {
		t.Fatal("Push() unresolved = false, want true")
	}

	resolverCalls := 0
	worker := NewWorker(queue, NewMemoryStore(store.DB()), store.DB(), func(modelRef string) (*model.ModelBinding, error) {
		resolverCalls++
		return nil, nil
	})
	worker.processPending()

	if resolverCalls != 1 {
		t.Fatalf("resolver calls = %d, want 1 for only non-empty model_ref", resolverCalls)
	}
	if got := QueueCount(context.Background(), store.DB(), DefaultUserID); got != 2 {
		t.Fatalf("QueueCount() = %d, want 2 retained items", got)
	}
}

func TestWorkerStopPreservesPendingQueue(t *testing.T) {
	store, err := NewStore(t.TempDir() + "/memory.db")
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer store.Close()

	queue := NewExtractQueue(store.DB())
	worker := NewWorker(queue, NewMemoryStore(store.DB()), store.DB(), func(modelRef string) (*model.ModelBinding, error) {
		if modelRef != "openai/test" {
			t.Fatalf("resolver model_ref = %q, want openai/test", modelRef)
		}
		return newTestModelBinding(t, `{"memories":[]}`, nil), nil
	})

	if ok := queue.Push(context.Background(), DefaultUserID, "openai/test", Candidate{Kind: MemoryKindCommunication, Content: "用户偏好简洁中文回复", Tags: []string{"communication"}, Source: MemorySourceExplicit, Confidence: 0.9, Evidence: "I prefer concise Chinese replies.", Significance: SignificanceMedium}); !ok {
		t.Fatal("Push() failed")
	}
	queue.Close()

	done := make(chan struct{})
	go func() {
		worker.Run()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after queue close")
	}

	got := QueueCount(context.Background(), store.DB(), DefaultUserID)
	if got != 1 {
		t.Fatalf("QueueCount() = %d, want 1", got)
	}
}
