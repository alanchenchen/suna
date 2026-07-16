package tui

import (
	"errors"
	"testing"

	"github.com/alanchenchen/suna/internal/protocol"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
)

func TestSessionMetadataResultUpdatesTitleWithoutRestoringTranscript(t *testing.T) {
	tui := &TUI{
		mode:           uipage.Chat,
		currentSession: protocol.SessionInfo{ID: "session-1", Title: "old", ModelRef: "openai/gpt-session"},
		chat:           chatpage.Model{Messages: []chatpage.Msg{{Role: "user", Content: "in-flight message"}}},
	}

	tui.handleProtocolResultMsg(sessionMetadataResultMsg{Session: protocol.SessionInfo{ID: "session-1", Title: "new", ModelRef: "openai/gpt-session"}})

	if got := tui.currentSession.Title; got != "new" {
		t.Fatalf("current session title = %q, want new", got)
	}
	if got := len(tui.chat.Messages); got != 1 {
		t.Fatalf("chat message count = %d, want in-flight transcript retained", got)
	}
	if tui.mode != uipage.Chat {
		t.Fatalf("mode = %v, want chat", tui.mode)
	}
}

func TestAutoTitleFailureRestoresOptimisticTitleWithoutRestoringTranscript(t *testing.T) {
	tui := &TUI{
		i18n:           newTranslator(LocaleZH),
		mode:           uipage.Chat,
		currentSession: protocol.SessionInfo{ID: "session-1", Title: "old", CWD: "/tmp/old"},
		sessions:       []protocol.SessionInfo{{ID: "session-1", Title: "old", CWD: "/tmp/old"}},
		chat:           chatpage.Model{Messages: []chatpage.Msg{{Role: "user", Content: "in-flight message"}}},
	}

	cmd := tui.maybeAutoTitleSessionCmd("修复标题更新")
	if cmd == nil {
		t.Fatal("maybeAutoTitleSessionCmd() = nil, want title update command")
	}
	if got := tui.currentSession.Title; got != "修复标题更新" {
		t.Fatalf("optimistic current title = %q, want updated title", got)
	}

	msg := cmd()
	result, ok := msg.(sessionTitleUpdateResultMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want sessionTitleUpdateResultMsg", msg)
	}
	if result.Err == nil || result.SessionID != "session-1" || result.OldTitle != "old" || result.OptimisticTitle != "修复标题更新" {
		t.Fatalf("failure result = %+v, want session ID, old title, optimistic title, and error", result)
	}
	tui.handleProtocolResultMsg(result)

	if got := tui.currentSession.Title; got != "old" {
		t.Fatalf("current session title after failure = %q, want old", got)
	}
	if got := tui.sessions[0].Title; got != "old" {
		t.Fatalf("listed session title after failure = %q, want old", got)
	}
	if got := len(tui.chat.Messages); got != 1 {
		t.Fatalf("chat message count = %d, want in-flight transcript retained", got)
	}
	if tui.mode != uipage.Chat {
		t.Fatalf("mode = %v, want chat", tui.mode)
	}
}

func TestAutoTitleFailureDoesNotOverwriteNewerTitle(t *testing.T) {
	tui := &TUI{
		currentSession: protocol.SessionInfo{ID: "session-1", Title: "newer"},
		sessions:       []protocol.SessionInfo{{ID: "session-1", Title: "newer"}},
	}

	tui.handleProtocolResultMsg(sessionTitleUpdateResultMsg{
		SessionID:       "session-1",
		OldTitle:        "old",
		OptimisticTitle: "optimistic",
		Err:             errors.New("rejected"),
	})

	if got := tui.currentSession.Title; got != "newer" {
		t.Fatalf("current session title = %q, want newer title retained", got)
	}
	if got := tui.sessions[0].Title; got != "newer" {
		t.Fatalf("listed session title = %q, want newer title retained", got)
	}
}

func TestAutoTitleSuccessAppliesOnlyMetadata(t *testing.T) {
	tui := &TUI{
		mode:           uipage.Chat,
		currentSession: protocol.SessionInfo{ID: "session-1", Title: "optimistic"},
		sessions:       []protocol.SessionInfo{{ID: "session-1", Title: "optimistic"}},
		chat:           chatpage.Model{Messages: []chatpage.Msg{{Role: "user", Content: "in-flight message"}}},
	}

	tui.handleProtocolResultMsg(sessionTitleUpdateResultMsg{
		SessionID:       "session-1",
		OldTitle:        "old",
		OptimisticTitle: "optimistic",
		Session:         protocol.SessionInfo{ID: "session-1", Title: "persisted"},
	})

	if got := tui.currentSession.Title; got != "persisted" {
		t.Fatalf("current session title = %q, want persisted metadata title", got)
	}
	if got := len(tui.chat.Messages); got != 1 {
		t.Fatalf("chat message count = %d, want in-flight transcript retained", got)
	}
}

func TestModelPickerMarksCurrentSessionModelInsteadOfDefault(t *testing.T) {
	tui := &TUI{
		currentSession: protocol.SessionInfo{ID: "session-1", ModelRef: "anthropic/claude-session"},
		configState: protocol.ConfigParams{
			ActiveModel: "openai/gpt-default",
			Models: []protocol.ConfigModel{
				{Provider: "openai", Model: "gpt-default"},
				{Provider: "anthropic", Model: "claude-session"},
			},
		},
	}

	if tui.isCurrentSessionModelRef("openai/gpt-default") {
		t.Fatal("default model must not be marked as current session model")
	}
	if !tui.isCurrentSessionModelRef("anthropic/claude-session") {
		t.Fatal("current session model must be marked in chat picker")
	}
	if !tui.isDefaultModelRef("openai/gpt-default") {
		t.Fatal("default model must remain marked in config")
	}
}
