package chat

import "github.com/alanchenchen/suna/internal/protocol"

type MemoryConfirmMode int

const (
	MemoryConfirmNone MemoryConfirmMode = iota
	MemoryConfirmDelete
	MemoryConfirmClear
)

type MemoryAction struct {
	ID string
}

func (m *Model) OpenMemoryOverlay() {
	m.MemoryOverlayOpen = true
	m.MemoryLoading = true
	m.MemoryError = ""
	m.MemoryConfirm = MemoryConfirmNone
	m.MemoryConfirmText = ""
	m.MemoryCursor = ClampMemoryCursor(m.MemoryCursor, len(m.Memories))
	m.SkillsOverlayOpen = false
	m.MCPOverlayOpen = false
}

func (m *Model) CloseMemoryOverlay() {
	m.MemoryOverlayOpen = false
	m.MemoryError = ""
	m.MemoryConfirm = MemoryConfirmNone
	m.MemoryConfirmText = ""
}

func (m *Model) SetMemories(memories []protocol.MemoryItem) {
	m.Memories = memories
	m.MemoryLoading = false
	m.MemoryError = ""
	m.MemoryCursor = ClampMemoryCursor(m.MemoryCursor, len(m.Memories))
	if m.MemoryCursor < m.MemoryScroll {
		m.MemoryScroll = m.MemoryCursor
	}
}

func (m *Model) MoveMemoryCursor(delta int) {
	m.MemoryCursor = ClampMemoryCursor(m.MemoryCursor+delta, len(m.Memories))
}

func (m *Model) MemoryClearIndex() int { return len(m.Memories) }

func (m *Model) MemorySelectionIsClear() bool {
	return m.MemoryCursor == m.MemoryClearIndex()
}

func (m *Model) BeginMemoryDelete() bool {
	if len(m.Memories) == 0 || m.MemoryCursor < 0 || m.MemoryCursor >= len(m.Memories) {
		return false
	}
	m.MemoryConfirm = MemoryConfirmDelete
	m.MemoryConfirmText = ""
	m.MemoryError = ""
	return true
}

func (m *Model) BeginMemoryClear() bool {
	m.MemoryConfirm = MemoryConfirmClear
	m.MemoryConfirmText = ""
	m.MemoryError = ""
	return true
}

func (m *Model) ConfirmMemoryDelete() (MemoryAction, bool) {
	if m.MemoryConfirm != MemoryConfirmDelete || m.MemoryCursor < 0 || m.MemoryCursor >= len(m.Memories) {
		return MemoryAction{}, false
	}
	id := m.Memories[m.MemoryCursor].ID
	m.MemoryConfirm = MemoryConfirmNone
	m.MemoryConfirmText = ""
	return MemoryAction{ID: id}, id != ""
}

func (m *Model) ConfirmMemoryClear() bool {
	if m.MemoryConfirm != MemoryConfirmClear || m.MemoryConfirmText != "clear" {
		return false
	}
	m.MemoryConfirm = MemoryConfirmNone
	m.MemoryConfirmText = ""
	return true
}

func (m *Model) CancelMemoryConfirm() {
	m.MemoryConfirm = MemoryConfirmNone
	m.MemoryConfirmText = ""
	m.MemoryError = ""
}

func (m *Model) UpdateMemoryConfirmText(key string) {
	if m.MemoryConfirm != MemoryConfirmClear {
		return
	}
	switch key {
	case "backspace", "ctrl+h":
		if len(m.MemoryConfirmText) > 0 {
			m.MemoryConfirmText = m.MemoryConfirmText[:len(m.MemoryConfirmText)-1]
		}
	case "ctrl+u":
		m.MemoryConfirmText = ""
	default:
		if len([]rune(key)) == 1 && len(m.MemoryConfirmText) < len("clear") {
			m.MemoryConfirmText += key
		}
	}
}

func ClampMemoryCursor(cursor, n int) int {
	max := n // 额外的 clear 项位于所有记忆之后。
	if cursor < 0 {
		return 0
	}
	if cursor > max {
		return max
	}
	return cursor
}
