package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/alanchenchen/suna/internal/protocol"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
)

// newMouseSelectChatForTest 构造一个带固定 transcript 内容的 Chat TUI，便于拖选测试。
func newMouseSelectChatForTest(t *testing.T) *TUI {
	t.Helper()
	tui := &TUI{i18n: newTranslator(LocaleZH), mode: uipage.Chat, ready: true, width: 80, height: 24, currentSession: protocol.SessionInfo{ID: "session-1", Status: protocol.SessionStatusRunning}}
	tui.initChatComponents()
	// 三条消息：user 短文本、assistant 较长文本、system 带样式，验证列切割和样式剥离。
	tui.appendNonToolMessage(chatMsg{Role: "user", Content: "hello suna"})
	tui.appendNonToolMessage(chatMsg{Role: "assistant", Content: "line two content here"})
	tui.appendNonToolMessage(chatMsg{Role: "system", Content: "line three"})
	tui.layoutChat()
	tui.syncContent()
	return tui
}

// viewportLineScreenY 返回 viewport 内容第 contentLine 行对应的屏幕行号。
func viewportLineScreenY(tui *TUI, contentLine int) int {
	return contentLine + transcriptViewportTop
}

// findContentLine 返回第一条包含 want 的 viewport 内容行号，找不到返回 -1。
func findContentLine(t *testing.T, tui *TUI, want string) int {
	t.Helper()
	for i, l := range strings.Split(tui.chat.Viewport.GetContent(), "\n") {
		if strings.Contains(ansi.Strip(l), want) {
			return i
		}
	}
	t.Fatalf("content line containing %q not found", want)
	return -1
}

func TestMouseSelectionStartsOnLeftClickInViewport(t *testing.T) {
	tui := newMouseSelectChatForTest(t)
	// 屏幕行 5（viewport 顶部 4 行之后第一行）按下左键。
	consumed := tui.handleMouseSelection(tea.MouseClickMsg(tea.Mouse{X: 2, Y: 5, Button: tea.MouseLeft}))
	if !consumed {
		t.Fatal("left click in viewport should start selection")
	}
	if !tui.mouseSel.dragging {
		t.Fatal("mouseSel.dragging = false after left click, want true")
	}
	if tui.mouseSel.anchorX != 2 || tui.mouseSel.anchorY != 5 {
		t.Fatalf("anchor = (%d,%d), want (2,5)", tui.mouseSel.anchorX, tui.mouseSel.anchorY)
	}
}

func TestMouseSelectionIgnoresComposerClick(t *testing.T) {
	tui := newMouseSelectChatForTest(t)
	// 屏幕底部（composer 区域）按下左键不应启动拖选。
	consumed := tui.handleMouseSelection(tea.MouseClickMsg(tea.Mouse{X: 2, Y: tui.height - 1, Button: tea.MouseLeft}))
	if consumed {
		t.Fatal("left click in composer should not start selection")
	}
	if tui.mouseSel.dragging {
		t.Fatal("mouseSel.dragging = true after composer click, want false")
	}
}

func TestMouseSelectionIgnoresNonLeftClick(t *testing.T) {
	tui := newMouseSelectChatForTest(t)
	for _, btn := range []tea.MouseButton{tea.MouseRight, tea.MouseMiddle, tea.MouseWheelUp} {
		consumed := tui.handleMouseSelection(tea.MouseClickMsg(tea.Mouse{X: 2, Y: 5, Button: btn}))
		if consumed {
			t.Fatalf("button %v click should not start selection", btn)
		}
	}
	if tui.mouseSel.dragging {
		t.Fatal("mouseSel.dragging = true after non-left click, want false")
	}
}

func TestMouseSelectionDisabledWhenOverlayOpen(t *testing.T) {
	tui := newMouseSelectChatForTest(t)
	// 打开工具详情 overlay 后，viewport 内左键点击不应启动拖选。
	tui.chat.ShowToolDetail = true
	consumed := tui.handleMouseSelection(tea.MouseClickMsg(tea.Mouse{X: 2, Y: 5, Button: tea.MouseLeft}))
	if consumed {
		t.Fatal("left click should not start selection while overlay is open")
	}
	if tui.mouseSel.dragging {
		t.Fatal("mouseSel.dragging = true with overlay open, want false")
	}
}

func TestMouseSelectionDragUpdatesCursor(t *testing.T) {
	tui := newMouseSelectChatForTest(t)
	tui.handleMouseSelection(tea.MouseClickMsg(tea.Mouse{X: 2, Y: 5, Button: tea.MouseLeft}))
	consumed := tui.handleMouseSelection(tea.MouseMotionMsg(tea.Mouse{X: 10, Y: 7}))
	if !consumed {
		t.Fatal("motion during drag should be consumed")
	}
	if tui.mouseSel.cursorX != 10 || tui.mouseSel.cursorY != 7 {
		t.Fatalf("cursor = (%d,%d), want (10,7)", tui.mouseSel.cursorX, tui.mouseSel.cursorY)
	}
}

