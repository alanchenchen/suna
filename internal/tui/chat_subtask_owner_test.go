package tui

import (
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/tui/components/toolview"
)

// 相同条目 ID 在历史块与当前块同时出现时，渲染与导航必须使用所属块。
func TestRootSubtaskOwnerIsolation(t *testing.T) {
	tui := newIncrementalTestTUI()
	history := &toolBlock{Entries: map[string]*toolEntry{}}
	history.Add(&toolEntry{ID: "same", RawName: "spawn", Intent: "历史任务", Status: toolDone})
	history.Add(&toolEntry{ID: "history-child", ParentID: "same", RawName: "readfile", Name: "历史工具", Status: toolDone})
	history.Add(&toolEntry{ID: "other", RawName: "spawn", Intent: "历史运行", Status: toolRunning})
	current := &toolBlock{Entries: map[string]*toolEntry{}}
	current.Add(&toolEntry{ID: "same", RawName: "spawn", Intent: "当前任务", Status: toolRunning})
	current.Add(&toolEntry{ID: "other", RawName: "spawn", Intent: "当前完成", Status: toolDone})
	current.Add(&toolEntry{ID: "current-child", ParentID: "same", RawName: "exec", Name: "当前工具", Status: toolRunning})
	tui.chat.Messages = append(tui.chat.Messages, chatMsg{Role: "tool", Content: history}, chatMsg{Role: "tool", Content: current})
	tui.chat.CurrentToolBlock = current
	panel := stripANSIForTest(tui.renderSubtaskBlock(history))
	if !strings.Contains(panel, "历史任务") || !strings.Contains(panel, "历史工具") || strings.Contains(panel, "当前任务") || strings.Contains(panel, "当前工具") {
		t.Fatalf("historical panel used unrelated owner: %q", panel)
	}
	done, running, failed := tui.subtaskStatusCounts(history, subtaskIDsInBlock(history))
	if done != 1 || running != 1 || failed != 0 {
		t.Fatalf("owner counts = %d/%d/%d", done, running, failed)
	}
	if got := tui.subtaskActivity(history, history.Entries["same"], 80); !strings.Contains(got, "历史工具") {
		t.Fatalf("historical activity = %q", got)
	}
	tui.chat.CurrentToolBlock = nil
	tui.chat.ExpandedBlock, tui.chat.ExpandedBoxKind = history, "subtask"
	if tui.defaultSubtaskCursor() != 1 {
		t.Fatal("default cursor used unrelated owner")
	}
	tui.chat.SubtaskCursor = 0
	tui.chat.SubtaskCursorUserSet = true
	if tui.selectedSubtask() != history.Entries["same"] {
		t.Fatal("selected subtask used unrelated owner")
	}
	if selected := tui.selectedSubtaskTool(); selected == nil || selected.ID != "history-child" {
		t.Fatalf("selected child = %#v", selected)
	}
	tui.chat.CurrentToolBlock = current
	if tui.selectedSubtask() != current.Entries["same"] {
		t.Fatal("current owner did not take priority")
	}
	if activity := tui.subtaskActivity(history, history.Entries["other"], 80); activity != tui.tr("tui.subtask_panel.waiting") {
		t.Fatalf("owner waiting label = %q", activity)
	}
	if activity := tui.subtaskActivity(history, &toolEntry{Status: toolview.StatusDone}, 80); activity != tui.tr("tui.subtask_panel.done") {
		t.Fatalf("owner done label = %q", activity)
	}
}
