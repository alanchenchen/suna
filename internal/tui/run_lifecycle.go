package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/tui/components/toolview"
)

func (t *TUI) handleAgentRunNotification(p protocol.AgentRunParams) {
	if p.RunID != "" && p.RunID == t.completedRunID {
		return
	}
	if p.State == protocol.AgentRunRunning {
		t.startRunElapsed(p.RunID, time.Now())
	}
	if p.State == protocol.AgentRunRunning && p.RunID != "" {
		if t.completedRunID != "" {
			t.resetTranscriptAutoResumeCycle()
		}
		t.completedRunID = ""
	}
	if p.State == protocol.AgentRunRunning && !t.cancelling {
		t.currentRunCanControl = p.CanControl
	}
	switch p.State {
	case protocol.AgentRunCancelling:
		t.enterCancelling()
	case protocol.AgentRunRetrying:
		t.setRunRetryStatus(p, time.Now())
	case protocol.AgentRunFailed, protocol.AgentRunCancelled:
		t.resetTranscriptAutoResumeCycle()
		if p.RunID != "" {
			t.completedRunID = p.RunID
		}
		t.clearRunRetryStatus()
		t.currentRunCanControl = false
		t.chat.ClearRunInteractions()
		t.finishStreamingMessages()
		if p.State == protocol.AgentRunCancelled {
			t.chat.FinishCancellingTools(time.Now())
			t.cancelling = false
			t.chatSpinnerTicking = false
			if t.shouldAppendCancelNotice(p.RunID) {
				t.appendNonToolMessage(chatMsg{Role: "error", Content: t.tr("model_error.cancelled")})
			}
		} else {
			if t.cancelling {
				t.chat.FinishCancellingTools(time.Now())
				t.chatSpinnerTicking = false
			}
			t.cancelling = false
			t.appendNonToolMessage(chatMsg{Role: "error", Content: t.formatModelError(p)})
		}
		t.chat.ResumeAvailable = p.ResumeAvailable
		t.appendRunElapsed(p.RunID)
		t.resetPhase()
	case protocol.AgentRunDone:
		t.resetTranscriptAutoResumeCycle()
		if p.RunID != "" {
			t.completedRunID = p.RunID
		}
		if t.cancelling {
			t.chat.FinishCancellingTools(time.Now())
			t.chatSpinnerTicking = false
		}
		t.clearRunRetryStatus()
		t.currentRunCanControl = false
		t.chat.ClearRunInteractions()
		t.cancelling = false
		t.finishStreamingMessages()
		t.chat.ResumeAvailable = false
		t.appendRunElapsed(p.RunID)
		t.resetPhase()
	case protocol.AgentRunRunning:
		if t.cancelling {
			return
		}
		t.clearRunRetryStatus()
		if p.Phase == protocol.AgentRunPhaseModel {
			t.startLLMWait()
			t.chat.SetStatusLabel(t.tr("status.waiting_model"), time.Now())
		}
	}
}

func (t *TUI) startRunElapsed(runID string, now time.Time) {
	if runID != "" && t.activeRunID != "" && runID != t.activeRunID {
		t.runStartedAt = now
		t.runHadToolCall = false
	}
	if t.runStartedAt.IsZero() {
		t.runStartedAt = now
	}
	if runID != "" {
		t.activeRunID = runID
	}
}

func (t *TUI) appendRunElapsed(runID string) {
	if t.runStartedAt.IsZero() {
		return
	}
	if runID != "" && t.activeRunID != "" && runID != t.activeRunID {
		return
	}
	duration := time.Since(t.runStartedAt)
	if t.runHadToolCall {
		t.appendNonToolMessage(chatMsg{Role: "run_duration", Content: toolview.FormatCompactDuration(duration)})
	}
	t.runStartedAt = time.Time{}
	t.activeRunID = ""
	t.runHadToolCall = false
}

func (t *TUI) runElapsedLabel() string {
	if t.runStartedAt.IsZero() {
		return ""
	}
	return toolview.FormatCompactDuration(time.Since(t.runStartedAt))
}

func (t *TUI) setRunRetryStatus(p protocol.AgentRunParams, now time.Time) {
	t.retryAttempt = p.Attempt
	t.retryMaxAttempts = p.MaxAttempts
	if p.DelayMs > 0 {
		t.retryDeadline = now.Add(time.Duration(p.DelayMs) * time.Millisecond)
	} else {
		t.retryDeadline = time.Time{}
	}
	t.chat.SetStatusLabel(t.formatRunRetryStatus(now), now)
}

func (t *TUI) clearRunRetryStatus() {
	t.retryDeadline = time.Time{}
	t.retryAttempt = 0
	t.retryMaxAttempts = 0
}

func (t *TUI) refreshRunRetryStatus(now time.Time) {
	if !t.retryDeadline.IsZero() {
		t.chat.StatusLabel = t.formatRunRetryStatus(now)
	}
}

func (t *TUI) formatRunRetryStatus(now time.Time) string {
	if !t.retryDeadline.IsZero() && t.retryAttempt > 0 && t.retryMaxAttempts > 0 {
		remaining := max(0, int(math.Ceil(t.retryDeadline.Sub(now).Seconds())))
		return t.i18n.Tf("status.model_retrying", remaining, t.retryAttempt, t.retryMaxAttempts)
	}
	return t.tr("status.model_retrying_simple")
}

func (t *TUI) formatModelError(p protocol.AgentRunParams) string {
	if p.RunError != nil {
		return t.formatRunError(*p.RunError)
	}
	summary := t.tr("model_error.unknown")
	detail := strings.TrimSpace(p.Message)
	if p.Error != nil {
		summary = t.modelErrorSummary(*p.Error)
		detail = strings.TrimSpace(p.Error.Message)
	}
	lines := []string{t.i18n.Tf("model_error.failed", summary)}
	if detail != "" && detail != summary && !strings.Contains(summary, detail) {
		lines = append(lines, t.i18n.Tf("model_error.detail", detail))
	}
	if p.ResumeAvailable {
		lines = append(lines, t.tr("model_error.action_retry"))
	} else {
		lines = append(lines, t.tr("model_error.action_check"))
	}
	return strings.Join(lines, "\n")
}

func (t *TUI) formatRunError(err protocol.RunError) string {
	switch err.Kind {
	case protocol.RunErrorNoModelConfigured:
		return t.tr("model_error.no_model_configured")
	case protocol.RunErrorSessionModelUnavailable:
		return t.i18n.Tf("model_error.session_model_unavailable", err.ModelRef)
	default:
		return t.tr("model_error.unknown")
	}
}

func (t *TUI) modelErrorSummary(err protocol.ModelError) string {
	switch err.Kind {
	case protocol.ModelErrorHTTP:
		if err.StatusCode > 0 {
			label := strings.TrimSpace(firstNonEmpty(err.Code, err.Type, err.Message))
			if label == "" {
				return fmt.Sprintf("HTTP %d", err.StatusCode)
			}
			return fmt.Sprintf("HTTP %d · %s", err.StatusCode, label)
		}
	case protocol.ModelErrorNetwork:
		if strings.TrimSpace(err.Message) != "" {
			return t.i18n.Tf("model_error.network_with_message", err.Message)
		}
		return t.tr("model_error.network")
	case protocol.ModelErrorCancelled:
		return t.tr("model_error.cancelled")
	case protocol.ModelErrorInternal:
		if strings.TrimSpace(err.Message) != "" {
			return err.Message
		}
		return t.tr("model_error.internal")
	}
	return firstNonEmpty(err.Message, t.tr("model_error.unknown"))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
