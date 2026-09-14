package chat

import (
	"testing"

	"github.com/alanchenchen/suna/internal/tui/components/toolview"
)

// TestToolAndSubtaskBoxesExpandIndependently 保证同一个 tool 消息渲染出的
// tool 盒子与 subtask 面板能各自独立展开：Ctrl+T 只切换视窗内最近的盒子，
// 展开 tool 盒子不应连带激活 subtask 面板，反之亦然。
func TestToolAndSubtaskBoxesExpandIndependently(t *testing.T) {
	m := newExpandTestModel(t)
	// 混合块：同时含主条目（普通工具）和 subtask 条目，会渲染出两个盒子。
	block := appendToolBlock(m, map[string]*toolview.Entry{
		"t1": {ID: "t1", Name: "readfile", Status: toolview.StatusDone},
		"s1": {ID: "s1", Name: "spawn", RawName: "spawn", Status: toolview.StatusDone},
	}, "t1", "s1")

	// 视窗内只有 tool 盒子（BoxKind=tool）时，Ctrl+T 展开 tool 盒子。
	m.TranscriptBlocks = []transcriptBlock{{MsgIndex: 0, LineCount: 3, BoxKind: boxKindTool}}
	m.TranscriptTotalLines = 3
	if _, changed := m.ToggleVisibleBlockDetail(); !changed {
		t.Fatal("expected tool box to expand")
	}
	if m.ExpandedBoxKind != boxKindTool {
		t.Fatalf("expected tool box kind, got %q", m.ExpandedBoxKind)
	}
	if m.ActiveSubtaskBlock() != nil {
		t.Fatal("expanding the tool box must not activate the subtask panel")
	}

	// 视窗内只有 subtask 盒子时，Ctrl+T 展开 subtask 面板（独立于 tool 盒子）。
	m.ExpandedBlock = nil
	m.ExpandedBoxKind = ""
	m.TranscriptBlocks = []transcriptBlock{{MsgIndex: 0, LineCount: 3, BoxKind: boxKindSubtask}}
	if _, changed := m.ToggleVisibleBlockDetail(); !changed {
		t.Fatal("expected subtask box to expand")
	}
	if m.ExpandedBoxKind != boxKindSubtask {
		t.Fatalf("expected subtask box kind, got %q", m.ExpandedBoxKind)
	}
	if m.ExpandedBlock != block {
		t.Fatal("expected the subtask panel's block to expand")
	}
	if m.ActiveSubtaskBlock() != block {
		t.Fatal("expanding the subtask box must activate the subtask panel")
	}
}
