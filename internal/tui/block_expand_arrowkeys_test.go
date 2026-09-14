package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/alanchenchen/suna/internal/tui/components/toolview"
)

// 就地展开不是模态：run 结束后通过 Ctrl+T 展开 subtask 盒子时，
// 若面板内没有可切换的工具，↑↓ 必须让位给输入历史，
// 而不是被消耗却没有任何可见反馈（隐形操作）。
func TestExpandedSubtaskWithoutToolsYieldsArrowKeys(t *testing.T) {
	tui := newEdgeTUI(t)
	block := &toolview.Block{Order: []string{"s1"}, Entries: map[string]*toolview.Entry{
		"s1": {ID: "s1", RawName: "spawn", Status: toolview.StatusDone},
	}}
	tui.chat.Messages = append(tui.chat.Messages, chatMsg{Role: "tool", Content: block})
	tui.chat.ExpandedBlock = block
	tui.chat.ExpandedBoxKind = "subtask"
	tui.chat.Textarea.SetValue("")

	if !tui.hasActiveSubtaskPanel() {
		t.Fatal("precondition: expanded subtask box must activate the panel")
	}
	if tui.canNavigateSubtaskTools() {
		t.Fatal("subtask without inner tools must not capture arrow keys")
	}
	if !tui.canBrowseInputHistory() {
		t.Fatal("arrow keys must fall through to input history")
	}
}

// 面板确实有内部工具时，↑↓ 仍应接管为工具导航（避免过度放宽）。
func TestExpandedSubtaskWithToolsKeepsArrowKeys(t *testing.T) {
	tui := newEdgeTUI(t)
	block := &toolview.Block{Order: []string{"s1", "c1"}, Entries: map[string]*toolview.Entry{
		"s1": {ID: "s1", RawName: "spawn", Status: toolview.StatusDone},
		"c1": {ID: "c1", RawName: "readfile", ParentID: "s1", Status: toolview.StatusDone},
	}}
	tui.chat.Messages = append(tui.chat.Messages, chatMsg{Role: "tool", Content: block})
	tui.chat.ExpandedBlock = block
	tui.chat.ExpandedBoxKind = "subtask"

	if !tui.canNavigateSubtaskTools() {
		t.Fatal("subtask with inner tools must capture arrow keys")
	}

	before := tui.chat.SubtaskToolCursor
	tui.updateChat(tea.KeyPressMsg{Code: tea.KeyDown})
	if tui.chat.SubtaskToolCursor == before && len(tui.selectedSubtaskTools()) > 1 {
		t.Fatal("arrow key should move the subtask tool cursor")
	}
}

// 展开态（CurrentToolBlock 已置空）仍要能列出子任务的内部工具：
// 否则 ↑↓/Enter 无法查看工具详情，面板也只显示"无工具"。
func TestExpandedSubtaskListsInnerTools(t *testing.T) {
	tui := newEdgeTUI(t)
	block := &toolview.Block{Order: []string{"s1", "c1"}, Entries: map[string]*toolview.Entry{
		"s1": {ID: "s1", RawName: "spawn", Status: toolview.StatusDone},
		"c1": {ID: "c1", RawName: "readfile", ParentID: "s1", Status: toolview.StatusDone},
	}}
	tui.chat.Messages = append(tui.chat.Messages, chatMsg{Role: "tool", Content: block})
	tui.chat.CurrentToolBlock = nil // run 已结束
	tui.chat.ExpandedBlock = block
	tui.chat.ExpandedBoxKind = "subtask"

	children := tui.selectedSubtaskTools()
	if len(children) != 1 || children[0].ID != "c1" {
		t.Fatalf("selectedSubtaskTools() = %v, want the c1 child of the expanded block", children)
	}
}
