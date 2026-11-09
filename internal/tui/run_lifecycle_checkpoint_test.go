package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/tui/components/toolview"
)

func TestFailedRunClosesToolsWithoutToolEndAsUnknown(t *testing.T) {
	for _, cancelling := range []bool{false, true} {
		name := "ordinary failure"
		if cancelling {
			name = "failure after local cancelling"
		}
		t.Run(name, func(t *testing.T) {
			tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 18}
			tui.initChatComponents()
			tui.currentSession = protocol.SessionInfo{ID: "session-1"}
			started := time.Now().Add(-time.Second)
			known := tui.chat.StartTool(protocol.ToolStartParams{Tool: "exec"}, "known", started)
			tui.chat.EndTool(protocol.ToolEndParams{Result: "known saved result"}, "known", started.Add(time.Millisecond))
			failed := tui.chat.StartTool(protocol.ToolStartParams{Tool: "exec"}, "failed", started)
			tui.chat.EndTool(protocol.ToolEndParams{Error: true, Result: "known tool failure"}, "failed", started.Add(time.Millisecond))
			missing := tui.chat.StartTool(protocol.ToolStartParams{Tool: "exec"}, "missing-end", started)
			tui.activeRunID = "run-1"
			tui.chatSpinnerTicking = true
			tui.cancelling = cancelling
			if cancelling {
				tui.chat.MarkActiveToolsCancelling()
			}

			// 模拟 result 检查点失败：有 tool.start，但没有可确认结果的 tool.end。
			failure := protocol.AgentRunParams{
				SessionID: "session-1",
				RunID:     "run-1",
				State:     protocol.AgentRunFailed,
				Error:     &protocol.ModelError{Kind: protocol.ModelErrorInternal, Message: "checkpoint save failed"},
			}
			tui.handleAgentRunNotification(failure)

			if missing.Status != toolview.StatusUnknown || missing.EndedAt.IsZero() || missing.Duration <= 0 {
				t.Fatalf("unfinished tool was not closed conservatively: %#v", missing)
			}
			if missing.Result != "" || missing.Metadata != nil || missing.ResultBytes != 0 {
				t.Fatalf("missing tool.end gained fabricated result facts: %#v", missing)
			}
			if known.Status != toolview.StatusDone || known.Result != "known saved result" || failed.Status != toolview.StatusError || failed.Result != "known tool failure" {
				t.Fatalf("confirmed terminal facts changed: known=%#v, failed=%#v", known, failed)
			}
			if len(tui.chat.ActiveTools) != 0 || len(tui.chat.ToolStartTimes) != 0 || tui.chat.Loading || tui.chatSpinnerTicking || tui.cancelling {
				t.Fatalf("failed run retained active UI state: active=%d timers=%d loading=%v spinner=%v cancelling=%v", len(tui.chat.ActiveTools), len(tui.chat.ToolStartTimes), tui.chat.Loading, tui.chatSpinnerTicking, tui.cancelling)
			}
			errorCount := 0
			for _, msg := range tui.chat.Messages {
				if msg.Role != "error" {
					continue
				}
				errorCount++
				content, _ := msg.Content.(string)
				if !strings.Contains(content, "checkpoint save failed") || strings.Contains(content, "已取消") {
					t.Fatalf("main error = %q, want checkpoint explanation without cancellation claim", content)
				}
			}
			if errorCount != 1 {
				t.Fatalf("error count = %d, want 1", errorCount)
			}
			count := len(tui.chat.Messages)
			tui.handleAgentRunNotification(failure)
			if len(tui.chat.Messages) != count {
				t.Fatal("duplicate failed lifecycle appended another error")
			}
		})
	}
}

func TestHardCompactFailureUsesMainRunError(t *testing.T) {
	ui := &TUI{i18n: newTranslator(LocaleZH)}
	ui.initChatComponents()
	ui.currentSession = protocol.SessionInfo{ID: "session-1"}
	ui.activeRunID = "run-1"
	ui.runStartedAt = time.Now().Add(-time.Second)
	ui.chat.Loading, ui.chat.Compacting = true, true
	ui.compactState = protocol.CompactState{CompactID: "compact-1", State: "preparing", Wait: "hard_limit"}
	failure := protocol.AgentRunParams{
		SessionID: "session-1", RunID: "run-1", State: protocol.AgentRunFailed, Phase: protocol.AgentRunPhaseCompact,
		Error: &protocol.ModelError{Kind: protocol.ModelErrorInternal, Message: "compression stream idle timeout"},
	}
	ui.handleAgentRunNotification(failure)
	if ui.chat.Loading || ui.chat.Compacting || len(ui.chat.Messages) != 1 || ui.chat.Messages[0].Role != "error" {
		t.Fatalf("hard failure loading/compacting/messages = %v/%v/%#v", ui.chat.Loading, ui.chat.Compacting, ui.chat.Messages)
	}
	if got, _ := ui.chat.Messages[0].Content.(string); !strings.Contains(got, "compression stream idle timeout") {
		t.Fatalf("got error = %q, want original hard failure", got)
	}
	ui.handleAgentRunNotification(failure)
	if len(ui.chat.Messages) != 1 {
		t.Fatal("duplicate hard failure appended another error")
	}
}

func TestManualCompactLifecycleKeepsResultInCompactNotification(t *testing.T) {
	for _, localPending := range []bool{false, true} {
		t.Run(fmt.Sprintf("localPending=%v", localPending), func(t *testing.T) {
			ui := &TUI{i18n: newTranslator(LocaleZH)}
			ui.initChatComponents()
			ui.currentSession = protocol.SessionInfo{ID: "session-1"}
			ui.activeRunID = "run-1"
			ui.chat.Loading, ui.chat.Compacting = true, true
			ui.manualCompactPending = localPending
			if !localPending {
				ui.compactState = protocol.CompactState{CompactID: "compact-1", State: "preparing", Wait: "manual"}
			}
			ui.handleAgentRunNotification(protocol.AgentRunParams{
				SessionID: "session-1", RunID: "run-1", State: protocol.AgentRunCancelled, Phase: protocol.AgentRunPhaseCompact,
			})
			if ui.chat.Loading || ui.chat.Compacting || len(ui.chat.Messages) != 0 || ui.manualCompactPending != localPending {
				t.Fatalf("manual lifecycle loading/compacting/messages/pending = %v/%v/%#v/%v", ui.chat.Loading, ui.chat.Compacting, ui.chat.Messages, ui.manualCompactPending)
			}
		})
	}
}
