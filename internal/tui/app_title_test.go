package tui

import (
	"testing"

	"github.com/alanchenchen/suna/internal/protocol"
)

func TestWindowTitleUsesCurrentSessionWorkspaceAndModel(t *testing.T) {
	tui := &TUI{
		currentSession: protocol.SessionInfo{ID: "session-1", CWD: "/Users/example/projects/suna"},
		modelName:      "gpt-test",
	}

	if got, want := tui.windowTitle(), "Suna — suna — gpt-test"; got != want {
		t.Fatalf("windowTitle() = %q, want %q", got, want)
	}
}

func TestWindowTitleOmitsMissingContext(t *testing.T) {
	tests := []struct {
		name string
		tui  TUI
		want string
	}{
		{name: "without session", want: "Suna"},
		{
			name: "without workspace",
			tui:  TUI{currentSession: protocol.SessionInfo{ID: "session-1"}, modelName: "gpt-test"},
			want: "Suna — gpt-test",
		},
		{
			name: "without model",
			tui:  TUI{currentSession: protocol.SessionInfo{ID: "session-1", CWD: "/workspace/demo"}},
			want: "Suna — demo",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.tui.windowTitle(); got != tt.want {
				t.Fatalf("windowTitle() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWindowTitleRemovesControlCharacters(t *testing.T) {
	tui := &TUI{
		currentSession: protocol.SessionInfo{ID: "session-1", CWD: "/workspace/su\x1bna"},
		modelName:      "model\x07name",
	}

	if got, want := tui.windowTitle(), "Suna — suna — modelname"; got != want {
		t.Fatalf("windowTitle() = %q, want %q", got, want)
	}
}

func TestWindowTitleMarksActiveRun(t *testing.T) {
	tui := &TUI{
		currentSession: protocol.SessionInfo{ID: "session-1", CWD: "/workspace/demo"},
		modelName:      "gpt-test",
	}

	tui.chat.Loading = true
	if got, want := tui.windowTitle(), "Suna — demo — gpt-test · working"; got != want {
		t.Fatalf("windowTitle() = %q, want %q", got, want)
	}

	tui.chat.Loading = false
	if got, want := tui.windowTitle(), "Suna — demo — gpt-test"; got != want {
		t.Fatalf("windowTitle() = %q, want %q", got, want)
	}
}

func TestViewSetsWindowTitle(t *testing.T) {
	tui := &TUI{
		currentSession: protocol.SessionInfo{ID: "session-1", CWD: "/workspace/demo"},
		modelName:      "gpt-test",
	}

	if got, want := tui.View().WindowTitle, "Suna — demo — gpt-test"; got != want {
		t.Fatalf("View().WindowTitle = %q, want %q", got, want)
	}
}
