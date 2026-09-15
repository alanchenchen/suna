package tui

import (
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
)

func notificationUpdatesChatContent(msg notificationMsg) bool {
	switch msg.(type) {
	case agentDeltaMsg, agentRunMsg, steeringMsg, userMessageMsg, toolStartMsg, toolGuardMsg, toolEndMsg, askUserMsg, guardConfirmMsg, interactionResolvedMsg, compactResultMsg, skillLoadMsg, skillReviewMsg:
		return true
	default:
		return false
	}
}

func (t *TUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := t.update(msg)
	// 主题切换等路径只登记待办，命令必须在这里返回给事件循环执行。
	// 在 update 内部直接 program.Send 会死锁（消息 channel 无缓冲且无人接收）。
	if pending := t.takeTranscriptSyncCmd(); pending != nil {
		cmd = tea.Batch(cmd, pending)
	}
	return model, cmd
}

func (t *TUI) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if background, ok := msg.(tea.BackgroundColorMsg); ok {
		t.applyDetectedBackground(background)
		return t, nil
	}
	if _, ok := msg.(petTickMsg); ok {
		return t, t.updatePetTick()
	}
	if notif, ok := msg.(localNotification); ok {
		msg = decodeLocalNotification(notif)
	}
	if notif, ok := msg.(notificationMsg); ok {
		t.handleNotificationMsg(notif)
		if t.mode == uipage.Chat && t.chat.ManualScrollPaused && notificationUpdatesChatContent(notif) {
			t.chat.NewContentWhilePaused = true
		}
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
