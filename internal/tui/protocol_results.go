package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/alanchenchen/suna/internal/protocol"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
)

func (t *TUI) handleProtocolResultMsg(msg tea.Msg) tea.Cmd {
	// method response 在这里转成 TUI 本地状态更新；daemon notification 仍走 notification pump，保持协议语义分层。
	schedule := false
	switch m := msg.(type) {
	case steerResultMsg:
		clientMsgID := m.Message.ClientMsgID
		if clientMsgID == "" {
			clientMsgID = m.ClientMsgID
		}
		resolved, restore := t.resolveSteeringSubmission(clientMsgID, m.Err != nil)
		if m.Err != nil {
			if resolved {
				t.restoreSteeringDrafts(restore)
				t.appendNonToolMessage(chatMsg{Role: "error", Content: m.Err.Error()})
				schedule = true
			}
		} else {
			t.handleSteeringNotification(m.Message)
		}
	case steerRemoveResultMsg:
		if m.Err != nil {
			t.appendNonToolMessage(chatMsg{Role: "error", Content: m.Err.Error()})
			schedule = true
		} else {
			t.handleSteeringNotification(m.Message)
		}
	case cancelResultMsg:
		if m.Err != nil && m.Rejected {
			// daemon 明确拒绝取消时恢复运行展示，允许用户重试；真实 run 不在 TUI 中提前终止。
			t.cancelling = false
			t.currentRunCanControl = true
			t.chat.RevertActiveToolsCancelling()
			t.chat.ClearStatusLabel()
			t.appendNonToolMessage(chatMsg{Role: "error", Content: m.Err.Error()})
			schedule = true
		} else if m.Err != nil {
			// 取消请求写出后的传输错误属于结果不确定，继续等待 daemon lifecycle 收敛。
			t.appendNonToolMessage(chatMsg{Role: "error", Content: m.Err.Error()})
			schedule = true
		}
	case daemonStatusResultMsg:
		t.handleDaemonFullStatusNotification(m.Params)
	case configResultMsg:
		t.handleConfigStateNotification(m.Params)
	case attachmentStatusResultMsg:
		t.handleAttachmentStatusNotification(m.Params)
	case sessionListResultMsg:
		t.sessions = m.Params.Sessions
		if t.mode == uipage.Chat {
			if t.chat.SessionsOverlayOpen {
				t.setSessionOverlaySessions()
			} else {
				t.chat.SetSessions(m.Params.Sessions)
			}
		}
		t.pickWelcomeSessions()
		if t.mode == uipage.Welcome && t.welcomeIdlePicker && len(t.idleWelcomeSessions()) == 0 {
			// 删除最后一个可见空闲会话后直接回到 Welcome，避免只剩无意义的返回项。
			t.welcomeIdlePicker = false
			t.welcomeDeleteConfirm = false
			t.welcomeDeleteID = ""
		}
	case sessionErrorMsg:
		t.chat.SessionsLoading = false
		t.chat.SessionsError = m.Message
	case newSessionResultMsg:
		switched := t.applySessionSnapshot(m.Params)
		t.mode = uipage.Chat
		schedule = true
		if m.DeleteErr != nil {
			t.appendNonToolMessage(chatMsg{Role: "error", Content: t.i18n.Tf("tui.command.new.delete_failed", m.DeleteErr.Error())})
		}
		cmds := []tea.Cmd{t.attachmentStatusCmd(), t.scheduleTranscriptSync(), t.startChatSpinner()}
		if switched {
			// 会话切换清空了 MCP 列表数据，主动拉取一次恢复徽标/overlay 状态；
			// daemon 只在服务器状态变化时推送，不会主动发当前快照。
			cmds = append(cmds, t.listMCPCmd())
		}
		return tea.Batch(cmds...)
	case sessionSnapshotResultMsg:
		switched := t.applySessionSnapshot(m.Params)
		t.mode = uipage.Chat
		schedule = true
		cmds := []tea.Cmd{t.attachmentStatusCmd(), t.scheduleTranscriptSync(), t.startChatSpinner()}
		if switched {
			// 同上：会话切换后重新拉取 MCP 列表，恢复右上角徽章显示。
			cmds = append(cmds, t.listMCPCmd())
		}
		return tea.Batch(cmds...)
	case sessionMetadataResultMsg:
		t.handleSessionStateNotification(protocol.SessionStateParams{Session: m.Session})
	case sessionTitleUpdateResultMsg:
		if m.Err != nil {
			t.restoreOptimisticSessionTitle(m.SessionID, m.OptimisticTitle, m.OldTitle)
			break
		}
		t.handleSessionStateNotification(protocol.SessionStateParams{Session: m.Session})
	case memoryListResultMsg:
		t.handleMemoryListNotification(m.Params)
		schedule = true
	case skillListResultMsg:
		t.handleSkillListNotification(m.Params)
	case mcpListResultMsg:
		t.handleMCPListNotification(m.Params)
	default:
		return nil
	}
	if t.ready {
		if t.mode == uipage.Welcome {
			t.initWelcomeList()
		}
		if schedule || t.mode == uipage.Chat {
			return t.scheduleTranscriptSync()
		}
	}
	return func() tea.Msg { return nil }
}