func TestMouseSelectionReleaseCopiesText(t *testing.T) {
	tui := newMouseSelectChatForTest(t)
	// 全选包含 "hello suna" 的行。
	row := viewportLineScreenY(tui, findContentLine(t, tui, "hello suna"))
	tui.handleMouseSelection(tea.MouseClickMsg(tea.Mouse{X: 0, Y: row, Button: tea.MouseLeft}))
	tui.handleMouseSelection(tea.MouseMotionMsg(tea.Mouse{X: 50, Y: row}))
	var copied string
	origWrite := clipboardWriteText
	clipboardWriteText = func(s string) bool { copied = s; return true }
	defer func() { clipboardWriteText = origWrite }()

	consumed := tui.handleMouseSelection(tea.MouseReleaseMsg(tea.Mouse{X: 50, Y: row}))
	if !consumed {
		t.Fatal("release during drag should be consumed")
	}
	if tui.mouseSel.dragging {
		t.Fatal("mouseSel.dragging = true after release, want false")
	}
	if copied == "" {
		t.Fatal("release should copy selected text")
	}
	if !strings.Contains(copied, "hello suna") {
		t.Fatalf("copied = %q, want contains %q", copied, "hello suna")
	}
	if !tui.mouseCopyFlashActive() {
		t.Fatal("copied flash should be active after successful copy")
	}
}

func TestMouseSelectionMultiLineCopy(t *testing.T) {
	tui := newMouseSelectChatForTest(t)
	// 选 "hello suna" 行到 "line two" 行，x 0..50。
	row0 := viewportLineScreenY(tui, findContentLine(t, tui, "hello suna"))
	row2 := viewportLineScreenY(tui, findContentLine(t, tui, "line two"))
	tui.handleMouseSelection(tea.MouseClickMsg(tea.Mouse{X: 0, Y: row0, Button: tea.MouseLeft}))
	tui.handleMouseSelection(tea.MouseMotionMsg(tea.Mouse{X: 50, Y: row2}))
	var copied string
	origWrite := clipboardWriteText
	clipboardWriteText = func(s string) bool { copied = s; return true }
	defer func() { clipboardWriteText = origWrite }()

	tui.handleMouseSelection(tea.MouseReleaseMsg(tea.Mouse{X: 50, Y: row2}))
	lines := strings.Split(copied, "\n")
	if len(lines) < 3 {
		t.Fatalf("copied = %q, want 3 lines", copied)
	}
	// 中间可能夹着 Suna header 等渲染行，只验证首尾内容存在。
	if !strings.Contains(lines[0], "hello suna") {
		t.Fatalf("line 0 = %q, want contains %q", lines[0], "hello suna")
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, "line two") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("copied = %q, want one line containing %q", copied, "line two")
	}
}

func TestMouseSelectionStripsANSI(t *testing.T) {
	tui := newMouseSelectChatForTest(t)
	// 选 "line three" 行（system 消息带样式）。
	row := viewportLineScreenY(tui, findContentLine(t, tui, "line three"))
	tui.handleMouseSelection(tea.MouseClickMsg(tea.Mouse{X: 0, Y: row, Button: tea.MouseLeft}))
	tui.handleMouseSelection(tea.MouseMotionMsg(tea.Mouse{X: 50, Y: row}))
	var copied string
	origWrite := clipboardWriteText
	clipboardWriteText = func(s string) bool { copied = s; return true }
	defer func() { clipboardWriteText = origWrite }()

	tui.handleMouseSelection(tea.MouseReleaseMsg(tea.Mouse{X: 50, Y: row}))
	if strings.Contains(copied, "\x1b[") {
		t.Fatalf("copied = %q, should not contain ANSI escapes", copied)
	}
	if !strings.Contains(copied, "line three") {
		t.Fatalf("copied = %q, want contains %q", copied, "line three")
	}
}

func TestMouseSelectionNoCopyOnEmpty(t *testing.T) {
	tui := newMouseSelectChatForTest(t)
	// 在 viewport 底部之外（无内容区域）拖选，不应复制。
	tui.handleMouseSelection(tea.MouseClickMsg(tea.Mouse{X: 0, Y: 22, Button: tea.MouseLeft}))
	tui.handleMouseSelection(tea.MouseMotionMsg(tea.Mouse{X: 5, Y: 23}))
	var copied string
	origWrite := clipboardWriteText
	clipboardWriteText = func(s string) bool { copied = s; return true }
	defer func() { clipboardWriteText = origWrite }()

	tui.handleMouseSelection(tea.MouseReleaseMsg(tea.Mouse{X: 5, Y: 23}))
	if copied != "" {
		t.Fatalf("copied = %q, want empty for empty region", copied)
	}
	if tui.mouseCopyFlashActive() {
		t.Fatal("copied flash should not be active when nothing copied")
	}
}

