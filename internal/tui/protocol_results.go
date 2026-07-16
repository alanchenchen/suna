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
	case daemonStatusResultMsg:
		t.handleDaemonFullStatusNotification(m.Params)
	case configResultMsg:
		t.handleConfigStateNotification(m.Params)
	case attachmentStatusResultMsg:
		t.handleAttachmentStatusNotification(m.Params)
	case sessionListResultMsg:
		t.sessions = m.Params.Sessions
		t.chat.SetSessions(m.Params.Sessions)
		t.pickWelcomeSessions()
	case sessionErrorMsg:
		t.chat.SessionsLoading = false
		t.chat.SessionsError = m.Message
	case sessionSnapshotResultMsg:
		t.applySessionSnapshot(m.Params)
		t.mode = uipage.Chat
		schedule = true
		return tea.Batch(t.attachmentStatusCmd(), t.scheduleTranscriptSync(), t.startChatSpinner())
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
