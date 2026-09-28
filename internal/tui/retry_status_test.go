package tui

import (
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/protocol"
)

func TestRunRetryStatusCountsDown(t *testing.T) {
	tui := New(LocaleEN)
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	tui.setRunRetryStatus(protocol.AgentRunParams{Attempt: 2, DelayMs: 8_000, ElapsedMs: 24_000, BudgetMs: 120_000}, now)

	if got, want := tui.formatRunRetryStatus(now), "Model service temporarily unavailable · retry 2 (in 8s, 24s / 2m0s)"; got != want {
		t.Fatalf("formatRunRetryStatus() = %q, want %q", got, want)
	}
	if got, want := tui.formatRunRetryStatus(now.Add(3*time.Second)), "Model service temporarily unavailable · retry 2 (in 5s, 27s / 2m0s)"; got != want {
		t.Fatalf("formatRunRetryStatus() after 3s = %q, want %q", got, want)
	}
	if got, want := tui.formatRunRetryStatus(now.Add(9*time.Second)), "Model service temporarily unavailable · retry 2 (in 0s, 33s / 2m0s)"; got != want {
		t.Fatalf("formatRunRetryStatus() after deadline = %q, want %q", got, want)
	}
}

func TestRunRetryStatusClearsWhenRunContinues(t *testing.T) {
	tui := New(LocaleEN)
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	tui.setRunRetryStatus(protocol.AgentRunParams{Attempt: 2, DelayMs: 8_000, ElapsedMs: 24_000, BudgetMs: 120_000}, now)
	tui.handleAgentRunNotification(protocol.AgentRunParams{State: protocol.AgentRunRunning, Phase: protocol.AgentRunPhaseModel})

	if !tui.retryDeadline.IsZero() || tui.retryAttempt != 0 || tui.retryElapsed != 0 || tui.retryBudget != 0 {
		t.Fatalf("retry state = (%v, %d, %s, %s), want cleared", tui.retryDeadline, tui.retryAttempt, tui.retryElapsed, tui.retryBudget)
	}
}
