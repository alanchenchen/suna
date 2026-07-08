package tui

import (
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
)

func (t *TUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if notif, ok := msg.(localNotification); ok {
		msg = decodeLocalNotification(notif)
	}
	if notif, ok := msg.(notificationMsg); ok {
		t.handleNotificationMsg(notif)
		if t.mode == uipage.Welcome && t.ready {
			t.initWelcomeList()
		}
		if t.mode == uipage.Chat {
			return t, tea.Batch(t.scheduleTranscriptSync(), t.startChatSpinner())
		}
		return t, nil
	}
	if cmd := t.handleProtocolResultMsg(msg); cmd != nil {
		return t, cmd
	}
	if _, ok := msg.(inputCursorBlinkMsg); ok {
		// tick 链永不断：不论当前是哪个页面都要继续重排，否则离开 chat 后闪烁链会永久停止。
		if t.inputCursorBlinking {
			return t, t.updateInputCursorBlink()
		}
		return t, nil
	}
	if _, ok := msg.(spinner.TickMsg); ok && t.mode != uipage.Chat {
		// spinner tick 只属于 Chat；离开 Chat 时终止链，避免回到运行会话后误判已有 tick。
		t.chatSpinnerTicking = false
		return t, nil
	}

	if !t.ready {
		if ws, ok := msg.(tea.WindowSizeMsg); ok {
			t.width = ws.Width
			t.height = ws.Height
			t.ready = true
			if t.mode == uipage.Chat {
				return t, t.initChatComponents()
			}
			return t, nil
		}
		return t, nil
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "ctrl+y":
			t.copyMode = !t.copyMode
			return t, nil
		case "esc":
			if t.copyMode {
				t.copyMode = false
				return t, nil
			}
		}
	}

	switch t.mode {
	case uipage.Welcome:
		return t.updateWelcome(msg)
	case uipage.Config:
		return t.updateConfig(msg)
	case uipage.Help:
		return t.updateHelp(msg)
	default:
		return t.updateChat(msg)
	}
}
