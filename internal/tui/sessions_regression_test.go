package tui

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/alanchenchen/suna/internal/protocol"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
	welcomepage "github.com/alanchenchen/suna/internal/tui/pages/welcome"
)

func TestWelcomeUsesActiveSessionPicker(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24, ready: true, providerName: "test", modelName: "model"}
	tui.sessions = []protocol.SessionInfo{
		{ID: "active-1", Title: "Fix welcome picker", CWD: "/tmp/a", Status: protocol.SessionStatusRunning},
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
	for _, item := range items {
		if item.SessionID == "active-1" && (item.Key != "Fix welcome picker" || item.CWD != "/tmp/a") {
			t.Fatalf("active picker item = %+v, want friendly title and cwd", item)
		}
	}
}

func TestWelcomeInfoUsesConfiguredDefaultNotCurrentSession(t *testing.T) {
	tui := &TUI{
		i18n:           newTranslator(LocaleEN),
		currentSession: protocol.SessionInfo{ID: "session-1", ModelRef: "session/session-model"},
		configState: protocol.ConfigParams{
			ActiveModel: "default/default-model",
			Models: []protocol.ConfigModel{
				{Provider: "default", Model: "default-model"},
				{Provider: "session", Model: "session-model"},
			},
		},
	}

	info := stripANSIForTest(tui.renderWelcomeInfo())
	if !strings.Contains(info, "default/default-model") {
		t.Fatalf("welcome info = %q, want configured default model", info)
	}
	if strings.Contains(info, "session/session-model") {
		t.Fatalf("welcome info = %q, must not show current session model", info)
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

func TestDeriveSessionTitleCleansCommonPrefix(t *testing.T) {
	got := deriveSessionTitle("请帮我修复 TUI 复制模式的问题\n第二行")
	want := "修复 TUI 复制模式的问题"
	if got != want {
		t.Fatalf("deriveSessionTitle = %q, want %q", got, want)
	}
}

func TestWelcomeNewRequiresConfirmWhenIdleCWDSessionsExist(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24, ready: true, providerName: "test", modelName: "model"}
	tui.sessions = []protocol.SessionInfo{{ID: "old", CWD: cwd, Status: protocol.SessionStatusIdle, MessageCount: 1}}
	tui.pickWelcomeSessions()
	cmd := tui.handleWelcomeAction(welcomepage.ActionNew)
	if cmd != nil {
		t.Fatal("ActionNew returned cmd, want confirmation")
	}
	if !tui.welcomeNewConfirm {
		t.Fatal("welcomeNewConfirm = false, want true")
	}
}
