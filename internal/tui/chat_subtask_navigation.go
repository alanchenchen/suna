package tui

import (
	"strings"

	"github.com/alanchenchen/suna/internal/tui/components/toolview"
)

func (t *TUI) hasActiveSubtaskPanel() bool {
	return t.chat.VisibleSubtaskIDs() != nil
}

func (t *TUI) canToggleSubtaskDetailWithEnter() bool {
	return t.canUseSubtaskPanelKeys() &&
		len(t.chat.Attachments) == 0 &&
		len(t.chat.CmdSuggestions) == 0 &&
		!t.chat.HasBlockingInteraction()
}

// canUseSubtaskPanelKeys 判断 ↑↓/Tab 是否应交给 subtask 面板。
// 面板是随 run 自动出现的隐式模式，不能抢正在编辑的输入框：
// 草稿非空时 ↑↓ 必须留给文本光标，否则多行输入无法移动光标。
func (t *TUI) canUseSubtaskPanelKeys() bool {
	return t.hasActiveSubtaskPanel() && strings.TrimSpace(t.chat.Textarea.Value()) == ""
}

// canNavigateSubtaskTools 报告 ↑↓ 是否可以接管为子任务工具导航。
// 只有面板激活且确实有内部工具可切换时才接管，否则让位给输入历史：
// 就地展开不是模态，按键被消耗却没有任何可见反馈属于隐形操作。
func (t *TUI) canNavigateSubtaskTools() bool {
	return t.canUseSubtaskPanelKeys() && len(t.selectedSubtaskTools()) > 0
}

func (t *TUI) selectedSubtaskID() string {
	ids := t.chat.VisibleSubtaskIDs()
	if len(ids) == 0 {
		return ""
	}
	t.clampSubtaskCursor()
	return ids[t.chat.SubtaskCursor]
}

func (t *TUI) selectedSubtask() *toolEntry {
	block := t.chat.ActiveSubtaskBlock()
	if block == nil {
		return nil
	}
	return block.Entries[t.selectedSubtaskID()]
}

func (t *TUI) selectedSubtaskTools() []*toolEntry {
	parent := t.selectedSubtask()
	// 用 ActiveSubtaskBlock 而不是 CurrentToolBlock：run 结束后块仍在 Messages 里，
	// 用户通过 Ctrl+T 展开 subtask 盒子查看结果时，内部工具列表也必须可见。
	block := t.chat.ActiveSubtaskBlock()
	if parent == nil || block == nil {
		return nil
	}
	return toolview.SubtaskChildren(block, parent.ID)
}

func (t *TUI) selectedSubtaskTool() *toolEntry {
	children := t.selectedSubtaskTools()
	if len(children) == 0 {
		return nil
	}
	if !t.chat.SubtaskToolCursorUserSet {
		t.chat.SubtaskToolCursor = t.defaultSubtaskToolCursor()
	}
	t.clampSubtaskToolCursor()
	return children[t.chat.SubtaskToolCursor]
}

func (t *TUI) moveSubtaskCursor(delta int) {
	ids := t.chat.VisibleSubtaskIDs()
	if len(ids) == 0 {
		t.chat.SubtaskCursor = 0
		t.chat.SubtaskCursorUserSet = false
		t.chat.SubtaskToolCursor = 0
		t.chat.SubtaskToolCursorUserSet = false
		t.chat.SubtaskToolDetailScroll = 0
		t.chat.SubtaskResultScroll = 0
		return
	}
	t.chat.SubtaskCursor += delta
	t.chat.SubtaskCursorUserSet = true
	if t.chat.SubtaskCursor < 0 {
		t.chat.SubtaskCursor = len(ids) - 1
	}
	if t.chat.SubtaskCursor >= len(ids) {
		t.chat.SubtaskCursor = 0
	}
	t.chat.SubtaskToolCursor = t.defaultSubtaskToolCursor()
	t.chat.SubtaskToolCursorUserSet = false
	t.chat.SubtaskToolDetailScroll = 0
	t.chat.SubtaskResultScroll = 0
}

