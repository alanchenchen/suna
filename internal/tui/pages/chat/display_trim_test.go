package chat

import (
	"strings"
	"testing"
	"time"
)

func TestTrimDisplayHistoryDropsOldTurnsAndKeepsUserAtTop(t *testing.T) {
	m := &Model{Messages: []Msg{
		{Role: "user", Content: strings.Repeat("u1", 200)},
		{Role: "assistant", Content: strings.Repeat("a1", 200)},
		{Role: "reasoning", Content: strings.Repeat("r1", 200)},
		{Role: "user", Content: strings.Repeat("u2", 200)},
		{Role: "assistant", Content: strings.Repeat("a2", 200)},
		{Role: "user", Content: "recent"},
		{Role: "assistant", Content: "answer"},
	}}

	if !m.TrimDisplayHistory(1800) {
		t.Fatal("TrimDisplayHistory() = false, want true")
	}
	if len(m.Messages) == 0 {
		t.Fatal("messages empty after trim, want recent turn kept")
	}
	if got := m.Messages[0].Role; got != "user" {
		t.Fatalf("first role = %q, want user", got)
	}
	if m.DisplayDiscard.Turns == 0 || m.DisplayDiscard.Messages == 0 || m.DisplayDiscard.Bytes == 0 {
		t.Fatalf("discard summary = %+v, want accumulated values", m.DisplayDiscard)
	}
}

func TestTrimDisplayHistoryDoesNotDropOnlyTurn(t *testing.T) {
	m := &Model{Messages: []Msg{
		{Role: "user", Content: strings.Repeat("u", 2000)},
		{Role: "assistant", Content: strings.Repeat("a", 2000)},
	}}

	if m.TrimDisplayHistory(1024) {
		t.Fatal("TrimDisplayHistory() = true, want false when no next user boundary exists")
	}
	if got := len(m.Messages); got != 2 {
		t.Fatalf("messages = %d, want 2", got)
	}
}

func TestTrimDisplayHistoryAccumulatesSummary(t *testing.T) {
	m := &Model{
		DisplayDiscard: DisplayDiscardSummary{Messages: 2, Turns: 1, Bytes: 100},
		Messages: []Msg{
			{Role: "user", Content: strings.Repeat("u1", 200)},
			{Role: "assistant", Content: strings.Repeat("a1", 200)},
			{Role: "user", Content: "recent"},
			{Role: "assistant", Content: "answer"},
		},
	}

	if !m.TrimDisplayHistory(1200) {
		t.Fatal("TrimDisplayHistory() = false, want true")
	}
	if got, want := m.DisplayDiscard.Turns, 2; got != want {
		t.Fatalf("discard turns = %d, want %d", got, want)
	}
	if got := m.DisplayDiscard.Messages; got <= 2 {
		t.Fatalf("discard messages = %d, want accumulated > 2", got)
	}
}

func TestStreamingStateRendersAndMaterializesOnFinish(t *testing.T) {
	m := &Model{}
	m.AppendStreamMessage("assistant", "hello", time.Now())
	m.AppendStreamMessage("assistant", " world", time.Now())
	if got := len(m.Messages); got != 1 {
		t.Fatalf("messages = %d, want 1", got)
	}
	if m.Messages[0].Stream == nil {
		t.Fatal("stream state is nil, want active streaming buffer")
	}
	m.FinishStreamingMessages(time.Now())
	if m.Messages[0].Stream != nil {
		t.Fatal("stream state is not nil after finish")
	}
	if got, _ := m.Messages[0].Content.(string); got != "hello world" {
		t.Fatalf("content = %q, want hello world", got)
	}
}
