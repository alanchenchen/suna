package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/protocol"
)

func TestRunElapsedAppearsInInputAndFreezesInTranscript(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24, currentRunCanControl: true}
	tui.initChatComponents()
	tui.handleAgentRunNotification(protocol.AgentRunParams{RunID: "run-1", State: protocol.AgentRunRunning, Phase: protocol.AgentRunPhaseModel})
	tui.runStartedAt = time.Now().Add(-61 * time.Second)

	input := stripANSIForTest(tui.renderInputArea())
	if !strings.Contains(input, strings.TrimSpace(stripANSIForTest(tui.chat.Spinner.View()))) {
		t.Fatalf("renderInputArea() = %q, want spinner in editable run status", input)
	}
	if !strings.Contains(input, "正在等待模型") || !strings.Contains(input, "1m1s") {
		t.Fatalf("renderInputArea() = %q, want running status with total elapsed", input)
	}
	if !lineContainsAll(input, "正在等待模型", "1m1s", "Esc 取消") {
		t.Fatalf("renderInputArea() = %q, want elapsed and cancel help on one line", input)
	}
	tui.handleToolStartNotification(protocol.ToolStartParams{ID: "tool-1", Tool: "readfile"})

	tui.handleAgentRunNotification(protocol.AgentRunParams{RunID: "run-1", State: protocol.AgentRunDone})
	if !tui.runStartedAt.IsZero() || tui.activeRunID != "" {
		t.Fatalf("run timer = (%v, %q), want cleared", tui.runStartedAt, tui.activeRunID)
	}
	if got := len(tui.chat.Messages); got != 2 {
		t.Fatalf("messages = %d, want tool block and duration message", got)
	}
	tui.syncContent()
	view := stripANSIForTest(tui.chat.Viewport.GetContent())
	if !strings.Contains(view, "✦ 已工作 1m1s") {
		t.Fatalf("transcript = %q, want frozen worked duration", view)
	}
}

func TestRunElapsedPureTextRunDoesNotAppendFooter(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH)}
	tui.initChatComponents()
	tui.handleAgentRunNotification(protocol.AgentRunParams{RunID: "run-1", State: protocol.AgentRunRunning})
	tui.runStartedAt = time.Now().Add(-2 * time.Second)

	tui.handleAgentRunNotification(protocol.AgentRunParams{RunID: "run-1", State: protocol.AgentRunDone})

	if len(tui.chat.Messages) != 0 {
		t.Fatalf("messages = %d, want no footer for a run without tool calls", len(tui.chat.Messages))
	}
	if !tui.runStartedAt.IsZero() || tui.activeRunID != "" || tui.runHadToolCall {
		t.Fatalf("run state = (%v, %q, %v), want cleared", tui.runStartedAt, tui.activeRunID, tui.runHadToolCall)
	}
}

func TestRunElapsedDoesNotAppendForUnknownTerminalRun(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH)}
	tui.initChatComponents()
	tui.startRunElapsed("run-1", time.Now().Add(-time.Second))

	tui.handleAgentRunNotification(protocol.AgentRunParams{RunID: "run-2", State: protocol.AgentRunDone})

	if len(tui.chat.Messages) != 0 {
		t.Fatalf("messages = %d, want no duration for a different run", len(tui.chat.Messages))
	}
	if tui.activeRunID != "run-1" {
		t.Fatalf("activeRunID = %q, want run-1", tui.activeRunID)
	}
}

func TestCurrentStatusLabelOmitsRunningToolCount(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH)}
	tui.initChatComponents()
	tui.chat.StartTool(protocol.ToolStartParams{ID: "tool-1", Tool: "readfile"}, "tool-1", time.Now())
	tui.runStartedAt = time.Now().Add(-2 * time.Second)

	got := tui.currentInputStatusLabel()
	if !strings.Contains(got, "执行工具中") || !strings.Contains(got, "2.0s") {
		t.Fatalf("currentInputStatusLabel() = %q, want tool status with elapsed", got)
	}
	if strings.Contains(got, "running") {
		t.Fatalf("currentInputStatusLabel() = %q, should not include running count", got)
	}
}

func TestInlineRunStatusUsesCompactFallbackAtNarrowWidth(t *testing.T) {
	got := stripANSIForTest(renderInlineRunStatus(24, "正在自动重试 · 2m1s", "Esc 取消"))
	if got != "正在自动重试 · 2m1s · Esc 取消" {
		t.Fatalf("renderInlineRunStatus() = %q, want compact fallback", got)
	}
}

func lineContainsAll(text string, values ...string) bool {
	for _, line := range strings.Split(text, "\n") {
		matched := true
		for _, value := range values {
			if !strings.Contains(line, value) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}
