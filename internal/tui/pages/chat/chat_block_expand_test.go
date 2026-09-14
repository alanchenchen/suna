package chat

import (
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/tui/components/toolview"
)

// newExpandTestModel 构造一个带视窗高度的最小 Model，供块展开相关测试使用。
func newExpandTestModel(t *testing.T) *Model {
	t.Helper()
	m := &Model{}
	m.Viewport.SetHeight(20)
	m.Viewport.SetWidth(80)
	return m
}

// appendToolBlock 追加一个 tool 块并同步 transcript 块表，返回该块。
func appendToolBlock(m *Model, entries map[string]*toolview.Entry, order ...string) *toolview.Block {
	block := &toolview.Block{Order: order, Entries: entries}
	m.Messages = append(m.Messages, Msg{Role: "tool", Content: block})
	// BoxKind 必须与生产路径一致：Ctrl+T 的目标定位按盒子类型区分，
	// 否则定位循环会跳过该块（boxKind == ""）。
	kind := boxKindTool
	if len(toolview.VisibleMainEntries(block)) == 0 {
		kind = boxKindSubtask
	}
	m.TranscriptBlocks = append(m.TranscriptBlocks, transcriptBlock{MsgIndex: len(m.Messages) - 1, LineCount: 3, BoxKind: kind})
	m.TranscriptTotalLines += 3
	return block
}

func addToolBlock(m *Model, id string) *toolview.Block {
	return appendToolBlock(m, map[string]*toolview.Entry{
		id: {ID: id, Name: "readfile", Status: toolview.StatusDone},
	}, id)
}

func addSubtaskOnlyBlock(m *Model, id string) *toolview.Block {
	return appendToolBlock(m, map[string]*toolview.Entry{
		id: {ID: id, Name: "spawn", RawName: "spawn", Status: toolview.StatusDone},
	}, id)
}

func TestToggleBlockDetailExpandsNearestVisibleBlock(t *testing.T) {
	m := newExpandTestModel(t)
	block := addToolBlock(m, "t1")

	if m.ExpandedBlock != nil {
		t.Fatal("expected no expanded block initially")
	}
	_, changed := m.ToggleVisibleBlockDetail()
	if !changed {
		t.Fatal("expected toggle to expand a block")
	}
	if m.ExpandedBlock != block {
		t.Fatal("expected the visible tool block to expand")
	}
}

func TestToggleBlockDetailCollapsesSameBlock(t *testing.T) {
	m := newExpandTestModel(t)
	addToolBlock(m, "t1")
	m.ToggleVisibleBlockDetail()

	_, changed := m.ToggleVisibleBlockDetail()
	if !changed {
		t.Fatal("expected toggle to report change")
	}
	if m.ExpandedBlock != nil {
		t.Fatal("expected second toggle to collapse")
	}
}

func TestToggleBlockDetailSkipsNonToolBlocks(t *testing.T) {
	m := newExpandTestModel(t)
	m.Messages = append(m.Messages, Msg{Role: "user", Content: "hello"})
	m.TranscriptBlocks = append(m.TranscriptBlocks, transcriptBlock{MsgIndex: 0, LineCount: 2})
	m.TranscriptTotalLines = 2

	if _, changed := m.ToggleVisibleBlockDetail(); changed {
		t.Fatal("expected no expandable block")
	}
}

func TestMoveExpandedBlockCursorClampsWithinEntries(t *testing.T) {
	m := newExpandTestModel(t)
	appendToolBlock(m, map[string]*toolview.Entry{
		"t1": {ID: "t1", Name: "readfile", Status: toolview.StatusDone},
		"t2": {ID: "t2", Name: "grep", Status: toolview.StatusDone},
	}, "t1", "t2")
	m.ToggleVisibleBlockDetail()

	m.MoveExpandedBlockCursor(-1)
	if m.ExpandedBlockCursor != 0 {
		t.Fatalf("expected cursor clamped at 0, got %d", m.ExpandedBlockCursor)
	}
	m.MoveExpandedBlockCursor(1)
	if m.ExpandedBlockCursor != 1 {
		t.Fatalf("expected cursor at 1, got %d", m.ExpandedBlockCursor)
	}
	m.MoveExpandedBlockCursor(5)
	if m.ExpandedBlockCursor != 1 {
		t.Fatalf("expected cursor clamped at last entry, got %d", m.ExpandedBlockCursor)
	}
}