func (t *TUI) moveSubtaskToolCursor(delta int) {
	children := t.selectedSubtaskTools()
	if len(children) == 0 {
		t.chat.SubtaskToolCursor = 0
		t.chat.SubtaskToolCursorUserSet = false
		t.chat.SubtaskToolDetailScroll = 0
		t.chat.SubtaskResultScroll = 0
		return
	}
	t.chat.SubtaskToolCursor += delta
	t.chat.SubtaskToolCursorUserSet = true
	if t.chat.SubtaskToolCursor < 0 {
		t.chat.SubtaskToolCursor = 0
	}
	if t.chat.SubtaskToolCursor >= len(children) {
		t.chat.SubtaskToolCursor = len(children) - 1
	}
	t.chat.SubtaskToolDetailScroll = 0
	t.chat.SubtaskResultScroll = 0
}

func (t *TUI) clampSubtaskCursor() {
	ids := t.chat.VisibleSubtaskIDs()
	if len(ids) == 0 {
		t.chat.SubtaskCursor = 0
		return
	}
	if t.chat.SubtaskCursor < 0 {
		t.chat.SubtaskCursor = 0
	}
	if t.chat.SubtaskCursor >= len(ids) {
		t.chat.SubtaskCursor = len(ids) - 1
	}
}

func (t *TUI) clampSubtaskToolCursor() {
	children := t.selectedSubtaskTools()
	if len(children) == 0 {
		t.chat.SubtaskToolCursor = 0
		return
	}
	if t.chat.SubtaskToolCursor < 0 {
		t.chat.SubtaskToolCursor = 0
	}
	if t.chat.SubtaskToolCursor >= len(children) {
		t.chat.SubtaskToolCursor = len(children) - 1
	}
}

func (t *TUI) ensureSubtaskSelection() {
	if !t.chat.SubtaskCursorUserSet {
		t.chat.SubtaskCursor = t.defaultSubtaskCursor()
	}
	t.clampSubtaskCursor()
	if !t.chat.SubtaskToolCursorUserSet {
		t.chat.SubtaskToolCursor = t.defaultSubtaskToolCursor()
	}
	t.clampSubtaskToolCursor()
}

func (t *TUI) defaultSubtaskCursor() int {
	ids := t.chat.VisibleSubtaskIDs()
	if len(ids) == 0 {
		return 0
	}
	block := t.chat.ActiveSubtaskBlock()
	for i, id := range ids {
		if te := block.Entries[id]; te != nil && te.Status == toolview.StatusRunning {
			return i
		}
	}
	return 0
}

func (t *TUI) defaultSubtaskToolCursor() int {
	children := t.selectedSubtaskTools()
	if len(children) == 0 {
		return 0
	}
	lastDone := 0
	for i, child := range children {
		if child.Status == toolview.StatusRunning || child.Status == toolview.StatusCancelling {
			return i
		}
		if child.Status == toolview.StatusDone || child.Status == toolview.StatusError || child.Status == toolview.StatusCancelled {
			lastDone = i
		}
	}
	return lastDone
}

// scrollSubtaskToolDetail 滚动子任务工具详情窗口。
// 返回是否真正消费了本次滚动：已到边界时返回 false，让调用方透传给
// transcript（滚动链）。
func (t *TUI) scrollSubtaskToolDetail(delta int) bool {
	te := t.selectedSubtaskTool()
	if te == nil {
		t.chat.SubtaskToolDetailScroll = 0
		return false
	}
	deps := t.toolDetailDeps()
	source := toolview.DetailLineSource(te, deps)
	maxOffset := max(0, source.Len()-t.subtaskToolDetailHeight())
	next := clampInt(t.chat.SubtaskToolDetailScroll+delta, 0, maxOffset)
	if next == t.chat.SubtaskToolDetailScroll {
		return false
	}
	t.chat.SubtaskToolDetailScroll = next
	return true
}
