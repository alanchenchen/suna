package tui

import (
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/protocol"

	tea "charm.land/bubbletea/v2"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
)

func TestChatPrintableKeyUpdatesTextarea(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24, ready: true}
	tui.initChatComponents()

	_, cmd := tui.updateChat(tea.KeyPressMsg(tea.Key{Text: "你", Code: '你'}))
	if got := tui.chat.Textarea.Value(); got != "你" {
		t.Fatalf("textarea.Value() = %q, want %q", got, "你")
	}
	if cmd == nil {
		t.Fatalf("cmd = nil, want textarea update command")
	}
}

func TestChatPrintableKeyUpdatesCommandSuggestions(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24, ready: true}
	tui.initChatComponents()

	tui.updateChat(tea.KeyPressMsg(tea.Key{Text: "/", Code: '/'}))
	if got := tui.chat.Textarea.Value(); got != "/" {
		t.Fatalf("textarea.Value() = %q, want /", got)
	}
	if len(tui.chat.CmdSuggestions) == 0 {
		t.Fatalf("CmdSuggestions empty after typing slash")
	}
}

func TestGuardKeyBindingsRouteWithKeyPressMessages(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), mode: uipage.Chat, ready: true, width: 80, height: 24}
	tui.initChatComponents()
	tui.chat.EnqueueGuardConfirm(&chatpage.GuardConfirmView{ID: "guard-1", Tool: "writefile", Risk: "high"})

	_, _ = tui.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft}))
	if tui.chat.GuardCursor != 0 {
		t.Fatalf("GuardCursor = %d after left, want approve", tui.chat.GuardCursor)
	}
	_, _ = tui.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyPgDown}))
	if tui.chat.ActiveGuard() == nil {
		t.Fatal("ActiveGuard() = nil after PgDown")
	}
	_, _ = tui.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if tui.chat.ActiveGuard() != nil {
		t.Fatal("ActiveGuard() != nil after Esc rejection")
	}
}

func TestGuardLocksComposerAndBlocksTerminalSelection(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), mode: uipage.Chat, ready: true, width: 80, height: 24}
	tui.initChatComponents()
	tui.chat.EnqueueGuardConfirm(&chatpage.GuardConfirmView{ID: "guard-1", Tool: "writefile", Risk: "high"})
	_ = tui.syncInputFocus()

	if !tui.inputLocked() {
		t.Fatal("inputLocked() = false while guard confirmation is active")
	}
	if tui.chat.Textarea.Focused() {
		t.Fatal("textarea.Focused() = true while guard confirmation is active")
	}
	view := stripANSIForTest(tui.renderInputArea())
	if !strings.Contains(view, "正在等待安全确认") {
		t.Fatalf("renderInputArea() = %q, want guard waiting state", view)
	}

	_, _ = tui.Update(tea.KeyPressMsg(tea.Key{Code: 's', Mod: tea.ModCtrl}))
	if tui.selectionMode {
		t.Fatal("selectionMode = true while guard confirmation is active")
	}
}

func TestGuardNotificationExitsTerminalSelection(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), mode: uipage.Chat, ready: true, width: 80, height: 24, currentRunCanControl: true, currentSession: protocol.SessionInfo{ID: "session-1", Status: protocol.SessionStatusRunning}}
	tui.initChatComponents()
	tui.selectionMode = true

	tui.handleGuardConfirmNotification(protocol.GuardConfirmParams{ID: "guard-1", Tool: "writefile", SessionID: "session-1", CanReply: true})
	if tui.selectionMode {
		t.Fatal("selectionMode = true after guard notification")
	}
	if tui.chat.Textarea.Focused() {
		t.Fatal("textarea.Focused() = true after guard notification")
	}
	if tui.chat.ActiveGuard() == nil {
		t.Fatal("ActiveGuard() = nil after guard notification")
	}
}

