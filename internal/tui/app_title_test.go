package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/alanchenchen/suna/internal/protocol"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
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

// working 态带 spinner 帧时标题含纯文本帧；idle 态忽略帧保持静态。
func TestWindowTitleWithFrameAnimatesWorkingState(t *testing.T) {
	tui := &TUI{currentSession: protocol.SessionInfo{ID: "session-1", CWD: "/Users/example/projects/suna-app"}}

	tui.chat.Loading = true
	if got, want := tui.windowTitleWithFrame("⠋"), "suna-app · ⠋ working"; got != want {
		t.Fatalf("windowTitleWithFrame() working = %q, want %q", got, want)
	}

	tui.chat.Loading = false
	if got, want := tui.windowTitleWithFrame("⠋"), "suna-app · idle"; got != want {
		t.Fatalf("windowTitleWithFrame() idle = %q, want %q", got, want)
	}
}

// spinner 未初始化时纯文本帧为空串，标题回退静态 working。
func TestLiveSpinnerFramePlainEmptyWhenUninitialized(t *testing.T) {
	tui := &TUI{}
	if got := tui.liveSpinnerFramePlain(); got != "" {
		t.Fatalf("liveSpinnerFramePlain() = %q, want empty when uninitialized", got)
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

// chat 模式下终端光标精确跟随 textarea 光标：IME 组合文本（拼音 preedit）由终端
// 绘制在光标位置，锚定 textarea 光标后组合文本始终显示在输入框内，
// 不会跟随 renderer 增量渲染的光标移动残留到 pet 等其他区域。
func TestViewAnchorsTerminalCursorToTextarea(t *testing.T) {
	tui := &TUI{ready: true, mode: uipage.Chat, width: 100, height: 40}
	tui.initChatComponents()
	v := tui.View()
	if v.Cursor == nil {
		t.Fatal("View().Cursor = nil, want anchored to textarea cursor")
	}
	// 光标必须落在输入区（屏幕下半部分），不能残留到 pet 等上方区域。
	if v.Cursor.Y < tui.height/2 {
		t.Fatalf("View().Cursor.Y = %d, want in input area (>= %d)", v.Cursor.Y, tui.height/2)
	}
}
