package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/alanchenchen/suna/internal/protocol"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
)

func TestSteeringNotificationsKeepFIFOAndRestoreTerminalMessageOnce(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 90, height: 24, activeRunID: "run-1", currentRunCanControl: true}
	tui.initChatComponents()
	tui.chat.Loading = true
	tui.chat.SteeringSubmissions = []chatpage.SteeringSubmission{{ClientMsgID: "client-1", Text: "first"}, {ClientMsgID: "client-2", Text: "second"}}

	second := protocol.SteeringMessage{ID: "id-2", RunID: "run-1", ClientMsgID: "client-2", State: protocol.SteeringQueued, Sequence: 2, CanControl: true, Parts: textPartsForTUI("second")}
	first := protocol.SteeringMessage{ID: "id-1", RunID: "run-1", ClientMsgID: "client-1", State: protocol.SteeringQueued, Sequence: 1, CanControl: true, Parts: textPartsForTUI("first")}
	tui.handleSteeringNotification(second)
	tui.handleSteeringNotification(first)
	if len(tui.chat.PendingSteering) != 2 || tui.chat.PendingSteering[0].ID != "id-1" || len(tui.chat.SteeringSubmissions) != 0 {
		t.Fatalf("queue/submissions = %#v / %#v", tui.chat.PendingSteering, tui.chat.SteeringSubmissions)
	}

	removed := first
	removed.State = protocol.SteeringRemoved
	tui.handleSteeringNotification(removed)
	if got := strings.TrimSpace(tui.chat.Textarea.Value()); got != "first" {
		t.Fatalf("draft after remove = %q, want first", got)
	}
	tui.handleProtocolResultMsg(steerRemoveResultMsg{Message: removed})
	if got := strings.TrimSpace(tui.chat.Textarea.Value()); got != "first" {
		t.Fatalf("draft after duplicate response = %q, want one copy", got)
	}
}

func TestSteeringResponseAfterNotificationDoesNotRestoreAcceptedMessage(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), activeRunID: "run-1"}
	tui.initChatComponents()
	tui.chat.SteeringSubmissions = []chatpage.SteeringSubmission{{ClientMsgID: "client-1", Text: "queued"}}
	queued := protocol.SteeringMessage{ID: "id-1", RunID: "run-1", ClientMsgID: "client-1", State: protocol.SteeringQueued, Sequence: 1, CanControl: true, Parts: textPartsForTUI("queued")}
	tui.handleSteeringNotification(queued)
	tui.handleProtocolResultMsg(steerResultMsg{ClientMsgID: "client-1", Err: errors.New("response timeout")})
	if got := strings.TrimSpace(tui.chat.Textarea.Value()); got != "" {
		t.Fatalf("draft = %q, want accepted message not restored", got)
	}
}

func TestSteeringTerminalStateIgnoresLateQueuedResponse(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), activeRunID: "run-1"}
	tui.initChatComponents()
	applied := protocol.SteeringMessage{ID: "id-1", RunID: "run-1", ClientMsgID: "client-1", State: protocol.SteeringApplied, Sequence: 7, CanControl: true, Parts: textPartsForTUI("applied")}
	tui.handleSteeringNotification(applied)
	queued := applied
	queued.State = protocol.SteeringQueued
	tui.handleProtocolResultMsg(steerResultMsg{Message: queued, ClientMsgID: "client-1"})
	if len(tui.chat.PendingSteering) != 0 {
		t.Fatalf("pending after late queued = %#v, want empty", tui.chat.PendingSteering)
	}
}

func TestRejectedSteeringRestoresMultipleMessagesInFIFOOrder(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), activeRunID: "run-1"}
	tui.initChatComponents()
	// Agent emits rejected events in reverse queue order so prepend-based draft restoration preserves FIFO.
	second := protocol.SteeringMessage{ID: "id-2", RunID: "run-1", State: protocol.SteeringRejected, Sequence: 2, CanControl: true, Parts: textPartsForTUI("second")}
	first := protocol.SteeringMessage{ID: "id-1", RunID: "run-1", State: protocol.SteeringRejected, Sequence: 1, CanControl: true, Parts: textPartsForTUI("first")}
	tui.handleSteeringNotification(second)
	tui.handleSteeringNotification(first)
	if got := strings.TrimSpace(tui.chat.Textarea.Value()); got != "first\nsecond" {
		t.Fatalf("restored draft = %q, want FIFO", got)
	}
}