func TestGuardDecisionRestoresFocusForQueuedCustomAsk(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), mode: uipage.Chat, ready: true, width: 80, height: 24}
	tui.initChatComponents()
	tui.chat.EnqueueGuardConfirm(&chatpage.GuardConfirmView{ID: "guard-1", Tool: "writefile", Risk: "high"})
	tui.chat.EnqueueAskUser(protocol.AskUserParams{ID: "ask-1", Question: "continue?", AllowCustom: true})
	tui.chat.Textarea.Blur()

	_ = tui.submitGuardDecision("approve")
	if tui.chat.ActiveAsk() == nil || !tui.chat.ActiveAsk().AllowCustom {
		t.Fatal("custom AskUser did not become the active interaction")
	}
	if !tui.chat.Textarea.Focused() {
		t.Fatal("textarea.Focused() = false after guard decision advances to custom AskUser")
	}
}

func TestInteractionResolvedRestoresFocusForQueuedCustomAsk(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), mode: uipage.Chat, ready: true, width: 80, height: 24}
	tui.initChatComponents()
	tui.chat.EnqueueGuardConfirm(&chatpage.GuardConfirmView{ID: "guard-1", Tool: "writefile", Risk: "high"})
	tui.chat.EnqueueAskUser(protocol.AskUserParams{ID: "ask-1", Question: "continue?", AllowCustom: true})
	tui.chat.Textarea.Blur()

	tui.handleInteractionResolvedNotification(protocol.InteractionResolvedParams{ID: "guard-1"})
	if tui.chat.ActiveAsk() == nil || !tui.chat.ActiveAsk().AllowCustom {
		t.Fatal("custom AskUser did not become the active interaction")
	}
	if !tui.chat.Textarea.Focused() {
		t.Fatal("textarea.Focused() = false after queued custom AskUser becomes active")
	}
}

func TestChoiceOnlyAskUserNotificationExitsTerminalSelection(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), mode: uipage.Chat, ready: true, width: 80, height: 24, currentSession: protocol.SessionInfo{ID: "session-1", Status: protocol.SessionStatusWaiting}}
	tui.initChatComponents()
	tui.selectionMode = true

	tui.handleAskUserNotification(protocol.AskUserParams{ID: "ask-1", SessionID: "session-1", Question: "continue?", Options: []string{"yes", "no"}, CanReply: true})
	if tui.selectionMode {
		t.Fatal("selectionMode = true after AskUser notification")
	}
	if tui.chat.Textarea.Focused() {
		t.Fatal("textarea.Focused() = true after choice-only AskUser notification")
	}
	if tui.chat.ActiveAsk() == nil {
		t.Fatal("ActiveAsk() = nil after AskUser notification")
	}
}

func TestCustomAskUserNotificationRestoresComposerFocus(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), mode: uipage.Chat, ready: true, width: 80, height: 24, currentSession: protocol.SessionInfo{ID: "session-1", Status: protocol.SessionStatusWaiting}}
	tui.initChatComponents()
	tui.selectionMode = true

	tui.handleAskUserNotification(protocol.AskUserParams{ID: "ask-1", SessionID: "session-1", Question: "continue?", AllowCustom: true, CanReply: true})
	if tui.selectionMode {
		t.Fatal("selectionMode = true after custom AskUser notification")
	}
	if !tui.chat.Textarea.Focused() {
		t.Fatal("textarea.Focused() = false after custom AskUser notification")
	}
}

func TestSelectionModeShowsReadOnlyComposerAndRestoresFocus(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), mode: uipage.Chat, ready: true, width: 80, height: 24}
	tui.initChatComponents()

	_, _ = tui.Update(tea.KeyPressMsg(tea.Key{Code: 's', Mod: tea.ModCtrl}))
	if !tui.selectionMode {
		t.Fatal("selectionMode = false after Ctrl+S")
	}
	if tui.chat.Textarea.Focused() {
		t.Fatal("textarea.Focused() = true while terminal selection is active")
	}
	view := stripANSIForTest(tui.renderInputArea())
	if !strings.Contains(view, "正在选择终端文本") {
		t.Fatalf("renderInputArea() = %q, want terminal selection state", view)
	}
	if strings.Contains(view, "Esc 取消") {
		t.Fatalf("renderInputArea() = %q, should not advertise run cancellation during terminal selection", view)
	}

	_, _ = tui.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if tui.selectionMode {
		t.Fatal("selectionMode = true after Esc")
	}
	if !tui.chat.Textarea.Focused() {
		t.Fatal("textarea.Focused() = false after terminal selection exits")
	}
}