func TestMouseCopyFlashExpires(t *testing.T) {
	tui := newMouseSelectChatForTest(t)
	tui.mouseSel.copiedUntil = time.Now().Add(-time.Second)
	if tui.mouseCopyFlashActive() {
		t.Fatal("copied flash should be expired")
	}
}

func TestTranscriptLineAtScreenY(t *testing.T) {
	tui := newMouseSelectChatForTest(t)
	// 屏幕行 5（viewport 顶 4 行后第一行）应返回第一行内容。
	line := tui.transcriptLineAtScreenY(5)
	if line == "" {
		t.Fatal("transcriptLineAtScreenY(5) = empty, want first line")
	}
	// 屏幕行 4（分隔线）应为空。
	if got := tui.transcriptLineAtScreenY(4); got != "" {
		t.Fatalf("transcriptLineAtScreenY(4) = %q, want empty", got)
	}
}

func TestHighlightLineRange(t *testing.T) {
	line := "hello world"
	// 选中 [0, 4]（hello）应包裹反色。
	hl := highlightLineRange(line, 0, 4)
	if !strings.Contains(hl, "\x1b[7mhello\x1b[27m") {
		t.Fatalf("highlightLineRange = %q, want reverse-video hello", hl)
	}
	if !strings.Contains(hl, " world") {
		t.Fatalf("highlightLineRange = %q, want remainder preserved", hl)
	}
	// 选中 [6, 10]（world）。
	hl = highlightLineRange(line, 6, 10)
	if !strings.Contains(hl, "\x1b[7mworld\x1b[27m") {
		t.Fatalf("highlightLineRange = %q, want reverse-video world", hl)
	}
	// 越界列不应破坏行。
	hl = highlightLineRange(line, 100, 200)
	if hl != line {
		t.Fatalf("highlightLineRange out-of-range = %q, want unchanged %q", hl, line)
	}
}

func TestApplyMouseSelectionHighlight(t *testing.T) {
	tui := newMouseSelectChatForTest(t)
	// 未拖选时不改变内容。
	content := tui.chat.Viewport.View()
	if got := tui.applyMouseSelectionHighlight(content); got != content {
		t.Fatal("applyMouseSelectionHighlight without drag should return content unchanged")
	}
	// 拖选中：选 hello suna 行（屏幕行 5）的 [2, 12]。
	tui.handleMouseSelection(tea.MouseClickMsg(tea.Mouse{X: 2, Y: 5, Button: tea.MouseLeft}))
	tui.handleMouseSelection(tea.MouseMotionMsg(tea.Mouse{X: 12, Y: 5}))
	hl := tui.applyMouseSelectionHighlight(content)
	if !strings.Contains(hl, "\x1b[7m") {
		t.Fatal("applyMouseSelectionHighlight should add reverse-video during drag")
	}
	// 未选中行不应有反色。
	lines := strings.Split(hl, "\n")
	if strings.Contains(lines[6], "\x1b[7m") {
		t.Fatal("line outside selection should not be highlighted")
	}
}

func TestMouseSelectionFullUpdateFlow(t *testing.T) {
	tui := newMouseSelectChatForTest(t)
	var copied string
	origWrite := clipboardWriteText
	clipboardWriteText = func(s string) bool { copied = s; return true }
	defer func() { clipboardWriteText = origWrite }()

	row := viewportLineScreenY(tui, findContentLine(t, tui, "hello suna"))
	// 完整事件流：按下 → 拖动 → 松开，全部走 Update 分发。
	_, _ = tui.Update(tea.MouseClickMsg(tea.Mouse{X: 0, Y: row, Button: tea.MouseLeft}))
	_, _ = tui.Update(tea.MouseMotionMsg(tea.Mouse{X: 60, Y: row}))
	if !tui.mouseSel.dragging {
		t.Fatal("dragging = false after click+motion via Update")
	}
	if view := tui.viewChat(); !strings.Contains(view, "\x1b[7m") {
		t.Fatal("view during drag should contain reverse-video highlight")
	}
	_, _ = tui.Update(tea.MouseReleaseMsg(tea.Mouse{X: 60, Y: row}))
	if tui.mouseSel.dragging {
		t.Fatal("dragging = true after release via Update")
	}
	if !strings.Contains(copied, "hello suna") {
		t.Fatalf("copied = %q, want contains %q", copied, "hello suna")
	}
	if !tui.mouseCopyFlashActive() {
		t.Fatal("copied flash should be active after release")
	}
	if view := tui.viewChat(); !strings.Contains(view, "已复制") {
		t.Fatal("view after copy should show copied hint")
	}
}
