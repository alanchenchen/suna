package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/alanchenchen/suna/internal/tui/components/toolview"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
)

func newEdgeTUI(t *testing.T) *TUI {
	t.Helper()
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 120, height: 40, mode: uipage.Chat}
	tui.initChatComponents()
	return tui
}

// 展开块是指向 Messages 内块的指针。任何重建 Messages 的路径都必须同时清空它，
// 否则会悬挂指向已丢弃的块（内存无法回收，Ctrl+T 指向不可见内容）。
func TestResetRuntimeClearsExpandedBlock(t *testing.T) {
	tui := newEdgeTUI(t)
	block := &toolview.Block{Order: []string{"t1"}, Entries: map[string]*toolview.Entry{}}
	tui.chat.ExpandedBlock = block
	tui.chat.ExpandedBoxKind = "tool"
	tui.chat.ExpandedBlockCursor = 2
	tui.chat.ExpandedBlockDetailScroll = 5

	tui.chat.ResetRuntime()

	if tui.chat.ExpandedBlock != nil {
		t.Fatal("ResetRuntime must clear ExpandedBlock to avoid dangling pointer")
	}
	if tui.chat.ExpandedBlockCursor != 0 || tui.chat.ExpandedBlockDetailScroll != 0 {
		t.Fatalf("cursor/scroll must reset, got cursor=%d scroll=%d",
			tui.chat.ExpandedBlockCursor, tui.chat.ExpandedBlockDetailScroll)
	}
}

// 纯 subtask 块没有可渲染的条目光标（tool 盒子为空），因此 Ctrl+T 展开后
// ↑↓ 不应被块内光标吃掉，必须让位给 subtask 面板自身的工具导航。
func TestExpandedSubtaskOnlyBlockYieldsArrowKeys(t *testing.T) {
	tui := newEdgeTUI(t)
	sub := &toolEntry{ID: "s1", Name: "Spawn", RawName: "spawn", Intent: "分析", Status: toolDone,
		Result: `{"status":"completed","result":"done"}`}
	block := &toolview.Block{Order: []string{"s1"}, Entries: map[string]*toolview.Entry{"s1": sub}}
	tui.chat.Messages = append(tui.chat.Messages, chatMsg{Role: "tool", Content: block})
	tui.chat.ExpandedBlock = block
	tui.chat.ExpandedBoxKind = "tool"

	if tui.expandedBlockHasMainEntries() {
		t.Fatal("subtask-only block must report no main entries")
	}

	tui.chat.ExpandedBlockCursor = 0
	_, _ = tui.updateChat(tea.KeyPressMsg{Code: tea.KeyDown})
	if tui.chat.ExpandedBlockCursor != 0 {
		t.Fatalf("arrow key must not move block cursor for subtask-only block, got %d",
			tui.chat.ExpandedBlockCursor)
	}
}

// 展开块含主条目时，↑↓ 仍应正常移动条目光标（确认上面的让位没有过度放宽）。
func TestExpandedMainEntryBlockKeepsArrowKeys(t *testing.T) {
	tui := newEdgeTUI(t)
	e1 := &toolEntry{ID: "t1", Name: "Readfile", RawName: "readfile", Status: toolDone}
	e2 := &toolEntry{ID: "t2", Name: "Grep", RawName: "grep", Status: toolDone}
	block := &toolview.Block{
		Order:   []string{"t1", "t2"},
		Entries: map[string]*toolview.Entry{"t1": e1, "t2": e2},
	}
	tui.chat.Messages = append(tui.chat.Messages, chatMsg{Role: "tool", Content: block})
	tui.chat.ExpandedBlock = block
	tui.chat.ExpandedBoxKind = "tool"

	if !tui.expandedBlockHasMainEntries() {
		t.Fatal("block with main entries must report them")
	}
	_, _ = tui.updateChat(tea.KeyPressMsg{Code: tea.KeyDown})
	if tui.chat.ExpandedBlockCursor != 1 {
		t.Fatalf("arrow key should move block cursor, got %d", tui.chat.ExpandedBlockCursor)
	}
}

// 展开块标题提示必须完整可见：主标题可截断，但提示不能被切掉（否则等于没有）。
func TestToolBlockHintNeverTruncated(t *testing.T) {
	te := &toolEntry{ID: "t1", Name: "Readfile", RawName: "readfile", Status: toolDone}
	block := &toolview.Block{Order: []string{"t1"}, Entries: map[string]*toolview.Entry{"t1": te}}
	for _, width := range []int{60, 80, 100, 140} {
		tui := &TUI{i18n: newTranslator(LocaleZH), width: width, height: 40, mode: uipage.Chat}
		tui.initChatComponents()
		deps := tui.toolRenderDeps()
		deps.ExpandedHint = "Ctrl+T"
		out := stripANSIForTest(toolview.RenderBlock(block, deps))
		if !strings.Contains(out, "Ctrl+T") {
			t.Fatalf("width %d: hint truncated, got %q", width, out)
		}
	}
}

