package chat

import "github.com/alanchenchen/suna/internal/protocol"

type SessionConfirmMode int

const (
	SessionConfirmNone SessionConfirmMode = iota
	SessionConfirmDelete
)

type SessionAction struct {
	ID string
}

func (m *Model) OpenSessionsOverlay() {
	m.SessionsOverlayOpen = true
	m.SessionsLoading = true
	m.SessionsError = ""
	m.SessionConfirm = SessionConfirmNone
	m.SessionCursor = ClampSessionCursor(m.SessionCursor, len(m.Sessions))
	m.MemoryOverlayOpen = false
	m.SkillsOverlayOpen = false
	m.MCPOverlayOpen = false
}

func (m *Model) CloseSessionsOverlay() {
	m.SessionsOverlayOpen = false
	m.SessionsError = ""
	m.SessionConfirm = SessionConfirmNone
}

func (m *Model) SetSessions(sessions []protocol.SessionInfo) {
	m.Sessions = sessions
	m.SessionsLoading = false
	m.SessionsError = ""
	m.SessionCursor = ClampSessionCursor(m.SessionCursor, len(m.Sessions))
}

func (m *Model) MoveSessionCursor(delta int) {
	m.SessionCursor = ClampSessionCursor(m.SessionCursor+delta, len(m.Sessions))
}

func (m *Model) BeginSessionDelete(currentSessionID, currentMessage, activeMessage string) bool {
	if len(m.Sessions) == 0 || m.SessionCursor < 0 || m.SessionCursor >= len(m.Sessions) {
		return false
	}
	s := m.Sessions[m.SessionCursor]
	if s.ID == currentSessionID {
		m.SessionsError = currentMessage
		return false
	}
	if s.ClientCount > 0 || s.Status != protocol.SessionStatusIdle {
		m.SessionsError = activeMessage
		return false
	}
	m.SessionConfirm = SessionConfirmDelete
	m.SessionsError = ""
	return true
}

func (m *Model) ConfirmSessionDelete() (SessionAction, bool) {
	if m.SessionConfirm != SessionConfirmDelete || m.SessionCursor < 0 || m.SessionCursor >= len(m.Sessions) {
		return SessionAction{}, false
	}
	id := m.Sessions[m.SessionCursor].ID
	m.SessionConfirm = SessionConfirmNone
	return SessionAction{ID: id}, id != ""
}

func (m *Model) CancelSessionConfirm() {
	m.SessionConfirm = SessionConfirmNone
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
