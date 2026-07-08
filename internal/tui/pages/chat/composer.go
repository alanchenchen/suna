package chat

import "time"

func (m *Model) ResetDraft() {
	m.ExitInputHistory()
	m.Attachments = nil
	m.AttachmentMode = false
	m.AttachmentDelete = false
	m.AttachmentCursor = 0
	m.CmdSuggestions = nil
	m.CmdSuggestionIdx = 0
}

func (m *Model) UpdateCommandSuggestions(input string, max int) {
	m.CmdSuggestions = Suggestions(input, max)
	if m.CmdSuggestionIdx >= len(m.CmdSuggestions) {
		m.CmdSuggestionIdx = 0
	}
}

func (m *Model) ClearCommandSuggestions() {
	m.CmdSuggestions = nil
	m.CmdSuggestionIdx = 0
}

func (m *Model) MoveCommandSuggestion(delta int) {
	if len(m.CmdSuggestions) == 0 {
		return
	}
	m.CmdSuggestionIdx += delta
	if m.CmdSuggestionIdx < 0 {
		m.CmdSuggestionIdx = 0
	}
	if m.CmdSuggestionIdx >= len(m.CmdSuggestions) {
		m.CmdSuggestionIdx = len(m.CmdSuggestions) - 1
	}
}

func (m Model) SelectedCommandSuggestion() (CommandSpec, bool) {
	if len(m.CmdSuggestions) == 0 || m.CmdSuggestionIdx < 0 || m.CmdSuggestionIdx >= len(m.CmdSuggestions) {
		return CommandSpec{}, false
	}
	return m.CmdSuggestions[m.CmdSuggestionIdx], true
}

func (m *Model) SetStatusLabel(label string, now time.Time) {
	if m.Loading && m.StatusLabel == label && !m.PhaseStart.IsZero() {
		return
	}
	m.StatusLabel = label
	m.Loading = true
	m.PhaseStart = now
}

func (m *Model) ClearStatusLabel() {
	m.StatusLabel = ""
}

func (m *Model) ResetPhase() {
	m.Loading = false
	m.Phase = PhaseIdle
	m.PhaseStart = time.Time{}
	m.StatusLabel = ""
	m.LastWaitingTool = ""
	m.ResetToolState()
}
