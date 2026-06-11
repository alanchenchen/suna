package tui

import tea "charm.land/bubbletea/v2"

func (t *TUI) updateAskUser(ks string, msg tea.Msg) (tea.Model, tea.Cmd) {
	ask := t.chat.ActiveAsk()
	if ask == nil {
		return t, nil
	}
	switch ks {
	case "ctrl+c":
		t.doQuit()
		return t, tea.Quit
	case "up":
		t.moveChatCursor(-1)
		return t, nil
	case "down":
		t.moveChatCursor(1)
		return t, nil
	case "enter":
		return t.updateChatEnter()
	case "shift+enter", "ctrl+j":
		if ask.AllowCustom {
			t.chat.InsertNewline()
			t.layoutChat()
		}
		return t, nil
	case "esc":
		return t, nil
	}
	if !ask.AllowCustom {
		return t, nil
	}
	var cmd tea.Cmd
	t.chat.Textarea, cmd = t.chat.Textarea.Update(msg)
	t.updateCmdSuggestionState()
	t.layoutChat()
	return t, cmd
}
