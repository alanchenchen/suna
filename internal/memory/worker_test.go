package memory

import (
	"context"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/model"
)

func TestWorkerStopPreservesPendingQueue(t *testing.T) {
	store, err := NewStore(t.TempDir() + "/memory.db")
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer store.Close()

	queue := NewExtractQueue(store.DB())
	worker := NewWorker(queue, NewMemoryStore(store.DB()), store.DB(), workerTestProvider{})

	if ok := queue.Push(context.Background(), DefaultUserID, "user", "I prefer concise Chinese replies.", SignificanceMedium); !ok {
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

type workerTestProvider struct{}

func (workerTestProvider) Complete(context.Context, *model.CompletionRequest) (<-chan model.Chunk, error) {
	ch := make(chan model.Chunk, 1)
	ch <- model.Chunk{Content: `{"memories":[]}`, Done: true}
	close(ch)
	return ch, nil
}

func (workerTestProvider) EstimateTokens(string) int { return 0 }

func (workerTestProvider) ContextWindow() int { return 128000 }
