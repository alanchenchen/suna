package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/alanchenchen/suna/internal/protocol"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
	welcomepage "github.com/alanchenchen/suna/internal/tui/pages/welcome"
)

func TestWelcomeUsesActiveSessionPicker(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24, ready: true, providerName: "test", modelName: "model"}
	tui.sessions = []protocol.SessionInfo{
		{ID: "active-1", CWD: "/tmp/a", Status: protocol.SessionStatusRunning},
		{ID: "active-2", CWD: "/tmp/b", ClientCount: 1, Status: protocol.SessionStatusIdle},
	}

	items := tui.welcomeMenuItems()
	var joinItems int
	var picker welcomepage.Item
	for _, item := range items {
		if item.Action == welcomepage.ActionJoin {
			joinItems++
		}
		if item.Action == welcomepage.ActionJoinPicker {
			picker = item
		}
	}
	if joinItems != 0 {
		t.Fatalf("joinItems = %d, want 0 before opening picker", joinItems)
	}
	if picker.Action != welcomepage.ActionJoinPicker || picker.Key != "2" {
		t.Fatalf("picker = %+v, want ActionJoinPicker with count 2", picker)
	}

	tui.welcomeActivePicker = true
	items = tui.welcomeMenuItems()
	joinItems = 0
	seen := map[string]bool{}
	for _, item := range items {
		if item.Action == welcomepage.ActionJoin {
			joinItems++
			seen[item.SessionID] = true
		}
	}
	if joinItems != 2 || !seen["active-1"] || !seen["active-2"] {
		t.Fatalf("picker join items = %d seen=%v, want both active sessions", joinItems, seen)
	}
}

func TestSessionsOverlayCtrlCQuits(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24, ready: true}
	tui.initChatComponents()
	tui.chat.SessionsOverlayOpen = true

	_, cmd := tui.updateChatKey("ctrl+c", tea.KeyPressMsg{})
	if cmd == nil {
		t.Fatal("cmd = nil, want quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("cmd() = %T, want tea.QuitMsg", msg)
	}
}

func TestSessionsOverlayRejectsActiveDeletion(t *testing.T) {
	m := chatpage.Model{Sessions: []protocol.SessionInfo{{ID: "active", Status: protocol.SessionStatusRunning}}}
	if ok := m.BeginSessionDelete("", "current", "active"); ok {
		t.Fatal("BeginSessionDelete active = true, want false")
	}
	if m.SessionsError != "active" {
		t.Fatalf("SessionsError = %q, want active", m.SessionsError)
	}
}
