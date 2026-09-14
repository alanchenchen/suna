package chat

import (
	"time"

	"github.com/alanchenchen/suna/internal/tui/components/toolview"
)

func (m *Model) EnsureToolBlock() *toolview.Block {
	if m.CanAppendToCurrentToolBlock() {
		return m.CurrentToolBlock
	}
	block := &toolview.Block{Entries: make(map[string]*toolview.Entry)}
	m.CurrentToolBlock = block
	m.Messages = append(m.Messages, Msg{Role: "tool", Content: block})
	return block
}

func (m *Model) CanAppendToCurrentToolBlock() bool {
	if m.CurrentToolBlock == nil || len(m.Messages) == 0 {
		return false
	}
	last := m.Messages[len(m.Messages)-1]
	if last.Role != "tool" {
		return false
	}
	block, ok := last.Content.(*toolview.Block)
	return ok && block == m.CurrentToolBlock
}

func (m *Model) HasRunningTools() bool {
	for _, te := range m.ActiveTools {
		if te.Status == toolview.StatusRunning || te.Status == toolview.StatusCancelling {
			return true
		}
	}
	return false
}

// MarkActiveToolsCancelling 将仍在执行的 transcript 条目标记为取消中，但保留 active map 等待最终 tool_end 或 run 终态。
func (m *Model) MarkActiveToolsCancelling() {
	for _, te := range m.ActiveTools {
		if te != nil && te.Status == toolview.StatusRunning {
			te.Status = toolview.StatusCancelling
		}
	}
}

// RevertActiveToolsCancelling 在取消请求发送失败时恢复本地展示，允许用户再次取消。
func (m *Model) RevertActiveToolsCancelling() {
	for _, te := range m.ActiveTools {
		if te != nil && te.Status == toolview.StatusCancelling {
			te.Status = toolview.StatusRunning
		}
	}
}

// FinishCancellingTools 在 run 取消终态收尾仍未收到 tool_end 的条目，停止其 spinner 并固定耗时。
func (m *Model) FinishCancellingTools(now time.Time) {
	for id, te := range m.ActiveTools {
		if te == nil || (te.Status != toolview.StatusCancelling && te.Status != toolview.StatusRunning) {
			continue
		}
		te.Status = toolview.StatusCancelled
		te.EndedAt = now
		if start, ok := m.ToolStartTimes[id]; ok {
			te.Duration = now.Sub(start)
		} else if !te.StartedAt.IsZero() {
			te.Duration = now.Sub(te.StartedAt)
		}
		if te.RawName == "skill_load" {
			if view := m.findSkillLoad(id); view != nil {
				view.Status = "cancelled"
				view.EndedAt = now
				view.Duration = te.Duration
			}
		}
		delete(m.ToolStartTimes, id)
		delete(m.ActiveTools, id)
	}
}

// MoveExpandedBlockCursor 在展开的工具块内移动条目光标，并重置详情滚动位置。
// 只有展开态有意义；未展开或块内没有可展开条目时不做任何事。
func (m *Model) MoveExpandedBlockCursor(delta int) {
	if m == nil || m.ExpandedBlock == nil {
		return
	}
	entries := m.ExpandedBlockEntries()
	if len(entries) == 0 {
		m.ExpandedBlockCursor = 0
		return
	}
	idx := clampInt(m.ExpandedBlockCursor+delta, 0, len(entries)-1)
	if idx != m.ExpandedBlockCursor {
		m.ExpandedBlockCursor = idx
		m.ExpandedBlockDetailScroll = 0
	}
}

// ExpandedBlockEntries 返回展开块内可展开的条目：优先主条目（普通工具），
// 没有主条目时退回子任务条目（subtask 面板自身负责内部工具导航）。
func (m *Model) ExpandedBlockEntries() []*toolview.Entry {
	if m == nil || m.ExpandedBlock == nil {
		return nil
	}
	if entries := toolview.VisibleMainEntries(m.ExpandedBlock); len(entries) > 0 {
		return entries
	}
	entries := make([]*toolview.Entry, 0, len(m.ExpandedBlock.Order))
	for _, id := range m.ExpandedBlock.Order {
		if te := m.ExpandedBlock.Entries[id]; toolview.IsSubtask(te) {
			entries = append(entries, te)
		}
	}
	return entries
}

// SelectedExpandedEntry 返回展开块当前光标选中的条目。
func (m *Model) SelectedExpandedEntry() *toolview.Entry {
	entries := m.ExpandedBlockEntries()
	if len(entries) == 0 {
		return nil
	}
	idx := clampInt(m.ExpandedBlockCursor, 0, len(entries)-1)
	return entries[idx]
}

func (m *Model) FindTool(id string) *toolview.Entry {
	if id == "" {
		return nil
	}
	if m.CurrentToolBlock != nil {
		if te := m.CurrentToolBlock.Entries[id]; te != nil {
			return te
		}
	}
	for _, msg := range m.Messages {
		if block, ok := msg.Content.(*toolview.Block); ok && block != nil {
			if te := block.Entries[id]; te != nil {
				return te
			}
		}
	}
	return nil
}

// VisibleSubtaskIDs 返回 subtask 面板要展示的子任务条目。
// 数据来源与渲染判定共用 ActiveSubtaskBlock，避免出现"键位被接管但面板未渲染"的隐形操作。
func (m *Model) VisibleSubtaskIDs() []string {
	block := m.ActiveSubtaskBlock()
	if block == nil {
		return nil
	}
	ids := make([]string, 0, len(block.Order))
	for _, id := range block.Order {
		te := block.Entries[id]
		if toolview.IsSubtask(te) {
			ids = append(ids, id)
		}
	}
	return ids
}

// ActiveSubtaskBlock 返回当前应激活 subtask 面板的块：
// 正在执行的块优先，其次是用户用 Ctrl+T 展开 subtask 盒子的块。
// 展开 tool 盒子不激活面板（两个盒子独立展开）。
// 不做历史回退——run 结束后结果通过 Ctrl+T 展开查看，而不是让历史块自动激活面板，
// 否则 ↑↓/Tab/Enter 会被接管却没有任何可见反馈。
func (m *Model) ActiveSubtaskBlock() *toolview.Block {
	if m == nil {
		return nil
	}
	if m.CurrentToolBlock != nil && hasSubtaskEntries(m.CurrentToolBlock) {
		return m.CurrentToolBlock
	}
	if m.ExpandedBlock != nil && m.ExpandedBoxKind == boxKindSubtask && hasSubtaskEntries(m.ExpandedBlock) {
		return m.ExpandedBlock
	}
	return nil
}

func hasSubtaskEntries(block *toolview.Block) bool {
	if block == nil {
		return false
	}
	for _, id := range block.Order {
		if toolview.IsSubtask(block.Entries[id]) {
			return true
		}
	}
	return false
}

func (m *Model) RunningToolCount() int {
	count := 0
	for _, te := range m.ActiveTools {
		if te.Status == toolview.StatusRunning || te.Status == toolview.StatusCancelling {
			count++
		}
	}
	return count
}

func (m *Model) MarkToolRejected(id, rejectedText string, now time.Time) bool {
	if id == "" {
		return false
	}
	te := m.FindTool(id)
	if te == nil {
		return false
	}
	te.Status = toolview.StatusError
	te.Result = rejectedText
	te.EndedAt = now
	if start, ok := m.ToolStartTimes[id]; ok {
		te.Duration = time.Since(start)
		delete(m.ToolStartTimes, id)
	}
	delete(m.ActiveTools, id)
	return true
}
