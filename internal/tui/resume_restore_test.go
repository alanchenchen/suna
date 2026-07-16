package tui

import (
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/protocol"
)

func TestStreamErrorUsesStructuredResumeFlag(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 18}
	tui.initChatComponents()

	tui.handleAgentRunNotification(protocol.AgentRunParams{State: protocol.AgentRunFailed, Error: &protocol.ModelError{Kind: protocol.ModelErrorHTTP, StatusCode: 502, Message: "Bad Gateway"}, ResumeAvailable: true})

	if !tui.chat.ResumeAvailable {
		t.Fatal("ResumeAvailable = false, want true")
	}
	if got := len(tui.chat.Messages); got != 1 {
		t.Fatalf("messages = %d, want 1", got)
	}
	if got := tui.chat.Messages[0].Role; got != "error" {
		t.Fatalf("message role = %q, want error", got)
	}
	content, _ := tui.chat.Messages[0].Content.(string)
	if !strings.Contains(content, "模型请求失败：HTTP 502") || !strings.Contains(content, "按 Enter") {
		t.Fatalf("message content = %v, want structured retryable model error", content)
	}
}

func TestSessionModelUnavailableErrorUsesLocalizedRecoveryGuidance(t *testing.T) {
	for _, tt := range []struct {
		name   string
		locale LocaleID
		wants  []string
	}{
		{name: "Chinese", locale: LocaleZH, wants: []string{"当前会话使用的模型「openai/gpt-5.6」已不存在", "请使用 /model"}},
		{name: "English", locale: LocaleEN, wants: []string{"openai/gpt-5.6", "Use /model"}},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			tui := &TUI{i18n: newTranslator(tt.locale), width: 80, height: 18}
			tui.initChatComponents()
			tui.handleAgentRunNotification(protocol.AgentRunParams{
				State:    protocol.AgentRunFailed,
				RunError: &protocol.RunError{Kind: protocol.RunErrorSessionModelUnavailable, ModelRef: "openai/gpt-5.6"},
			})
			if got := len(tui.chat.Messages); got != 1 {
				t.Fatalf("messages = %d, want 1", got)
			}
			content, _ := tui.chat.Messages[0].Content.(string)
			for _, want := range tt.wants {
				if !strings.Contains(content, want) {
					t.Fatalf("message content = %q, want %q", content, want)
				}
			}
		})
	}
}

func TestRestoreStatusShowsCompactedNoticeAtEnd(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 18}
	tui.initChatComponents()
	tui.applySessionSnapshot(protocol.SessionSnapshot{Messages: []protocol.SnapshotMessage{{Role: "user", Content: "之前的问题"}}, Compacted: true})

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

func TestRestoreStatusShowsToolSummaryWithI18N(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 80, height: 18}
	tui.initChatComponents()

	tui.applySessionSnapshot(protocol.SessionSnapshot{ToolSummary: &protocol.ToolSummaryPayload{
		Total:    3,
		Success:  2,
		Failed:   1,
		Failures: []protocol.ToolSummaryItem{{Tool: "exec", Summary: "go test failed"}},
		Recent:   []protocol.ToolSummaryItem{{Tool: "readfile"}, {Tool: "editfile"}, {Tool: "exec"}},
		Changes:  []protocol.ToolChangeItem{{Tool: "editfile", Count: 1}},
	}})

	if got := len(tui.chat.Messages); got != 1 {
		t.Fatalf("messages = %d, want 1", got)
	}
	msg := tui.chat.Messages[0]
	if msg.Role != "restore_summary" {
		t.Fatalf("role = %q, want restore_summary", msg.Role)
	}
	content, _ := msg.Content.(string)
	for _, want := range []string{"Previous tool activity", "3 calls", "Failures: exec", "Recent: readfile"} {
		if !strings.Contains(content, want) {
			t.Fatalf("summary = %q, want %q", content, want)
		}
	}
}