func TestSelectionModeIgnoresWheelAsHistoryKeys(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), mode: uipage.Chat, ready: true, width: 80, height: 24}
	tui.initChatComponents()
	tui.chat.Messages = []chatpage.Msg{{Role: "user", Content: "previous prompt"}}
	tui.selectionMode = true

	_, _ = tui.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyUp}))
	if got := tui.chat.Textarea.Value(); got != "" {
		t.Fatalf("Textarea.Value() = %q, want empty while selection mode ignores up", got)
	}
	if tui.selectionMode != true {
		t.Fatal("selectionMode changed after ignored up key")
	}

	_, _ = tui.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if tui.selectionMode {
		t.Fatal("selectionMode = true after Esc, want false")
	}
}

func TestLateGuardAfterCancelledRunDoesNotRestoreLoading(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), mode: uipage.Chat, ready: true, width: 80, height: 24, currentSession: protocol.SessionInfo{ID: "session-1", Status: protocol.SessionStatusRunning}, currentRunCanControl: true}
	tui.initChatComponents()
	tui.chat.Loading = true

	tui.handleAgentRunNotification(protocol.AgentRunParams{RunID: "run-1", State: protocol.AgentRunCancelled})
	tui.currentSession.Status = protocol.SessionStatusIdle
	tui.handleGuardConfirmNotification(protocol.GuardConfirmParams{ID: "guard-late", SessionID: "session-1", Tool: "writefile", CanReply: true})

	if tui.chat.ActiveGuard() != nil {
		t.Fatal("late Guard was shown after cancelled run")
	}
	if tui.chat.Loading || tui.currentRunCanControl || tui.cancelling {
		t.Fatalf("loading/control/cancelling = %v/%v/%v, want false/false/false", tui.chat.Loading, tui.currentRunCanControl, tui.cancelling)
	}
}

func TestCancelledRunClearsVisibleGuard(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), mode: uipage.Chat, ready: true, width: 80, height: 24, currentSession: protocol.SessionInfo{ID: "session-1", Status: protocol.SessionStatusRunning}, currentRunCanControl: true}
	tui.initChatComponents()
	tui.chat.EnqueueGuardConfirm(&chatpage.GuardConfirmView{ID: "guard-1", Tool: "writefile"})

	tui.handleAgentRunNotification(protocol.AgentRunParams{RunID: "run-1", State: protocol.AgentRunCancelled})

	if tui.chat.ActiveGuard() != nil || tui.chat.HasBlockingInteraction() {
		t.Fatal("cancelled run retained Guard interaction")
	}
	if tui.chat.Loading || tui.currentRunCanControl {
		t.Fatalf("loading/control = %v/%v, want false/false", tui.chat.Loading, tui.currentRunCanControl)
	}
}

func TestRejectingStaleGuardCannotReviveCompletedRun(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), mode: uipage.Chat, ready: true, width: 80, height: 24, currentSession: protocol.SessionInfo{ID: "session-1", Status: protocol.SessionStatusIdle}, completedRunID: "run-1"}
	tui.initChatComponents()
	// 模拟终态与 Guard notification 已经交错入队，确保提交动作本身也有最终兜底。
	tui.chat.EnqueueGuardConfirm(&chatpage.GuardConfirmView{ID: "guard-stale", Tool: "writefile"})

	_ = tui.submitGuardDecision("reject")

	if tui.chat.Loading || tui.currentRunCanControl || tui.chat.Phase != chatpage.PhaseIdle {
		t.Fatalf("loading/control/phase = %v/%v/%v, want false/false/idle", tui.chat.Loading, tui.currentRunCanControl, tui.chat.Phase)
	}
	if tui.chat.ActiveGuard() != nil {
		t.Fatal("stale Guard remained after rejection")
	}
}

