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

	if got := b.addStream(context.Background(), sink, "hel"); got {
		t.Fatalf("addStream() = %v, want false before threshold", got)
	}
	if got := b.addStream(context.Background(), sink, "lo"); got {
		t.Fatalf("addStream() = %v, want false before threshold", got)
	}
	b.flush(context.Background(), sink)

	if got := len(sink.events); got != 1 {
		t.Fatalf("len(events) = %d, want %d", got, 1)
	}
	if got := sink.events[0].Method; got != protocol.NotifyAgentDelta {
		t.Fatalf("events[0].Method = %q, want %q", got, protocol.NotifyAgentDelta)
	}
	params := sink.events[0].Params.(protocol.AgentDeltaParams)
	if got := params.Content; got != "hello" {
		t.Fatalf("AgentDeltaParams.Content = %q, want %q", got, "hello")
	}
	if got := params.Kind; got != protocol.AgentDeltaAssistant {
		t.Fatalf("AgentDeltaParams.Kind = %q, want %q", got, protocol.AgentDeltaAssistant)
	}
}

func TestStreamBatcherFlushesOnTypeSwitch(t *testing.T) {
	sink := &captureSink{}
	b := &streamBatcher{}

	b.addStream(context.Background(), sink, "answer")
	b.addReasoning(context.Background(), sink, "think")
	b.flush(context.Background(), sink)

	if got := len(sink.events); got != 2 {
		t.Fatalf("len(events) = %d, want %d", got, 2)
	}
	if got := sink.events[0].Method; got != protocol.NotifyAgentDelta {
		t.Fatalf("events[0].Method = %q, want %q", got, protocol.NotifyAgentDelta)
	}
	first := sink.events[0].Params.(protocol.AgentDeltaParams)
	if got := first.Kind; got != protocol.AgentDeltaAssistant {
		t.Fatalf("first kind = %q, want %q", got, protocol.AgentDeltaAssistant)
	}
	if got := sink.events[1].Method; got != protocol.NotifyAgentDelta {
		t.Fatalf("events[1].Method = %q, want %q", got, protocol.NotifyAgentDelta)
	}
	second := sink.events[1].Params.(protocol.AgentDeltaParams)
	if got := second.Kind; got != protocol.AgentDeltaReasoning {
		t.Fatalf("second kind = %q, want %q", got, protocol.AgentDeltaReasoning)
	}
}

func TestStreamBatcherSizeThreshold(t *testing.T) {
	b := &streamBatcher{}
	large := make([]byte, maxStreamBatchBytes)
	for i := range large {
		large[i] = 'x'
	}
	if got := b.addStream(context.Background(), &captureSink{}, string(large)); !got {
		t.Fatalf("addStream() = %v, want true at threshold", got)
	}
}

func TestStreamBatcherEmptyFlush(t *testing.T) {
	sink := &captureSink{}
	b := &streamBatcher{}
	b.flush(context.Background(), sink)
	if got := len(sink.events); got != 0 {
		t.Fatalf("len(events) = %d, want %d", got, 0)
	}
}
