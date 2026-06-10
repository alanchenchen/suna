package tui

import (
	"encoding/json"
	"fmt"
	tuievents "github.com/alanchenchen/suna/internal/tui/events"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
	tuitransport "github.com/alanchenchen/suna/internal/tui/transport"
	"time"

	tea "charm.land/bubbletea/v2"
)

type localNotification struct {
	method string
	params json.RawMessage
}

func (n localNotification) toEvent() tuievents.Notification {
	return tuievents.Notification{Method: n.method, Params: n.params}
}

func (t *TUI) Connect(client *tuitransport.Client) {
	t.localCli = client
	t.startNotificationPump()
	t.mode = uipage.Welcome
	t.contextWindow = 128000
	t.chat.ToolStartTimes = make(map[string]time.Time)
	t.chat.ActiveTools = make(map[string]*toolEntry)
	t.chat.Phase = phaseIdle

	client.OnNotify(func(method string, params json.RawMessage) {
		// local transport 的 read loop 不能直接阻塞在 Bubble Tea 的 program.Send 上，否则流式输出背压时会反向卡住 daemon 写入。
		t.enqueueNotification(localNotification{method: method, params: params})
	})
}

func (t *TUI) startNotificationPump() {
	if t.notifyCh != nil {
		return
	}
	t.notifyCh = make(chan localNotification, 4096)
	// 单独 goroutine 串行转发通知；文本流在这里按帧合并，UI 状态仍只在 Bubble Tea 事件循环里更新。
	go (&tuievents.Batcher{Send: func(msg tea.Msg) {
		if t.program != nil {
			t.program.Send(msg)
		}
	}}).Run(eventNotificationChan(t.notifyCh))
}

func eventNotificationChan(in <-chan localNotification) <-chan tuievents.Notification {
	out := make(chan tuievents.Notification)
	go func() {
		defer close(out)
		for notif := range in {
			out <- notif.toEvent()
		}
	}()
	return out
}

func (t *TUI) enqueueNotification(notif localNotification) {
	if t.notifyCh == nil {
		return
	}
	select {
	case t.notifyCh <- notif:
	default:
		// 队列满时也不能阻塞 local receiveLoop；让后台 goroutine 等待入队。
		go func() { t.notifyCh <- notif }()
	}
}

func (t *TUI) daemonStatusCmd() tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return nil
		}
		if err := t.localCli.DaemonStatus(); err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		return nil
	}
}

func (t *TUI) configGetCmd() tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return nil
		}
		if err := t.localCli.ConfigGet(); err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		return nil
	}
}

func (t *TUI) attachmentStatusCmd() tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return nil
		}
		if err := t.localCli.AttachmentStatus(); err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		return nil
	}
}

func (t *TUI) attachmentClearCmd() tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification(notifyConfigError, fmt.Errorf("%s", t.tr("error.not_connected")))
		}
		if err := t.localCli.AttachmentClear(); err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		return nil
	}
}

func (t *TUI) sendMessageCmd(input string, attachments []attachmentItem) tea.Cmd {
	return func() tea.Msg {
		// 所有 transport 写请求都必须放在 tea.Cmd 中执行，避免快捷键处理阻塞 Update 主循环。
		if t.localCli == nil {
			return ipcErrorNotification(notifyConfigError, fmt.Errorf("%s", t.tr("error.not_connected")))
		}
		if err := t.localCli.SendMessage(input, attachments); err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		return nil
	}
}

func (t *TUI) resumeRunCmd() tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification(notifyConfigError, fmt.Errorf("%s", t.tr("error.not_connected")))
		}
		if err := t.localCli.ResumeRun(); err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		return nil
	}
}

func (t *TUI) cancelCmd() tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return nil
		}
		if err := t.localCli.Cancel(); err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		return nil
	}
}

func (t *TUI) newSessionCmd() tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification(notifyConfigError, fmt.Errorf("%s", t.tr("error.not_connected")))
		}
		if err := t.localCli.NewSession(); err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		return nil
	}
}

func (t *TUI) restoreSessionCmd() tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification(notifyConfigError, fmt.Errorf("%s", t.tr("error.not_connected")))
		}
		if err := t.localCli.RestoreSession(); err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		return nil
	}
}

func (t *TUI) askReplyCmd(askID, answer string) tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification(notifyConfigError, fmt.Errorf("%s", t.tr("error.not_connected")))
		}
		if err := t.localCli.AskReply(askID, answer); err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		return nil
	}
}

func (t *TUI) guardReplyCmd(guardID, decision string) tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification(notifyConfigError, fmt.Errorf("%s", t.tr("error.not_connected")))
		}
		if err := t.localCli.GuardReply(guardID, decision); err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		return nil
	}
}

func (t *TUI) listSkillsCmd() tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification(notifyConfigError, fmt.Errorf("%s", t.tr("error.not_connected")))
		}
		if err := t.localCli.ListSkills(); err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		return nil
	}
}

func (t *TUI) listMCPCmd() tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification(notifyMCPError, fmt.Errorf("%s", t.tr("error.not_connected")))
		}
		if err := t.localCli.ListMCP(); err != nil {
			return ipcErrorNotification(notifyMCPError, err)
		}
		return nil
	}
}

func (t *TUI) compactCmd() tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification(notifyCompactError, fmt.Errorf("%s", t.tr("error.not_connected")))
		}
		if err := t.localCli.Compact(); err != nil {
			return ipcErrorNotification(notifyCompactError, err)
		}
		return nil
	}
}

func deferManualCompactRequestCmd() tea.Cmd {
	return func() tea.Msg {
		return manualCompactRequestMsg{}
	}
}

func (t *TUI) listMemoryCmd() tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification(notifyConfigError, fmt.Errorf("%s", t.tr("error.not_connected")))
		}
		if err := t.localCli.ListMemory(); err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		return nil
	}
}

func errNotConnected(t *TUI) error {
	return fmt.Errorf("%s", t.tr("error.not_connected"))
}

func ipcErrorNotification(method string, err error) tea.Msg {
	return localNotification{method: method, params: []byte(fmt.Sprintf(`{"message":%q}`, err.Error()))}
}
