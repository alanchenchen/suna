package chat

import "github.com/alanchenchen/suna/internal/protocol"

type SessionConfirmMode int

const (
	SessionConfirmNone SessionConfirmMode = iota
	SessionConfirmDelete
)

type SessionRowKind int

const (
	SessionRowCurrentWorkspace SessionRowKind = iota
	SessionRowActiveElsewhere
	SessionRowIdleElsewhere
)

type SessionAction struct {
	ID string
}

func (m *Model) OpenSessionsOverlay() {
	m.SessionsOverlayOpen = true
	m.SessionsLoading = true
	m.SessionsError = ""
	m.SessionConfirm = SessionConfirmNone
	m.SessionConfirmID = ""
	m.SessionCursor = ClampSessionCursor(m.SessionCursor, len(m.Sessions))
	m.MemoryOverlayOpen = false
	m.SkillsOverlayOpen = false
	m.MCPOverlayOpen = false
}

func (m *Model) CloseSessionsOverlay() {
	m.SessionsOverlayOpen = false
	m.SessionsError = ""
	m.SessionConfirm = SessionConfirmNone
	m.SessionConfirmID = ""
}

func (m *Model) SetSessions(sessions []protocol.SessionInfo) {
	m.Sessions = sessions
	m.SessionRowKinds = nil
	m.SessionsLoading = false
	m.SessionsError = ""
	m.SessionCursor = ClampSessionCursor(m.SessionCursor, len(m.Sessions))
}

// SetSessionOverlay 写入已经按 Current/Active/Idle 分组的行及其交互语义。
func (m *Model) SetSessionOverlay(sessions []protocol.SessionInfo, kinds []SessionRowKind) {
	m.Sessions = sessions
	m.SessionRowKinds = kinds
	m.SessionsLoading = false
	m.SessionsError = ""
	m.SessionCursor = m.firstSelectableSession()
}

func (m *Model) firstSelectableSession() int {
	for i := range m.Sessions {
		if m.SessionRowKindAt(i) != SessionRowCurrentWorkspace {
			return i
		}
	}
	return -1
}

func (m *Model) SessionRowKindAt(index int) SessionRowKind {
	if index < 0 || index >= len(m.SessionRowKinds) {
		return SessionRowIdleElsewhere
	}
	return m.SessionRowKinds[index]
}

func (m *Model) SelectedActiveSession() (string, bool) {
	if m.SessionCursor < 0 || m.SessionCursor >= len(m.Sessions) || m.SessionRowKindAt(m.SessionCursor) != SessionRowActiveElsewhere {
		return "", false
	}
	id := m.Sessions[m.SessionCursor].ID
	return id, id != ""
}

func (m *Model) MoveSessionCursor(delta int) {
	if len(m.SessionRowKinds) == 0 {
		m.SessionCursor = ClampSessionCursor(m.SessionCursor+delta, len(m.Sessions))
		return
	}
	if len(m.Sessions) == 0 || delta == 0 {
		return
	}
	for i := 0; i < len(m.Sessions); i++ {
		m.SessionCursor = (m.SessionCursor + delta + len(m.Sessions)) % len(m.Sessions)
		if m.SessionRowKindAt(m.SessionCursor) != SessionRowCurrentWorkspace {
			return
		}
	}
	m.SessionCursor = -1
}

func (m *Model) BeginSessionDelete(currentSessionID, currentMessage, activeMessage string) bool {
	if len(m.Sessions) == 0 || m.SessionCursor < 0 || m.SessionCursor >= len(m.Sessions) {
		return false
	}
	s := m.Sessions[m.SessionCursor]
	if m.SessionRowKindAt(m.SessionCursor) != SessionRowIdleElsewhere {
		m.SessionsError = activeMessage
		return false
	}
	if s.ID == currentSessionID {
		m.SessionsError = currentMessage
		return false
	}
	if s.ClientCount > 0 || s.Status != protocol.SessionStatusIdle {
		m.SessionsError = activeMessage
		return false
	}
	m.SessionConfirm = SessionConfirmDelete
	m.SessionConfirmID = s.ID
	m.SessionsError = ""
	return true
}

func (m *Model) ConfirmSessionDelete() (SessionAction, bool) {
	if m.SessionConfirm != SessionConfirmDelete || m.SessionConfirmID == "" {
		return SessionAction{}, false
	}
	id := m.SessionConfirmID
	m.SessionConfirm = SessionConfirmNone
	m.SessionConfirmID = ""
	return SessionAction{ID: id}, true
}

func (m *Model) CancelSessionConfirm() {
	m.SessionConfirm = SessionConfirmNone
	m.SessionConfirmID = ""
	m.SessionsError = ""
}

func ClampSessionCursor(cursor, n int) int {
	if n <= 0 {
		return 0
	}
	if cursor < 0 {
		return 0
	}
	if cursor >= n {
		return n - 1
	}
	return cursor
}