func TestHandoffObserverAcceptsReplyableGuardForActiveSession(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), mode: uipage.Chat, ready: true, width: 80, height: 24, currentSession: protocol.SessionInfo{ID: "session-1", Status: protocol.SessionStatusWaiting}, currentRunCanControl: false}
	tui.initChatComponents()

	tui.handleGuardConfirmNotification(protocol.GuardConfirmParams{ID: "guard-handoff", SessionID: "session-1", Tool: "writefile", CanReply: true})

	if tui.chat.ActiveGuard() == nil {
		t.Fatal("replyable handoff Guard was rejected for active session")
	}
}

func TestLateAskAfterCancelledRunDoesNotRestoreLoading(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), mode: uipage.Chat, ready: true, width: 80, height: 24, currentSession: protocol.SessionInfo{ID: "session-1", Status: protocol.SessionStatusWaiting}, completedRunID: "run-1"}
	tui.initChatComponents()

	tui.handleAskUserNotification(protocol.AskUserParams{ID: "ask-late", SessionID: "session-1", Question: "continue?", CanReply: true})

	if tui.chat.ActiveAsk() != nil {
		t.Fatal("late AskUser was shown after cancelled run")
	}
	if tui.chat.Loading || tui.currentRunCanControl {
		t.Fatalf("loading/control = %v/%v, want false/false", tui.chat.Loading, tui.currentRunCanControl)
	}
}

func TestCancelThenLateGuardRejectLeavesChatIdle(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), mode: uipage.Chat, ready: true, width: 80, height: 24, currentSession: protocol.SessionInfo{ID: "session-1", Status: protocol.SessionStatusRunning}, currentRunCanControl: true}
	tui.initChatComponents()
	tui.chat.Loading = true

	tui.enterCancelling()
	tui.handleAgentRunNotification(protocol.AgentRunParams{RunID: "run-1", State: protocol.AgentRunCancelled})
	tui.handleGuardConfirmNotification(protocol.GuardConfirmParams{ID: "guard-late", SessionID: "session-1", Tool: "writefile", CanReply: true})
	if tui.chat.ActiveGuard() != nil {
		_ = tui.submitGuardDecision("reject")
	}

	if tui.chat.Loading || tui.cancelling || tui.currentRunCanControl || tui.chat.Phase != chatpage.PhaseIdle {
		t.Fatalf("loading/cancelling/control/phase = %v/%v/%v/%v, want false/false/false/idle", tui.chat.Loading, tui.cancelling, tui.currentRunCanControl, tui.chat.Phase)
	}
	if tui.chat.HasBlockingInteraction() {
		t.Fatal("cancelled run retained a blocking interaction")
	}
}

func TestLateObserverInteractionsAfterCancelledRunAreIgnored(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), mode: uipage.Chat, ready: true, width: 80, height: 24, currentSession: protocol.SessionInfo{ID: "session-1", Status: protocol.SessionStatusWaiting}, completedRunID: "run-1"}
	tui.initChatComponents()

	tui.handleAskUserNotification(protocol.AskUserParams{ID: "ask-late", SessionID: "session-1", Question: "continue?", CanReply: false})
	tui.handleGuardConfirmNotification(protocol.GuardConfirmParams{ID: "guard-late", SessionID: "session-1", Tool: "writefile", CanReply: false})

	if len(tui.chat.Messages) != 0 {
		t.Fatalf("late observer interactions appended %d messages, want 0", len(tui.chat.Messages))
	}
	if tui.chat.HasBlockingInteraction() || tui.chat.Loading {
		t.Fatalf("blocking/loading = %v/%v, want false/false", tui.chat.HasBlockingInteraction(), tui.chat.Loading)
	}
}
