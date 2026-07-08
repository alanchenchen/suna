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
	if km, ok := msg.(tea.KeyMsg); ok {
		if t.selectionMode {
			// 选择模式会关闭鼠标捕获，把拖拽交还给终端原生选择。
			// 某些终端在该模式下会把滚轮转成 up/down 键序列；这里必须吞掉
			// 除退出键外的所有按键，避免误触发输入历史、滚动或其他 Chat 行为。
			switch km.String() {
			case "esc", "ctrl+s":
				t.selectionMode = false
			}
			return t, nil
		}
		switch km.String() {
		case "ctrl+s":
			if t.mode == uipage.Chat {
				t.selectionMode = true
				return t, nil
			}
		}
	}
	if t.selectionMode {
		if _, ok := msg.(tea.MouseMsg); ok {
			return t, nil
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
