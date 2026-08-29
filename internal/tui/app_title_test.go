package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/alanchenchen/suna/internal/protocol"
)

// statusBarCWD 显示当前会话项目目录（basename），无会话目录时回退启动目录。
func TestStatusBarCWDShowsSessionWorkspace(t *testing.T) {
	tui := &TUI{currentSession: protocol.SessionInfo{ID: "session-1", CWD: "/Users/example/projects/suna-app"}}
	got := tui.statusBarCWD(100)
	if !strings.Contains(got, "suna-app") {
		t.Fatalf("statusBarCWD() = %q, want contains suna-app", got)
	}
}

func TestStatusBarCWDFallsBackToLaunchWorkspace(t *testing.T) {
	tui := &TUI{launchCWD: "/Users/example/projects/launcher"}
	got := tui.statusBarCWD(100)
	if !strings.Contains(got, "launcher") {
		t.Fatalf("statusBarCWD() = %q, want contains launcher", got)
	}
}

func TestStatusBarCWDEmptyWhenNoWorkspace(t *testing.T) {
	tui := &TUI{}
	if got := tui.statusBarCWD(100); got != "" {
		t.Fatalf("statusBarCWD() = %q, want empty", got)
	}
}

// 长目录名在窄宽度下截断，不溢出。
func TestStatusBarCWDTruncatesLongWorkspace(t *testing.T) {
	tui := &TUI{currentSession: protocol.SessionInfo{ID: "session-1", CWD: "/Users/example/projects/very-long-project-name-2026"}}
	got := tui.statusBarCWD(20)
	if lipgloss.Width(got) > 20 {
		t.Fatalf("statusBarCWD() width = %d, want <= 20", lipgloss.Width(got))
	}
}

// 宽度不足时隐藏 cwd（返回空串），不挤压 ctx 与用量。
func TestStatusBarCWDHiddenWhenTooNarrow(t *testing.T) {
	tui := &TUI{currentSession: protocol.SessionInfo{ID: "session-1", CWD: "/Users/example/projects/suna-app"}}
	if got := tui.statusBarCWD(2); got != "" {
		t.Fatalf("statusBarCWD() = %q, want empty when too narrow", got)
	}
}

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
