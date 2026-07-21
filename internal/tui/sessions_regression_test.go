package tui

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/alanchenchen/suna/internal/protocol"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
	welcomepage "github.com/alanchenchen/suna/internal/tui/pages/welcome"
)

func TestWelcomeSeparatesOtherCWDSessions(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24, ready: true, providerName: "test", modelName: "model"}
	tui.sessions = []protocol.SessionInfo{
		{ID: "current-active", CWD: cwd, Status: protocol.SessionStatusRunning},
		{ID: "current-idle", CWD: cwd, Status: protocol.SessionStatusIdle, MessageCount: 1},
		{ID: "active-1", Title: "Fix welcome picker", CWD: "/tmp/a", Status: protocol.SessionStatusRunning},
		{ID: "active-2", CWD: "/tmp/b", ClientCount: 1, Status: protocol.SessionStatusIdle},
		{ID: "idle-1", Title: "Archived work", CWD: "/tmp/c", Status: protocol.SessionStatusIdle},
	}
	tui.pickWelcomeSessions()

	items := tui.welcomeMenuItems()
	var newOrResume, activePicker, idlePicker int
	for _, item := range items {
		if item.Action == welcomepage.ActionNew || item.Action == welcomepage.ActionResume {
			newOrResume++
		}
		if item.Action == welcomepage.ActionJoinPicker {
			activePicker++
			if item.Key != "2" {
				t.Fatalf("active picker count = %q, want 2", item.Key)
			}
		}
		if item.Action == welcomepage.ActionIdlePicker {
			idlePicker++
			if item.Key != "1" {
				t.Fatalf("idle picker count = %q, want 1", item.Key)
			}
		}
	}
	if newOrResume != 1 || activePicker != 1 || idlePicker != 1 {
		t.Fatalf("welcome menu counts = new/resume:%d active:%d idle:%d, want 1 each", newOrResume, activePicker, idlePicker)
	}

	tui.welcomeActivePicker = true
	items = tui.welcomeMenuItems()
	seenActive := map[string]bool{}
	for _, item := range items {
		if item.Action == welcomepage.ActionJoin {
			seenActive[item.SessionID] = true
		}
	}
	if len(seenActive) != 2 || !seenActive["active-1"] || !seenActive["active-2"] {
		t.Fatalf("active picker sessions = %v, want only other-cwd active sessions", seenActive)
	}

	tui.welcomeActivePicker = false
	tui.welcomeIdlePicker = true
	items = tui.welcomeMenuItems()
	var idle welcomepage.Item
	for _, item := range items {
		if item.SessionID != "" {
			idle = item
		}
	}
	if idle.SessionID != "idle-1" || !idle.Deletable || idle.Action != welcomepage.ActionNone {
		t.Fatalf("idle picker item = %+v, want delete-only idle session", idle)
	}
}

func TestWelcomeIdleSessionDeleteRequiresConfirmation(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 80, height: 24, ready: true, providerName: "test", modelName: "model"}
	tui.sessions = []protocol.SessionInfo{{ID: "idle", CWD: "/tmp/other", Status: protocol.SessionStatusIdle}}
	tui.welcomeIdlePicker = true
	tui.initWelcomeList()

	items := tui.welcomeMenuItems()
	if action, handled := tui.menu.UpdateKey("down", items); !handled || action != welcomepage.ActionNone {
		t.Fatalf("Down to idle session = (%v, %v), want (%v, true)", action, handled, welcomepage.ActionNone)
	}
	if action, handled := tui.menu.UpdateKey("enter", items); !handled || action != welcomepage.ActionNone {
		t.Fatalf("Enter on idle session = (%v, %v), want (%v, true)", action, handled, welcomepage.ActionNone)
	}
	if action, handled := tui.menu.UpdateKey("delete", items); !handled || action != welcomepage.ActionDelete {
		t.Fatalf("Delete on idle session = (%v, %v), want (%v, true)", action, handled, welcomepage.ActionDelete)
	}
	if cmd := tui.handleWelcomeAction(welcomepage.ActionDelete); cmd != nil {
		t.Fatal("ActionDelete returned command, want confirmation")
	}
	if !tui.welcomeDeleteConfirm || tui.welcomeDeleteID != "idle" {
		t.Fatalf("delete confirmation = (%v, %q), want (true, %q)", tui.welcomeDeleteConfirm, tui.welcomeDeleteID, "idle")
	}
}

