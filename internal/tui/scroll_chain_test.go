package tui

import (
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/tui/components/toolview"
)

// 滚动链：内层窗口到达边界后必须把滚动透传给 transcript，
// 否则视窗会被展开块/结果窗口"卡住"，用户无法滚动查看其它内容。
func TestExpandedBlockDetailScrollChainsAtBoundary(t *testing.T) {
	tui := newEdgeTUI(t)
	entry := &toolEntry{
		ID:      "t1",
		RawName: "readfile",
		Status:  toolview.StatusDone,
		Intent:  "读取文件",
		Result:  strings.Repeat("line\n", 40),
	}
	block := &toolview.Block{Order: []string{"t1"}, Entries: map[string]*toolview.Entry{"t1": entry}}
	tui.chat.Messages = append(tui.chat.Messages, chatMsg{Role: "tool", Content: block})
	tui.chat.ExpandedBlock = block
	tui.chat.ExpandedBoxKind = "tool"
	tui.chat.ExpandedBlockDetailScroll = 0

	if !tui.scrollExpandedBlockDetail(3) {
		t.Fatal("scrolling down within detail window should be consumed")
	}
	for i := 0; i < 200; i++ {
		tui.scrollExpandedBlockDetail(3)
	}
	if tui.scrollExpandedBlockDetail(3) {
		t.Fatal("scrolling past the detail window end must not be consumed (scroll chaining)")
	}
	for i := 0; i < 200; i++ {
		tui.scrollExpandedBlockDetail(-3)
	}
	if tui.scrollExpandedBlockDetail(-3) {
		t.Fatal("scrolling past the detail window start must not be consumed (scroll chaining)")
	}
}

// 展开块滚出视窗后不应继续接管滚动：用户此时意图是滚动 transcript。
func TestExpandedBlockScrollYieldsWhenOffscreen(t *testing.T) {
	tui := newEdgeTUI(t)
	entry := &toolEntry{
		ID:      "t1",
		RawName: "readfile",
		Status:  toolview.StatusDone,
		Result:  strings.Repeat("line\n", 10),
	}
	block := &toolview.Block{Order: []string{"t1"}, Entries: map[string]*toolview.Entry{"t1": entry}}
	tui.chat.Messages = append(tui.chat.Messages, chatMsg{Role: "tool", Content: block})
	tui.chat.ExpandedBlock = block
	tui.chat.ExpandedBoxKind = "tool"
	tui.syncContent()

	// 尚无 transcript 窗口 → 不可见，不接管滚动。
	tui.chat.TranscriptWindowStart = 0
	tui.chat.TranscriptWindowEnd = 0
	if tui.expandedBlockVisible() {
		t.Fatal("block must not be visible before a transcript window exists")
	}

	// 窗口覆盖该块 → 可见，应接管。
	tui.chat.TranscriptWindowStart = 0
	tui.chat.TranscriptWindowEnd = 1 << 20
	if !tui.expandedBlockVisible() {
		t.Fatal("block must be visible when the transcript window covers it")
	}
}

func TestWheelDeltaDirections(t *testing.T) {
	if got := wheelDelta(true, false); got != -3 {
		t.Fatalf("wheel up: got %d, want -3", got)
	}
	if got := wheelDelta(false, true); got != 3 {
		t.Fatalf("wheel down: got %d, want 3", got)
	}
	if got := wheelDelta(false, false); got != 0 {
		t.Fatalf("no direction: got %d, want 0", got)
	}
}

// 结果小节到达边界后同样透传滚动。
func TestSubtaskResultScrollChainsAtBoundary(t *testing.T) {
	tui := newEdgeTUI(t)
	sub := &toolEntry{
		ID:        "s1",
		RawName:   "spawn",
		Status:    toolview.StatusDone,
		ParamsRaw: map[string]any{"task": "分析"},
		Result:    `{"status":"completed","result":"` + strings.Repeat("result line content\\n", 40) + `"}`,
	}
	block := &toolview.Block{Order: []string{"s1"}, Entries: map[string]*toolview.Entry{"s1": sub}}
	tui.chat.Messages = append(tui.chat.Messages, chatMsg{Role: "tool", Content: block})
	tui.chat.ExpandedBlock = block
	// 纯 subtask 块：结果小节由 subtask 面板渲染，因此展开的是 subtask 盒子。
	tui.chat.ExpandedBoxKind = "subtask"
	tui.chat.SubtaskResultScroll = 0

	if !tui.subtaskResultScrollable() {
		t.Fatal("precondition: long result must be scrollable")
	}
	if !tui.scrollSubtaskResult(3) {
		t.Fatal("scrolling within result window should be consumed")
	}
	for i := 0; i < 300; i++ {
		tui.scrollSubtaskResult(3)
	}
	if tui.scrollSubtaskResult(3) {
		t.Fatal("scrolling past the result end must not be consumed (scroll chaining)")
	}
}