// 纯 subtask 展开块没有可渲染的详情窗口，PgUp/PgDn 不应被它消耗，
// 否则用户会看到"按键无反应"；应放行给 transcript 翻页。
func TestExpandedSubtaskOnlyBlockYieldsPageKeys(t *testing.T) {
	tui := newEdgeTUI(t)
	sub := &toolEntry{ID: "s1", Name: "Spawn", RawName: "spawn", Intent: "分析", Status: toolDone,
		Result: `{"status":"completed","result":"done"}`}
	block := &toolview.Block{Order: []string{"s1"}, Entries: map[string]*toolview.Entry{"s1": sub}}
	tui.chat.Messages = append(tui.chat.Messages, chatMsg{Role: "tool", Content: block})
	tui.chat.ExpandedBlock = block
	tui.chat.ExpandedBoxKind = "tool"
	tui.chat.ExpandedBlockDetailScroll = 0

	_, _ = tui.updateChat(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if tui.chat.ExpandedBlockDetailScroll != 0 {
		t.Fatalf("page key must not scroll an unrendered detail window, got %d",
			tui.chat.ExpandedBlockDetailScroll)
	}
}

// 含主条目的展开块仍应让 PgUp/PgDn 滚动详情窗口（确认上面的让位没有过度放宽）。
func TestExpandedMainEntryBlockKeepsPageKeys(t *testing.T) {
	tui := newEdgeTUI(t)
	long := strings.Repeat("line\n", 200)
	e1 := &toolEntry{ID: "t1", Name: "Readfile", RawName: "readfile", Status: toolDone, Result: long}
	block := &toolview.Block{Order: []string{"t1"}, Entries: map[string]*toolview.Entry{"t1": e1}}
	tui.chat.Messages = append(tui.chat.Messages, chatMsg{Role: "tool", Content: block})
	tui.chat.ExpandedBlock = block
	tui.chat.ExpandedBoxKind = "tool"

	_, _ = tui.updateChat(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if tui.chat.ExpandedBlockDetailScroll == 0 {
		t.Fatal("page key should scroll detail window when block has main entries")
	}
}

// 结果小节超出可视行数时，PgUp/PgDn 必须优先滚动结果，
// 否则长结果永远看不到剩余内容（设计定稿的优先级第 ② 层）。
func TestSubtaskResultTakesPageKeysWhenScrollable(t *testing.T) {
	tui := newEdgeTUI(t)
	var sb strings.Builder
	for i := 0; i < 60; i++ {
		sb.WriteString(fmt.Sprintf("row-%02d\n", i))
	}
	sub := &toolEntry{ID: "s1", Name: "Spawn", RawName: "spawn", Intent: "分析", Status: toolDone,
		Result: `{"status":"completed","result":"` + strings.ReplaceAll(sb.String(), "\n", "\\n") + `"}`}
	block := &toolview.Block{Order: []string{"s1"}, Entries: map[string]*toolview.Entry{"s1": sub}}
	tui.chat.Messages = append(tui.chat.Messages, chatMsg{Role: "tool", Content: block})
	tui.chat.ExpandedBlock = block
	tui.chat.ExpandedBoxKind = "tool"
	tui.chat.CurrentToolBlock = block

	if !tui.subtaskResultScrollable() {
		t.Fatal("long result should be reported as scrollable")
	}
	_, _ = tui.updateChat(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if tui.chat.SubtaskResultScroll == 0 {
		t.Fatal("page key should scroll the result section when it overflows")
	}
}

// 结果区滚动宽度必须与渲染宽度一致；若滚动按更窄宽度 wrap，行数偏多会让
// maxOffset 偏大，滚动到最后一行时窗口会滑过内容尾部。
func TestSubtaskResultScrollWidthMatchesRenderWidth(t *testing.T) {
	tui := newEdgeTUI(t)
	sub := &toolEntry{ID: "s1", Name: "Spawn", RawName: "spawn", Intent: "分析", Status: toolDone,
		Result: `{"status":"completed","result":"line"}`}
	block := &toolview.Block{Order: []string{"s1"}, Entries: map[string]*toolview.Entry{"s1": sub}}
	tui.chat.Messages = append(tui.chat.Messages, chatMsg{Role: "tool", Content: block})
	tui.chat.ExpandedBlock = block
	tui.chat.ExpandedBoxKind = "tool"
	tui.chat.CurrentToolBlock = block

	// active 面板使用固定宽度，滚动量宽必须用同一公式。
	want := max(24, max(40, tui.width-8)-8)
	if got := tui.subtaskResultInnerWidth(); got != want {
		t.Fatalf("result scroll width = %d, want %d (must match active panel innerWidth)", got, want)
	}
}

// 结果未超出可视行数时不消耗 PgUp/PgDn，避免短结果下"按键无反应"。
func TestSubtaskResultYieldsPageKeysWhenShort(t *testing.T) {
	tui := newEdgeTUI(t)
	sub := &toolEntry{ID: "s1", Name: "Spawn", RawName: "spawn", Intent: "分析", Status: toolDone,
		Result: `{"status":"completed","result":"one line"}`}
	block := &toolview.Block{Order: []string{"s1"}, Entries: map[string]*toolview.Entry{"s1": sub}}
	tui.chat.Messages = append(tui.chat.Messages, chatMsg{Role: "tool", Content: block})
	tui.chat.ExpandedBlock = block
	tui.chat.ExpandedBoxKind = "tool"
	tui.chat.CurrentToolBlock = block

	if tui.subtaskResultScrollable() {
		t.Fatal("short result must not be reported as scrollable")
	}
	_, _ = tui.updateChat(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if tui.chat.SubtaskResultScroll != 0 {
		t.Fatalf("short result must not consume page keys, got scroll=%d", tui.chat.SubtaskResultScroll)
	}
}