func TestWelcomeIdlePickerDoesNotRepeatHelpInBackItem(t *testing.T) {
	tui := &TUI{
		i18n:              newTranslator(LocaleEN),
		width:             80,
		height:            24,
		ready:             true,
		providerName:      "test",
		modelName:         "model",
		welcomeIdlePicker: true,
		sessions: []protocol.SessionInfo{
			{ID: "idle", CWD: "/tmp/other", Status: protocol.SessionStatusIdle},
		},
	}

	tui.initWelcomeList()
	view := stripANSIForTest(tui.viewWelcome())
	if got, want := strings.Count(view, tui.tr("tui.welcome.idle_help")), 1; got != want {
		t.Fatalf("idle help occurrences = %d, want %d: %q", got, want, view)
	}
}

func TestWelcomeReturnsToMenuAfterDeletingLastIdleSession(t *testing.T) {
	tui := &TUI{
		i18n:                 newTranslator(LocaleEN),
		width:                80,
		height:               24,
		ready:                true,
		mode:                 uipage.Welcome,
		providerName:         "test",
		modelName:            "model",
		welcomeIdlePicker:    true,
		welcomeDeleteID:      "idle",
		welcomeDeleteConfirm: true,
		sessions: []protocol.SessionInfo{
			{ID: "idle", CWD: "/tmp/other", Status: protocol.SessionStatusIdle},
		},
	}

	tui.handleProtocolResultMsg(sessionListResultMsg{Params: protocol.SessionListResult{}})

	if tui.welcomeIdlePicker {
		t.Fatal("welcomeIdlePicker = true, want false after deleting last idle session")
	}
	if tui.welcomeDeleteConfirm || tui.welcomeDeleteID != "" {
		t.Fatalf("delete state = (%v, %q), want (false, empty)", tui.welcomeDeleteConfirm, tui.welcomeDeleteID)
	}
	for _, item := range tui.welcomeMenuItems() {
		if item.Action == welcomepage.ActionBack || item.Action == welcomepage.ActionIdlePicker {
			t.Fatalf("welcome item = %+v, should not remain after deleting last idle session", item)
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

func TestSessionsOverlayGroupsAndActions(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 80, height: 24, ready: true, currentSession: protocol.SessionInfo{ID: "current"}}
	tui.sessions = []protocol.SessionInfo{
		{ID: "current", CWD: cwd, Status: protocol.SessionStatusIdle, ClientCount: 1},
		{ID: "workspace-idle", CWD: cwd, Status: protocol.SessionStatusIdle},
		{ID: "active", CWD: "/tmp/active", Status: protocol.SessionStatusRunning},
		{ID: "idle", CWD: "/tmp/idle", Status: protocol.SessionStatusIdle},
	}
	tui.chat.OpenSessionsOverlay()
	tui.setSessionOverlaySessions()

	if got := tui.chat.SessionRowKinds; len(got) != 4 || got[0] != chatpage.SessionRowCurrentWorkspace || got[1] != chatpage.SessionRowCurrentWorkspace || got[2] != chatpage.SessionRowActiveElsewhere || got[3] != chatpage.SessionRowIdleElsewhere {
		t.Fatalf("SessionRowKinds = %v, want Current, Current, Active, Idle", got)
	}
	if tui.chat.SessionCursor != 2 {
		t.Fatalf("SessionCursor = %d, want first selectable active row 2", tui.chat.SessionCursor)
	}
	if id, ok := tui.chat.SelectedActiveSession(); !ok || id != "active" {
		t.Fatalf("SelectedActiveSession = (%q, %v), want (active, true)", id, ok)
	}
	if got := tui.sessionsHelpText(0, 8, 4); !strings.Contains(got, "Enter join") {
		t.Fatalf("active help = %q, want join hint", got)
	}
	tui.chat.MoveSessionCursor(1)
	if id, ok := tui.chat.SelectedActiveSession(); ok || id != "" {
		t.Fatalf("idle SelectedActiveSession = (%q, %v), want empty false", id, ok)
	}
	if got := tui.sessionsHelpText(0, 8, 4); !strings.Contains(got, "Delete/Backspace") {
		t.Fatalf("idle help = %q, want delete hint", got)
	}
	if !tui.chat.BeginSessionDelete(tui.currentSession.ID, "current", "active") {
		t.Fatal("BeginSessionDelete idle = false, want true")
	}
}

func TestNewRequiresExclusiveIdleCurrentSession(t *testing.T) {
	for _, tt := range []struct {
		name    string
		session protocol.SessionInfo
		want    bool
	}{
		{name: "exclusive idle", session: protocol.SessionInfo{ID: "current", Status: protocol.SessionStatusIdle, ClientCount: 1}, want: true},
		{name: "state operation", session: protocol.SessionInfo{ID: "current", Status: protocol.SessionStatusIdle, ClientCount: 1}, want: false},
		{name: "other client", session: protocol.SessionInfo{ID: "current", Status: protocol.SessionStatusIdle, ClientCount: 2}},
		{name: "running", session: protocol.SessionInfo{ID: "current", Status: protocol.SessionStatusRunning, ClientCount: 1}},
		{name: "waiting", session: protocol.SessionInfo{ID: "current", Status: protocol.SessionStatusWaiting, ClientCount: 1}},
		{name: "compacting", session: protocol.SessionInfo{ID: "current", Status: protocol.SessionStatusCompacting, ClientCount: 1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tui := &TUI{currentSession: protocol.SessionInfo{ID: "current"}, sessions: []protocol.SessionInfo{tt.session}}
			if tt.name == "state operation" {
				tui.chat.Loading = true
			}
			if got := tui.canReplaceCurrentSession(); got != tt.want {
				t.Fatalf("canReplaceCurrentSession = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeriveSessionTitleCleansCommonPrefix(t *testing.T) {
	got := deriveSessionTitle("请帮我修复 TUI 复制模式的问题\n第二行")
	want := "修复 TUI 复制模式的问题"
	if got != want {
		t.Fatalf("deriveSessionTitle = %q, want %q", got, want)
	}
}

func TestWelcomeNewStartsWhenCurrentCWDSessionExists(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name    string
		session protocol.SessionInfo
	}{
		{name: "idle", session: protocol.SessionInfo{ID: "idle", CWD: cwd, Status: protocol.SessionStatusIdle, MessageCount: 1}},
		{name: "active", session: protocol.SessionInfo{ID: "active", CWD: cwd, Status: protocol.SessionStatusRunning, MessageCount: 1}},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24, ready: true, providerName: "test", modelName: "model"}
			tui.sessions = []protocol.SessionInfo{tt.session}
			tui.pickWelcomeSessions()
			cmd := tui.handleWelcomeAction(welcomepage.ActionNew)
			if cmd == nil {
				t.Fatal("ActionNew returned nil, want create command")
			}
		})
	}
}

func TestSessionDeleteConfirmationKeepsOriginalTargetAfterRefresh(t *testing.T) {
	m := chatpage.Model{}
	m.SetSessionOverlay(
		[]protocol.SessionInfo{
			{ID: "current", Status: protocol.SessionStatusIdle, ClientCount: 1},
			{ID: "idle-one", Status: protocol.SessionStatusIdle},
			{ID: "idle-two", Status: protocol.SessionStatusIdle},
		},
		[]chatpage.SessionRowKind{
			chatpage.SessionRowCurrentWorkspace,
			chatpage.SessionRowIdleElsewhere,
			chatpage.SessionRowIdleElsewhere,
		},
	)
	if !m.BeginSessionDelete("current", "current", "active") {
		t.Fatal("BeginSessionDelete = false, want true")
	}

	// 通知刷新会重置 cursor，但不能改变已经确认的删除目标。
	m.SetSessionOverlay(
		[]protocol.SessionInfo{
			{ID: "current", Status: protocol.SessionStatusIdle, ClientCount: 1},
			{ID: "idle-two", Status: protocol.SessionStatusIdle},
			{ID: "idle-one", Status: protocol.SessionStatusIdle},
		},
		[]chatpage.SessionRowKind{
			chatpage.SessionRowCurrentWorkspace,
			chatpage.SessionRowIdleElsewhere,
			chatpage.SessionRowIdleElsewhere,
		},
	)
	action, ok := m.ConfirmSessionDelete()
	if !ok || action.ID != "idle-one" {
		t.Fatalf("ConfirmSessionDelete = (%+v, %v), want idle-one true", action, ok)
	}
}
