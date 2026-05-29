package daemon

import (
	"context"
	"testing"

	"github.com/alanchenchen/suna/internal/protocol"
)

type captureSink struct {
	events []protocol.Event
}

func (s *captureSink) Emit(ctx context.Context, event protocol.Event) error {
	s.events = append(s.events, event)
	return nil
}

func TestStreamBatcherMergesStream(t *testing.T) {
	sink := &captureSink{}
	b := &streamBatcher{}

	if b.addStream(context.Background(), sink, "hel") {
		t.Fatal("addStream flushed too early")
	}
	if b.addStream(context.Background(), sink, "lo") {
		t.Fatal("addStream flushed too early")
	}
	b.flush(context.Background(), sink)

	if len(sink.events) != 1 {
		t.Fatalf("events = %d, want 1", len(sink.events))
	}
	if sink.events[0].Method != protocol.NotifyStream {
		t.Fatalf("method = %q", sink.events[0].Method)
	}
	params := sink.events[0].Params.(protocol.StreamParams)
	if params.Chunk != "hello" {
		t.Fatalf("chunk = %q", params.Chunk)
	}
}

func TestStreamBatcherFlushesOnTypeSwitch(t *testing.T) {
	sink := &captureSink{}
	b := &streamBatcher{}

	b.addStream(context.Background(), sink, "answer")
	b.addReasoning(context.Background(), sink, "think")
	b.flush(context.Background(), sink)

	if len(sink.events) != 2 {
		t.Fatalf("events = %d, want 2", len(sink.events))
	}
	if sink.events[0].Method != protocol.NotifyStream || sink.events[1].Method != protocol.NotifyReasoning {
		t.Fatalf("methods = %q, %q", sink.events[0].Method, sink.events[1].Method)
	}
}

func TestStreamBatcherSizeThreshold(t *testing.T) {
	b := &streamBatcher{}
	large := make([]byte, maxStreamBatchBytes)
	for i := range large {
		large[i] = 'x'
	}
	if !b.addStream(context.Background(), &captureSink{}, string(large)) {
		t.Fatal("addStream did not request flush at threshold")
	}
}

func TestStreamBatcherEmptyFlush(t *testing.T) {
	sink := &captureSink{}
	b := &streamBatcher{}
	b.flush(context.Background(), sink)
	if len(sink.events) != 0 {
		t.Fatalf("events = %d, want 0", len(sink.events))
	}
}
