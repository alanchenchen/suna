package tui

import (
	"testing"

	"github.com/alanchenchen/suna/internal/protocol"
)

func TestWindowTitleUsesCurrentSessionWorkspaceAndState(t *testing.T) {
	tui := &TUI{currentSession: protocol.SessionInfo{ID: "session-1", CWD: "/Users/example/projects/suna-app"}}
	if got, want := tui.windowTitle(), "suna-app · idle"; got != want {
		t.Fatalf("windowTitle() = %q, want %q", got, want)
	}

	tui.chat.Loading = true
	if got, want := tui.windowTitle(), "suna-app · working"; got != want {
		t.Fatalf("windowTitle() = %q, want %q", got, want)
	}

	tui.chat.Loading = false
	tui.currentSession.Status = protocol.SessionStatusRunning
	if got, want := tui.windowTitle(), "suna-app · working"; got != want {
		t.Fatalf("windowTitle() from session status = %q, want %q", got, want)
	}
}

func TestWindowTitleFallsBackToCachedLaunchWorkspace(t *testing.T) {
	tui := &TUI{launchCWD: "/Users/example/projects/launcher"}
	if got, want := tui.windowTitle(), "launcher · idle"; got != want {
		t.Fatalf("windowTitle() = %q, want %q", got, want)
	}
}

func TestWindowTitleSupportsUnicodeWorkspace(t *testing.T) {
	tui := &TUI{currentSession: protocol.SessionInfo{ID: "session-1", CWD: "/workspace/苏纳应用"}}
	if got, want := tui.windowTitle(), "苏纳应用 · idle"; got != want {
		t.Fatalf("windowTitle() = %q, want %q", got, want)
	}
}

func TestWindowTitleRemovesControlCharacters(t *testing.T) {
	tui := &TUI{currentSession: protocol.SessionInfo{ID: "session-1", CWD: "/workspace/su\x1bna"}}
	if got, want := tui.windowTitle(), "suna · idle"; got != want {
		t.Fatalf("windowTitle() = %q, want %q", got, want)
	}
}

func TestViewSetsWindowTitle(t *testing.T) {
	tui := &TUI{currentSession: protocol.SessionInfo{ID: "session-1", CWD: "/workspace/demo"}}
	if got, want := tui.View().WindowTitle, "demo · idle"; got != want {
		t.Fatalf("View().WindowTitle = %q, want %q", got, want)
	}
}