func TestToggleBlockDetailResetsSubtaskPanelState(t *testing.T) {
	m := newExpandTestModel(t)
	block := addToolBlock(m, "t1")
	m.SubtaskToolDetailExpanded = true
	m.SubtaskToolDetailScroll = 7

	if _, changed := m.ToggleVisibleBlockDetail(); !changed {
		t.Fatal("expected block to expand")
	}
	if m.ExpandedBlock != block {
		t.Fatal("expected block expanded")
	}
	// 切换展开目标时必须清掉上一个块的面板状态，否则残留的展开态会作用到新块。
	if m.SubtaskToolDetailExpanded {
		t.Fatal("expected subtask tool detail collapsed on expand switch")
	}
	if m.SubtaskToolDetailScroll != 0 {
		t.Fatalf("expected scroll reset, got %d", m.SubtaskToolDetailScroll)
	}
}

func TestExpandedBlockEntriesPrefersMainEntries(t *testing.T) {
	m := newExpandTestModel(t)
	appendToolBlock(m, map[string]*toolview.Entry{
		"t1": {ID: "t1", Name: "readfile", Status: toolview.StatusDone},
		"s1": {ID: "s1", Name: "spawn", RawName: "spawn", Status: toolview.StatusDone},
	}, "t1", "s1")
	m.ToggleVisibleBlockDetail()

	entries := m.ExpandedBlockEntries()
	if len(entries) != 1 || entries[0].ID != "t1" {
		t.Fatalf("expected only main entries, got %d", len(entries))
	}
}

// TestSubtaskOnlyBlockStillExpandable 保证纯 subtask 块能通过 Ctrl+T 展开：
// 子任务结果恰好在根调用结束时到达（此时 CurrentToolBlock 已置空），
// 若此处不可展开，结果将永远无法查看。
func TestSubtaskOnlyBlockStillExpandable(t *testing.T) {
	m := newExpandTestModel(t)
	block := addSubtaskOnlyBlock(m, "s1")

	if _, changed := m.ToggleVisibleBlockDetail(); !changed {
		t.Fatal("expected subtask-only block to be expandable")
	}
	if m.ExpandedBlock != block {
		t.Fatal("expected subtask-only block expanded")
	}
	if !hasSubtaskEntries(m.ExpandedBlock) {
		t.Fatal("expected expanded block to expose subtask entries")
	}
	if got := m.ActiveSubtaskBlock(); got != block {
		t.Fatal("expected subtask panel to activate for expanded block")
	}
}

func TestBlockTitleCarriesNoHintByItself(t *testing.T) {
	// 提示由渲染层按块状态追加，BlockTitle 只负责计数信息，
	// 保证块渲染结果不依赖视窗位置（滚动签名可复用）。
	entries := []*toolview.Entry{{ID: "t1", Name: "readfile", Status: toolview.StatusDone}}
	title := toolview.BlockTitle(entries, toolview.RenderLabels{})
	if strings.Contains(title, "Ctrl+T") {
		t.Fatalf("expected no hint in block title, got %q", title)
	}
}

// TestExpandedBlockMainEntriesDistinguishesSubtaskOnly 保证滚轮/↑↓ 只在
// 含主条目的展开块上接管：纯 subtask 块没有可渲染的详情窗口，
// 若在此消耗滚轮或方向键，用户会看到"操作无反应"。
func TestExpandedBlockMainEntriesDistinguishesSubtaskOnly(t *testing.T) {
	mainModel := newExpandTestModel(t)
	addToolBlock(mainModel, "t1")
	mainModel.ToggleVisibleBlockDetail()
	if len(toolview.VisibleMainEntries(mainModel.ExpandedBlock)) == 0 {
		t.Fatal("expected main-entry block to expose main entries")
	}

	subtaskModel := newExpandTestModel(t)
	addSubtaskOnlyBlock(subtaskModel, "s1")
	subtaskModel.ToggleVisibleBlockDetail()
	if len(toolview.VisibleMainEntries(subtaskModel.ExpandedBlock)) != 0 {
		t.Fatal("expected subtask-only block to expose no main entries")
	}
}
