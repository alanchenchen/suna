package tui

import (
	"encoding/json"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
)

type ipcNotification struct {
	method string
	params json.RawMessage
}

func (t *TUI) Connect(client *ipcClient) {
	t.ipcCli = client
	t.startNotificationPump()
	t.mode = "welcome"
	t.contextWindow = 128000
	t.toolStartTimes = make(map[string]time.Time)
	t.activeTools = make(map[string]*toolEntry)
	t.phase = phaseIdle

	client.OnNotify(func(method string, params json.RawMessage) {
		// IPC read loop 不能直接阻塞在 Bubble Tea 的 program.Send 上，否则流式输出背压时会反向卡住 daemon 写入。
		t.enqueueNotification(ipcNotification{method: method, params: params})
	})
}

func (t *TUI) startNotificationPump() {
	if t.notifyCh != nil {
		return
	}
	t.notifyCh = make(chan ipcNotification, 4096)
	// 单独 goroutine 串行转发通知，保证 UI 状态只在 Bubble Tea 事件循环里更新。
	go func() {
		for notif := range t.notifyCh {
			if t.program != nil {
				t.program.Send(notif)
			}
		}
	}()
}

func (t *TUI) enqueueNotification(notif ipcNotification) {
	if t.notifyCh == nil {
		return
	}
	select {
	case t.notifyCh <- notif:
	default:
		// 队列满时也不能阻塞 IPC receiveLoop；让后台 goroutine 等待入队。
		go func() { t.notifyCh <- notif }()
	}
}

func (t *TUI) daemonStatusCmd() tea.Cmd {
	return func() tea.Msg {
		if t.ipcCli == nil {
			return nil
		}
		if err := t.ipcCli.DaemonStatus(); err != nil {
			return ipcErrorNotification("config.error", err)
		}
		return nil
	}
}

func (t *TUI) configGetCmd() tea.Cmd {
	return func() tea.Msg {
		if t.ipcCli == nil {
			return nil
		}
		if err := t.ipcCli.ConfigGet(); err != nil {
			return ipcErrorNotification("config.error", err)
		}
		return nil
	}
}

func (t *TUI) sendMessageCmd(input string) tea.Cmd {
	return func() tea.Msg {
		// 所有 IPC 写请求都必须放在 tea.Cmd 中执行，避免快捷键处理阻塞 Update 主循环。
		if t.ipcCli == nil {
			return ipcErrorNotification("config.error", fmt.Errorf("%s", t.tr("error.not_connected")))
		}
		if err := t.ipcCli.SendMessage(input); err != nil {
			return ipcErrorNotification("config.error", err)
		}
		return nil
	}
}

func (t *TUI) cancelCmd() tea.Cmd {
	return func() tea.Msg {
		if t.ipcCli == nil {
			return nil
		}
		if err := t.ipcCli.Cancel(); err != nil {
			return ipcErrorNotification("config.error", err)
		}
		return nil
	}
}

func (t *TUI) newSessionCmd() tea.Cmd {
	return func() tea.Msg {
		if t.ipcCli == nil {
			return ipcErrorNotification("config.error", fmt.Errorf("%s", t.tr("error.not_connected")))
		}
		if err := t.ipcCli.NewSession(); err != nil {
			return ipcErrorNotification("config.error", err)
		}
		return nil
	}
}

func (t *TUI) askReplyCmd(askID, answer string) tea.Cmd {
	return func() tea.Msg {
		if t.ipcCli == nil {
			return ipcErrorNotification("config.error", fmt.Errorf("%s", t.tr("error.not_connected")))
		}
		if err := t.ipcCli.AskReply(askID, answer); err != nil {
			return ipcErrorNotification("config.error", err)
		}
		return nil
	}
}

func (t *TUI) guardReplyCmd(guardID, decision string) tea.Cmd {
	return func() tea.Msg {
		if t.ipcCli == nil {
			return ipcErrorNotification("config.error", fmt.Errorf("%s", t.tr("error.not_connected")))
		}
		if err := t.ipcCli.GuardReply(guardID, decision); err != nil {
			return ipcErrorNotification("config.error", err)
		}
		return nil
	}
}

func (t *TUI) compactCmd() tea.Cmd {
	return func() tea.Msg {
		if t.ipcCli == nil {
			return ipcErrorNotification("compact.error", fmt.Errorf("%s", t.tr("error.not_connected")))
		}
		if err := t.ipcCli.Compact(); err != nil {
			return ipcErrorNotification("compact.error", err)
		}
		return nil
	}
}

func (t *TUI) searchMemoryCmd(query string, topK int) tea.Cmd {
	return func() tea.Msg {
		if t.ipcCli == nil {
			return ipcErrorNotification("config.error", fmt.Errorf("%s", t.tr("error.not_connected")))
		}
		if err := t.ipcCli.SearchMemory(query, topK); err != nil {
			return ipcErrorNotification("config.error", err)
		}
		return nil
	}
}

func ipcErrorNotification(method string, err error) tea.Msg {
	return ipcNotification{method: method, params: []byte(fmt.Sprintf(`{"message":%q}`, err.Error()))}
}
