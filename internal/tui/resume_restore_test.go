package tui

import (
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/protocol"
)

func TestStreamErrorUsesStructuredResumeFlag(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 18}
	tui.initChatComponents()

	tui.handleStreamNotification(protocol.StreamParams{Done: true, Error: true, Chunk: "502 Bad Gateway", ResumeAvailable: true})

	if !tui.chat.ResumeAvailable {
		t.Fatal("ResumeAvailable = false, want true")
	}
	if got := len(tui.chat.Messages); got != 1 {
		t.Fatalf("messages = %d, want 1", got)
	}
	if got := tui.chat.Messages[0].Role; got != "error" {
		t.Fatalf("message role = %q, want error", got)
	}
	if got := tui.chat.Messages[0].Content; got != "502 Bad Gateway" {
		t.Fatalf("message content = %v, want structured error chunk", got)
	}
}

func TestRestoreStatusShowsCompactedNoticeAtEnd(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 18}
	tui.initChatComponents()
	tui.handleSessionRestoreMessageNotification(sessionRestoreMessageMsg{Role: "user", Content: "之前的问题"})

	tui.handleSessionRestoreStatusNotification(protocol.SessionRestoreStatus{Messages: 1, Compacted: true})

	if got := len(tui.chat.Messages); got != 2 {
		t.Fatalf("messages = %d, want 2", got)
	}
	last := tui.chat.Messages[len(tui.chat.Messages)-1]
	if last.Role != "system" {
		t.Fatalf("last role = %q, want system", last.Role)
	}
	content, _ := last.Content.(string)
	if !strings.Contains(content, "Session State") {
		t.Fatalf("compacted notice = %q, want Session State explanation", content)
	}
}
