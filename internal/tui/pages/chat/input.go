package chat

func (m *Model) SyncInputFocus(inputLocked bool) bool {
	if m.Textarea.Placeholder == "" {
		return false
	}
	if inputLocked {
		m.Textarea.Blur()
		return false
	}
	return true
}

func (m *Model) SetInputValue(input string, chatActive bool) bool {
	m.ExitInputHistory()
	if chatActive && m.Textarea.Placeholder != "" {
		m.Textarea.SetValue(input)
		m.Textarea.CursorEnd()
		return true
	}
	m.PendingInput = input
	return false
}

func (m *Model) HasDraft() bool {
	return trimSpace(m.Textarea.Value()) != "" || len(m.Attachments) > 0
}

func (m *Model) AcceptCommandSuggestion() (CommandSpec, bool) {
	suggestion, ok := m.SelectedCommandSuggestion()
	if !ok {
		return CommandSpec{}, false
	}
	m.ExitInputHistory()
	m.Textarea.Reset()
	m.ClearCommandSuggestions()
	return suggestion, true
}

func (m *Model) BrowseInputHistory(delta int) bool {
	if delta == 0 {
		return false
	}
	items := m.inputHistoryItems()
	if len(items) == 0 {
		return false
	}
	if !m.InputHistoryActive {
		if delta > 0 || trimSpace(m.Textarea.Value()) != "" {
			return false
		}
		m.InputHistoryDraft = m.Textarea.Value()
		m.InputHistoryIndex = len(items) - 1
		m.InputHistoryActive = true
	} else {
		m.InputHistoryIndex += delta
		if m.InputHistoryIndex >= len(items) {
			m.Textarea.SetValue(m.InputHistoryDraft)
			m.Textarea.CursorEnd()
			m.ExitInputHistory()
			return true
		}
		if m.InputHistoryIndex < 0 {
			m.InputHistoryIndex = 0
		}
	}
	m.Textarea.SetValue(items[m.InputHistoryIndex])
	m.Textarea.CursorEnd()
	return true
}

func (m *Model) ExitInputHistory() {
	m.InputHistoryActive = false
	m.InputHistoryIndex = -1
	m.InputHistoryDraft = ""
}

func (m *Model) inputHistoryItems() []string {
	items := make([]string, 0)
	last := ""
	for _, msg := range m.Messages {
		if msg.Role != "user" {
			continue
		}
		text := userMessageText(msg.Content)
		if trimSpace(text) == "" || text == last {
			continue
		}
		items = append(items, text)
		last = text
	}
	return items
}

func userMessageText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case UserMessageContent:
		return v.Text
	case *UserMessageContent:
		if v != nil {
			return v.Text
		}
	}
	return ""
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
