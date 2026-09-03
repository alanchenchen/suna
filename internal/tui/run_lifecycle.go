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
	// 其它会话的 run lifecycle 不能影响本页 Composer 状态（attach 在途窗口期可能串扰）。
	if p.SessionID != "" && t.currentSession.ID != "" && p.SessionID != t.currentSession.ID {
		return
	}
	if p.RunID != "" && p.RunID == t.completedRunID {
		return
	}
	if isTerminalAgentRunState(p.State) && p.RunID != "" && t.activeRunID != "" && p.RunID != t.activeRunID {
		// 旧 run 的迟到终态不能清理或终止当前 run 的 Composer 状态。
		return
	}
	if p.State == protocol.AgentRunRunning {
		if p.RunID != "" && p.RunID != t.activeRunID {
			t.chat.PendingSteering = nil
			t.restoreUnresolvedSteeringSubmissions()
			t.chat.SteeringTerminal = nil
		}
		t.startRunElapsed(p.RunID, time.Now())
		if p.RunID != "" {
			t.completedRunID = ""
		}
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
		if p.RunID != "" {
			t.completedRunID = p.RunID
		}
		t.chat.PendingSteering = nil
		t.restoreUnresolvedSteeringSubmissions()
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
		if p.RunID != "" {
			t.completedRunID = p.RunID
		}
		t.chat.PendingSteering = nil
		t.restoreUnresolvedSteeringSubmissions()
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
		// run 成功完成后短暂显示开心眼奖励帧（约 1.6 秒），再回到 idle。
		t.petHappyUntil = time.Now().Add(petHappyDuration)
		t.resetPhase()
		// worked for 消息追加后总行数变化；用户未手动滚动时强制跟随到底部，
		// 否则视图停在回复末尾、worked for 行不可见，且 Home 跳转长回复顶部失效。
		if !t.chat.ManualScrollPaused {
			t.forceScrollToBottomOnNextSync()
		}
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

func isTerminalAgentRunState(state protocol.AgentRunState) bool {
	switch state {
	case protocol.AgentRunDone, protocol.AgentRunFailed, protocol.AgentRunCancelled:
		return true
	default:
		return false
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
		t.appendNonToolMessage(chatMsg{Role: "run_duration", Content: toolview.FormatCompactDuration(duration), EndedAt: time.Now()})
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
