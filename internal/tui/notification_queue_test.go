package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/protocol"
)

func TestAppendMergedNotificationMergesAdjacentDelta(t *testing.T) {
	items := appendMergedNotification(nil, localDeltaNotification(t, protocol.AgentDeltaAssistant, "a"))
	items = appendMergedNotification(items, localDeltaNotification(t, protocol.AgentDeltaAssistant, "b"))

	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	params := decodePendingDeltaForTest(t, items[0])
	if got, want := params.Content, "ab"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestAppendMergedNotificationDoesNotMergeAcrossNonDelta(t *testing.T) {
	items := appendMergedNotification(nil, localDeltaNotification(t, protocol.AgentDeltaAssistant, "a"))
	items = append(items, pendingNotification{notif: localNotification{method: protocol.NotifyAgentRun}})
	items = appendMergedNotification(items, localDeltaNotification(t, protocol.AgentDeltaAssistant, "b"))

	if got, want := len(items), 3; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
}

func BenchmarkAppendMergedNotificationDeltaBurst(b *testing.B) {
	chunk := strings.Repeat("x", 64)
	notif := localDeltaNotification(b, protocol.AgentDeltaAssistant, chunk)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var items []pendingNotification
		for j := 0; j < 512; j++ {
			items = appendMergedNotification(items, notif)
		}
		if got, want := len(items), 1; got != want {
			b.Fatalf("len(items) = %d, want %d", got, want)
		}
	}
}

func BenchmarkNotificationQueueOverflowDeltaBurst(b *testing.B) {
	chunk := strings.Repeat("x", 64)
	notif := localDeltaNotification(b, protocol.AgentDeltaAssistant, chunk)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		seen := 0
		q := &notificationQueue{ch: make(chan localNotification), wake: make(chan struct{}, 1)}
		for j := 0; j < 512; j++ {
			q.enqueue(notif)
		}
		q.flushPending(func(localNotification) {
			seen++
		})
		if seen != 1 {
			b.Fatalf("seen = %d, want 1", seen)
		}
	}
}

func localDeltaNotification(tb testing.TB, kind protocol.AgentDeltaKind, content string) localNotification {
	tb.Helper()
	data, err := json.Marshal(protocol.AgentDeltaParams{Kind: kind, Content: content})
	if err != nil {
		tb.Fatal(err)
	}
	return localNotification{method: protocol.NotifyAgentDelta, params: data}
}

func decodePendingDeltaForTest(tb testing.TB, item pendingNotification) protocol.AgentDeltaParams {
	tb.Helper()
	if item.delta == nil {
		tb.Fatal("pending delta is nil")
	}
	notif, ok := item.delta.notification()
	if !ok {
		tb.Fatal("pending delta notification failed")
	}
	var params protocol.AgentDeltaParams
	if err := json.Unmarshal(notif.params, &params); err != nil {
		tb.Fatal(err)
	}
	return params
}

func TestNotificationQueuePreservesPendingOrder(t *testing.T) {
	q := &notificationQueue{ch: make(chan localNotification, 4), wake: make(chan struct{}, 1)}
	q.pending = appendMergedNotification(q.pending, localDeltaNotification(t, protocol.AgentDeltaAssistant, "a"))
	q.enqueue(localNotification{method: protocol.NotifyAgentRun})

	var methods []string
	q.flushPending(func(notif localNotification) {
		methods = append(methods, notif.method)
	})
	if got, want := len(methods), 2; got != want {
		t.Fatalf("len(methods) = %d, want %d", got, want)
	}
	if methods[0] != protocol.NotifyAgentDelta || methods[1] != protocol.NotifyAgentRun {
		t.Fatalf("methods = %v, want delta then run", methods)
	}
}