func TestSameRunSnapshotPreservesSteeringTerminalTombstone(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), activeRunID: "run-1", currentSession: protocol.SessionInfo{ID: "session-1"}}
	tui.initChatComponents()
	tui.chat.SteeringTerminal = map[string]protocol.SteeringState{"id-1": protocol.SteeringApplied}
	tui.applySessionSnapshot(protocol.SessionSnapshot{Session: protocol.SessionInfo{ID: "session-1", Status: protocol.SessionStatusRunning}, CurrentRun: &protocol.CurrentRunView{RunID: "run-1", State: protocol.AgentRunRunning, Status: protocol.SessionStatusRunning, CanControl: true}})
	queued := protocol.SteeringMessage{ID: "id-1", RunID: "run-1", State: protocol.SteeringQueued, Sequence: 1, CanControl: true, Parts: textPartsForTUI("late")}
	tui.handleSteeringNotification(queued)
	if len(tui.chat.PendingSteering) != 0 {
		t.Fatalf("pending after same-run snapshot and late queued = %#v", tui.chat.PendingSteering)
	}
}

func TestObserverRejectedSteeringDoesNotRestoreDraft(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), activeRunID: "run-1"}
	tui.initChatComponents()
	tui.handleSteeringNotification(protocol.SteeringMessage{ID: "id-1", RunID: "run-1", State: protocol.SteeringRejected, CanControl: false, Parts: textPartsForTUI("owner message")})
	if got := strings.TrimSpace(tui.chat.Textarea.Value()); got != "" {
		t.Fatalf("observer draft = %q, want empty", got)
	}
}

func TestRunningSlashInputDoesNotExecuteLocalCommand(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24, activeRunID: "run-1", currentRunCanControl: true}
	tui.initChatComponents()
	tui.chat.Loading = true
	tui.chat.Textarea.SetValue("/compact")
	cmd := tui.handleSend()
	if cmd == nil {
		t.Fatal("handleSend() command = nil, want steering request")
	}
	if tui.chat.Compacting {
		t.Fatal("running /compact activated local compact")
	}
	if len(tui.chat.SteeringSubmissions) != 1 || tui.chat.SteeringSubmissions[0].Text != "/compact" {
		t.Fatalf("submissions = %#v, want /compact message", tui.chat.SteeringSubmissions)
	}
}

func TestRunningSteeringDisablesImagePasteButKeepsTextPaste(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24, activeRunID: "run-1", currentRunCanControl: true, ready: true}
	tui.initChatComponents()
	tui.chat.Loading = true

	_, cmd := tui.updateChatKeyNormal("ctrl+v", tea.KeyPressMsg{})
	if cmd != nil {
		t.Fatal("running Ctrl+V command != nil, want image clipboard read disabled")
	}
	imagePath := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(imagePath, []byte{0x89, 'P', 'N', 'G'}, 0o600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}
	model, _ := tui.Update(tea.PasteMsg{Content: imagePath})
	tui = model.(*TUI)
	if tui.chat.ActiveImagePaste() != nil || strings.Contains(tui.chat.Textarea.Value(), imagePath) {
		t.Fatalf("image paste state/text = %#v/%q, want rejected", tui.chat.ActiveImagePaste(), tui.chat.Textarea.Value())
	}
	model, _ = tui.Update(tea.PasteMsg{Content: "plain text"})
	tui = model.(*TUI)
	if got := tui.chat.Textarea.Value(); got != "plain text" {
		t.Fatalf("text paste = %q, want plain text", got)
	}
	data := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 'I', 'H', 'D', 'R'}
	model, _ = tui.Update(clipboardImagePasteMsg{StartedAt: time.Now(), Pending: pendingImagePaste{SourceKind: "clipboard_image", Name: "clip.png", MimeType: "image/png", Data: data}})
	tui = model.(*TUI)
	if tui.chat.ActiveImagePaste() != nil {
		t.Fatal("late clipboard image entered confirmation during run")
	}
}

