package events

import (
	"encoding/json"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/alanchenchen/suna/internal/protocol"
)

func TestBatcherRunFlushesDeltaOnTimer(t *testing.T) {
	ch := make(chan Notification, 1)
	out := make(chan AgentDeltaMsg, 1)
	batcher := &Batcher{Send: func(msg tea.Msg) {
		delta, ok := msg.(AgentDeltaMsg)
		if !ok {
			t.Fatalf("msg type = %T, want AgentDeltaMsg", msg)
		}
		out <- delta
	}}
	go batcher.Run(ch)
	ch <- deltaNotificationForTest(t, "hello")

	select {
	case got := <-out:
		if got.Params.Content != "hello" {
			t.Fatalf("content = %q, want hello", got.Params.Content)
		}
	case <-time.After(StreamFlushInterval * 4):
		t.Fatal("timed out waiting for timer flush")
	}
}

func deltaNotificationForTest(t *testing.T, content string) Notification {
	t.Helper()
	data, err := json.Marshal(protocol.AgentDeltaParams{Kind: protocol.AgentDeltaAssistant, Content: content})
	if err != nil {
		t.Fatal(err)
	}
	return Notification{Method: protocol.NotifyAgentDelta, Params: data}
}
