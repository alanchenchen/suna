package chat

import (
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/tui/components/toolview"
)

func TestFinishCancellingToolsStopsActiveEntries(t *testing.T) {
	started := time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)
	ended := started.Add(3 * time.Second)
	entry := &toolview.Entry{ID: "exec-1", Status: toolview.StatusRunning, StartedAt: started}
	m := Model{
		ActiveTools:    map[string]*toolview.Entry{"exec-1": entry},
		ToolStartTimes: map[string]time.Time{"exec-1": started},
	}

	m.MarkActiveToolsCancelling()
	if entry.Status != toolview.StatusCancelling {
		t.Fatalf("status = %v, want cancelling", entry.Status)
	}
	m.FinishCancellingTools(ended)

	if entry.Status != toolview.StatusCancelled {
		t.Fatalf("status = %v, want cancelled", entry.Status)
	}
	if entry.Duration != 3*time.Second || !entry.EndedAt.Equal(ended) {
		t.Fatalf("duration/end = %v/%v", entry.Duration, entry.EndedAt)
	}
	if len(m.ActiveTools) != 0 || len(m.ToolStartTimes) != 0 {
		t.Fatal("cancelled tool remains active")
	}
}

func TestLateToolEndDoesNotOverwriteCancelledStatus(t *testing.T) {
	entry := &toolview.Entry{ID: "exec-1", Status: toolview.StatusCancelled}
	block := &toolview.Block{Entries: map[string]*toolview.Entry{"exec-1": entry}, Order: []string{"exec-1"}}
	m := Model{Messages: []Msg{{Role: "tool", Content: block}}, ActiveTools: map[string]*toolview.Entry{}, ToolStartTimes: map[string]time.Time{}}

	m.EndTool(protocol.ToolEndParams{ID: "exec-1", Result: "late success"}, "exec-1", time.Now())
	if entry.Status != toolview.StatusCancelled || entry.Result != "" {
		t.Fatalf("late tool_end changed cancelled entry: %#v", entry)
	}
}

func TestEndToolUpdatesTranscriptEntryAfterAskClearsActiveTools(t *testing.T) {
	started := time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)
	ended := started.Add(2 * time.Second)
	entry := &toolview.Entry{ID: "ask-tool", Status: toolview.StatusRunning, StartedAt: started}
	block := &toolview.Block{Entries: map[string]*toolview.Entry{"ask-tool": entry}, Order: []string{"ask-tool"}}
	m := Model{
		Messages:       []Msg{{Role: "tool", Content: block}},
		ActiveTools:    map[string]*toolview.Entry{},
		ToolStartTimes: map[string]time.Time{},
	}

	m.EndTool(protocol.ToolEndParams{ID: "ask-tool", Result: `{"answer":"ok"}`}, "ask-tool", ended)

	if entry.Status != toolview.StatusDone {
		t.Fatalf("entry.Status = %v, want done", entry.Status)
	}
	if entry.Result != `{"answer":"ok"}` {
		t.Fatalf("entry.Result = %q", entry.Result)
	}
	if entry.EndedAt != ended {
		t.Fatalf("entry.EndedAt = %v, want %v", entry.EndedAt, ended)
	}
	if entry.Duration != 2*time.Second {
		t.Fatalf("entry.Duration = %v, want 2s", entry.Duration)
	}
	if _, ok := m.ActiveTools["ask-tool"]; ok {
		t.Fatalf("ActiveTools still contains ended tool")
	}
}