func TestNewRunClearsOldSteeringAndRejectsLateOldNotification(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), activeRunID: "run-old", completedRunID: "run-old", currentRunCanControl: true}
	tui.initChatComponents()
	tui.chat.PendingSteering = []protocol.SteeringMessage{{ID: "old-id", RunID: "run-old", State: protocol.SteeringQueued}}
	tui.chat.SteeringSubmissions = []chatpage.SteeringSubmission{{ClientMsgID: "old-client", Text: "old"}}
	tui.chat.SteeringTerminal = map[string]protocol.SteeringState{"old-terminal": protocol.SteeringApplied}

	tui.handleAgentRunNotification(protocol.AgentRunParams{RunID: "run-new", State: protocol.AgentRunRunning, CanControl: true})
	if tui.activeRunID != "run-new" || len(tui.chat.PendingSteering) != 0 || len(tui.chat.SteeringSubmissions) != 0 || len(tui.chat.SteeringTerminal) != 0 {
		t.Fatalf("new run state = run %q, pending %#v, submissions %#v, terminal %#v", tui.activeRunID, tui.chat.PendingSteering, tui.chat.SteeringSubmissions, tui.chat.SteeringTerminal)
	}
	if got := strings.TrimSpace(tui.chat.Textarea.Value()); got != "old" {
		t.Fatalf("new run restored draft = %q, want old", got)
	}
	late := protocol.SteeringMessage{ID: "late", RunID: "run-old", State: protocol.SteeringQueued, Parts: textPartsForTUI("late")}
	tui.handleSteeringNotification(late)
	if len(tui.chat.PendingSteering) != 0 {
		t.Fatalf("late old-run notification restored queue: %#v", tui.chat.PendingSteering)
	}
}

func TestTerminalRunRestoresUnconfirmedSubmissionAndIgnoresLateResult(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), activeRunID: "run-1"}
	tui.initChatComponents()
	tui.chat.SteeringSubmissions = []chatpage.SteeringSubmission{{ClientMsgID: "client-1", Text: "pending"}}
	tui.handleAgentRunNotification(protocol.AgentRunParams{RunID: "run-1", State: protocol.AgentRunDone})
	if got := strings.TrimSpace(tui.chat.Textarea.Value()); got != "pending" {
		t.Fatalf("terminal restored draft = %q, want pending", got)
	}
	if len(tui.chat.SteeringSubmissions) != 0 {
		t.Fatalf("terminal submissions = %#v, want empty", tui.chat.SteeringSubmissions)
	}
	tui.handleProtocolResultMsg(steerResultMsg{ClientMsgID: "client-1", Err: errors.New("late")})
	if got := strings.TrimSpace(tui.chat.Textarea.Value()); got != "pending" {
		t.Fatalf("late result draft = %q, want one copy", got)
	}
}

func TestOldRunTerminalDoesNotClearNewRunSteering(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), activeRunID: "run-new", currentRunCanControl: true}
	tui.initChatComponents()
	tui.chat.Loading = true
	tui.chat.PendingSteering = []protocol.SteeringMessage{{ID: "new-id", RunID: "run-new", State: protocol.SteeringQueued}}
	tui.chat.SteeringSubmissions = []chatpage.SteeringSubmission{{ClientMsgID: "new-client", Text: "new"}}

	tui.handleAgentRunNotification(protocol.AgentRunParams{RunID: "run-old", State: protocol.AgentRunDone})
	if tui.activeRunID != "run-new" || !tui.chat.Loading || len(tui.chat.PendingSteering) != 1 || len(tui.chat.SteeringSubmissions) != 1 {
		t.Fatalf("state after old terminal = run %q loading %v pending %#v submissions %#v", tui.activeRunID, tui.chat.Loading, tui.chat.PendingSteering, tui.chat.SteeringSubmissions)
	}
}

func TestCompletedRunRejectsLateSteeringNotification(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), activeRunID: "run-1"}
	tui.initChatComponents()
	tui.handleAgentRunNotification(protocol.AgentRunParams{RunID: "run-1", State: protocol.AgentRunDone})
	tui.handleSteeringNotification(protocol.SteeringMessage{ID: "late", RunID: "run-1", State: protocol.SteeringQueued, Parts: textPartsForTUI("late")})
	if len(tui.chat.PendingSteering) != 0 {
		t.Fatalf("late terminal notification restored queue: %#v", tui.chat.PendingSteering)
	}
}

