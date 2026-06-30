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
		if te.Status == toolview.StatusRunning {
			return true
		}
	}
	return false
}

func (m *Model) MoveSelectedTool(delta int) {
	ids := m.VisibleToolIDs()
	if len(ids) == 0 {
		return
	}
	idx := 0
	for i, id := range ids {
		if id == m.SelectedToolID {
			idx = i
			break
		}
	}
	idx += delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(ids) {
		idx = len(ids) - 1
	}
	if m.SelectedToolID != ids[idx] {
		m.SelectedToolID = ids[idx]
		m.ToolDetailScroll = 0
	}
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

func (m *Model) VisibleToolIDs() []string {
	block := m.CurrentToolBlock
	if block == nil {
		for i := len(m.Messages) - 1; i >= 0; i-- {
			if b, ok := m.Messages[i].Content.(*toolview.Block); ok {
				block = b
				break
			}
		}
	}
	if block == nil {
		return nil
	}
	ids := make([]string, 0, len(block.Order))
	for _, id := range block.Order {
		te := block.Entries[id]
		if te == nil || toolview.IsSubtask(te) || toolview.HasSubtaskParent(block, te.ParentID) {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

func (m *Model) VisibleSubtaskIDs() []string {
	block := m.CurrentToolBlock
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

func (m *Model) SelectedToolPosition() (int, int) {
	ids := m.VisibleToolIDs()
	for i, id := range ids {
		if id == m.SelectedToolID {
			return i, len(ids)
		}
	}
	return 0, len(ids)
}

func (m *Model) RunningToolCount() int {
	count := 0
	for _, te := range m.ActiveTools {
		if te.Status == toolview.StatusRunning {
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
	if m.SelectedToolID == "" {
		m.SelectedToolID = id
	}
	return true
}
