package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/alanchenchen/suna/internal/protocol"
	tuievents "github.com/alanchenchen/suna/internal/tui/events"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
	tuitransport "github.com/alanchenchen/suna/internal/tui/transport"
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
	if t.notifyQueue != nil {
		return
	}
	// 单独 goroutine 串行转发通知；文本流在这里按帧合并，UI 状态仍只在 Bubble Tea 事件循环里更新。
	batcher := &tuievents.Batcher{Send: func(msg tea.Msg) {
		if t.program != nil {
			t.program.Send(msg)
		}
	}}
	eventCh := make(chan tuievents.Notification, notificationQueueSize)
	go batcher.Run(eventCh)
	t.notifyQueue = newNotificationQueue(func(notif localNotification) {
		eventCh <- notif.toEvent()
	})
}

func (t *TUI) enqueueNotification(notif localNotification) {
	if t.notifyQueue == nil {
		return
	}
	t.notifyQueue.enqueue(notif)
}

func (t *TUI) daemonStatusCmd() tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return nil
		}
		result, err := t.localCli.DaemonStatus()
		if err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		return daemonStatusResultMsg{Params: result}
	}
}

func (t *TUI) configGetCmd() tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return nil
		}
		result, err := t.localCli.ConfigGet()
		if err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		return configResultMsg{Params: result}
	}
}

func (t *TUI) attachmentStatusCmd() tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return nil
		}
		result, err := t.localCli.AttachmentStatus()
		if err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		return attachmentStatusResultMsg{Params: result}
	}
}

func (t *TUI) attachmentClearCmd() tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification(notifyConfigError, fmt.Errorf("%s", t.tr("error.not_connected")))
		}
		result, err := t.localCli.AttachmentClear()
		if err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		return attachmentStatusResultMsg{Params: protocol.AttachmentStatusResult{SessionID: result.SessionID, Root: result.Root, Bytes: result.Bytes, Count: result.Count}}
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

func (t *TUI) sessionListCmd() tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return sessionErrorMsg{Message: t.tr("error.not_connected")}
		}
		result, err := t.localCli.ListSessions(protocol.SessionListParams{})
		if err != nil {
			return sessionErrorMsg{Message: err.Error()}
		}
		return sessionListResultMsg{Params: result}
	}
}

func (t *TUI) newSessionCmd(replaceSessionIDs ...string) tea.Cmd {
	replaceSessionID := ""
	if len(replaceSessionIDs) > 0 {
		replaceSessionID = replaceSessionIDs[0]
	}
	return func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification(notifyConfigError, fmt.Errorf("%s", t.tr("error.not_connected")))
		}
		cwd, _ := os.Getwd()
		created, err := t.localCli.CreateSession(cwd, defaultSessionTitle(cwd))
		if err != nil {
			// 创建失败时旧会话仍保持附着，不能提前清空或删除它。
			return ipcErrorNotification(notifyConfigError, err)
		}
		if replaceSessionID == "" {
			return newSessionResultMsg{Params: created}
		}
		// 创建/附着新会话成功后才删除旧会话；删除失败保留新会话并向用户报告。
		if err := t.localCli.DeleteSession(replaceSessionID); err != nil {
			return newSessionResultMsg{Params: created, DeleteErr: err}
		}
		return newSessionResultMsg{Params: created}
	}
}

func (t *TUI) attachSessionCmd(sessionID string, requireActive bool) tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification(notifyConfigError, fmt.Errorf("%s", t.tr("error.not_connected")))
		}
		result, err := t.localCli.AttachSession(sessionID, requireActive)
		if err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		return sessionSnapshotResultMsg{Params: result}
	}
}

func (t *TUI) detachSessionCmd() tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification(notifyConfigError, fmt.Errorf("%s", t.tr("error.not_connected")))
		}
		if err := t.localCli.DetachSession(); err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		result, err := t.localCli.ListSessions(protocol.SessionListParams{})
		if err != nil {
			return sessionErrorMsg{Message: err.Error()}
		}
		return sessionListResultMsg{Params: result}
	}
}

func (t *TUI) deleteSessionCmd(sessionID string) tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification(notifyConfigError, fmt.Errorf("%s", t.tr("error.not_connected")))
		}
		if err := t.localCli.DeleteSession(sessionID); err != nil {
			return sessionErrorMsg{Message: t.i18n.Tf("tui.sessions.delete_failed", err.Error())}
		}
		result, err := t.localCli.ListSessions(protocol.SessionListParams{})
		if err != nil {
			return sessionErrorMsg{Message: err.Error()}
		}
		return sessionListResultMsg{Params: result}
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
		result, err := t.localCli.ListSkills()
		if err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		return skillListResultMsg{Params: result}
	}
}

func (t *TUI) listMCPCmd() tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification(notifyMCPError, fmt.Errorf("%s", t.tr("error.not_connected")))
		}
		result, err := t.localCli.ListMCP()
		if err != nil {
			return ipcErrorNotification(notifyMCPError, err)
		}
		return mcpListResultMsg{Params: result}
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
		result, err := t.localCli.ListMemory()
		if err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		return memoryListResultMsg{Params: result}
	}
}

func errNotConnected(t *TUI) error {
	return fmt.Errorf("%s", t.tr("error.not_connected"))
}

func ipcErrorNotification(method string, err error) tea.Msg {
	return localNotification{method: method, params: []byte(fmt.Sprintf(`{"message":%q}`, err.Error()))}
}

func (t *TUI) updateSessionTitleCmd(sessionID, title, oldTitle string) tea.Cmd {
	return func() tea.Msg {
		result := sessionTitleUpdateResultMsg{
			SessionID:       sessionID,
			OldTitle:        oldTitle,
			OptimisticTitle: title,
		}
		if t.localCli == nil {
			result.Err = errNotConnected(t)
			return result
		}
		if sessionID == "" || strings.TrimSpace(title) == "" {
			result.Err = fmt.Errorf("invalid session title update")
			return result
		}
		updated, err := t.localCli.UpdateSession(protocol.SessionUpdateParams{SessionID: sessionID, Title: &title})
		if err != nil {
			result.Err = err
			return result
		}
		result.Session = updated.Session
		return result
	}
}