func TestQueuePreviewIsSingleLineAndUndoWaitsForLatestSubmission(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 90, height: 24, activeRunID: "run-1", currentRunCanControl: true}
	tui.initChatComponents()
	tui.chat.Loading = true
	tui.chat.PendingSteering = []protocol.SteeringMessage{{ID: "id-1", RunID: "run-1", State: protocol.SteeringQueued, CanControl: true, Parts: textPartsForTUI("first\nline")}}

	confirmed := stripANSIForTest(tui.renderSteeringQueueLine(86))
	if strings.Contains(confirmed, "\n") || !strings.Contains(confirmed, "first line") || !strings.Contains(confirmed, "Ctrl+Z") {
		t.Fatalf("confirmed queue preview = %q, want one line with undo", confirmed)
	}

	tui.chat.SteeringSubmissions = []chatpage.SteeringSubmission{{ClientMsgID: "client-2", Text: "second\nline"}}
	submitting := stripANSIForTest(tui.renderSteeringQueueLine(86))
	if strings.Contains(submitting, "\n") || !strings.Contains(submitting, "second line") || strings.Contains(submitting, "Ctrl+Z") {
		t.Fatalf("submitting queue preview = %q, want one line without undo", submitting)
	}
	_, cmd := tui.updateChatKeyNormal("ctrl+z", tea.KeyPressMsg{})
	if cmd != nil {
		t.Fatal("Ctrl+Z command != nil while latest message is still submitting")
	}
	if len(tui.chat.PendingSteering) != 1 {
		t.Fatalf("pending after ignored Ctrl+Z = %#v", tui.chat.PendingSteering)
	}
}

func TestCancellingComposerKeepsSpinnerStatusWithoutSecondCancelHint(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24, activeRunID: "run-1", currentRunCanControl: true}
	tui.initChatComponents()
	tui.chat.Loading = true
	tui.enterCancelling()

	view := stripANSIForTest(tui.renderInputArea())
	if !strings.Contains(view, strings.TrimSpace(stripANSIForTest(tui.chat.Spinner.View()))) || !strings.Contains(view, "正在取消") {
		t.Fatalf("cancelling composer = %q, want spinner and cancelling status", view)
	}
	if strings.Contains(view, "Esc 取消") {
		t.Fatalf("cancelling composer = %q, should not advertise a second cancellation", view)
	}
}

func TestCompactingRunSnapshotRestoresCompactComposer(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 90, height: 24, currentSession: protocol.SessionInfo{ID: "session-1"}}
	tui.initChatComponents()
	snapshot := protocol.SessionSnapshot{
		Session:    protocol.SessionInfo{ID: "session-1", Status: protocol.SessionStatusCompacting},
		CurrentRun: &protocol.CurrentRunView{RunID: "run-1", State: protocol.AgentRunRunning, Status: protocol.SessionStatusCompacting, CanControl: true},
	}
	tui.applySessionSnapshot(snapshot)
	if !tui.chat.Compacting || tui.compactStartedAt.IsZero() || tui.inputLocked() {
		t.Fatalf("compact snapshot state = compacting %v, started %v, locked %v", tui.chat.Compacting, tui.compactStartedAt, tui.inputLocked())
	}
	view := stripANSIForTest(tui.renderInputArea())
	if !strings.Contains(view, "上下文压缩中") || !strings.Contains(view, strings.TrimSpace(stripANSIForTest(tui.chat.Spinner.View()))) {
		t.Fatalf("compact snapshot composer = %q, want compact spinner status", view)
	}
}

func TestObserverCannotSendSteering(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), activeRunID: "run-1", currentRunCanControl: false, currentSession: protocol.SessionInfo{ID: "session-1"}}
	tui.initChatComponents()
	tui.chat.Loading = true
	if tui.canSteerCurrentRun() || !tui.inputLocked() {
		t.Fatalf("observer canSteer/inputLocked = %v/%v, want false/true", tui.canSteerCurrentRun(), tui.inputLocked())
	}
}

func TestAutomaticCompactUsesIndependentElapsedTimeAndKeepsComposerEditable(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 90, height: 24, activeRunID: "run-1", currentRunCanControl: true}
	tui.initChatComponents()
	tui.chat.Loading = true
	tui.runStartedAt = time.Now().Add(-5 * time.Minute)
	tui.handleCompactResultNotification(protocol.CompactResult{Running: boolPtrForTUI(true)})
	tui.compactStartedAt = time.Now().Add(-61 * time.Second)
	if tui.inputLocked() {
		t.Fatal("automatic compact inputLocked() = true, want steering composer")
	}
	view := stripANSIForTest(tui.renderInputArea())
	if !strings.Contains(view, strings.TrimSpace(stripANSIForTest(tui.chat.Spinner.View()))) {
		t.Fatalf("compact composer = %q, want spinner in editable compact status", view)
	}
	if !strings.Contains(view, "上下文压缩中") || !strings.Contains(view, "1m1s") || strings.Contains(view, "5m") {
		t.Fatalf("compact composer = %q, want independent compact duration", view)
	}
}

func textPartsForTUI(text string) []protocol.MessagePart {
	return []protocol.MessagePart{{Type: "text", Text: text}}
}

func boolPtrForTUI(value bool) *bool { return &value }
