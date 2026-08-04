package agent

import (
	"strings"
	"sync"

	"github.com/alanchenchen/suna/internal/guard"
)

const (
	guardTaskMaxDecisions          = 12
	guardReviewTaskLimit           = 700
	guardReviewLatestInputLimit    = 320
	guardReviewDecisionReasonLimit = 180
	guardReviewMaxDecisions        = 2
	guardReviewParamsLimit         = 1800
)

// guardTaskCard 只在当前 session agent 的内存中保存未完成任务的用户意图事实。
// 它不参与自动授权，所有中高风险调用仍会逐次经过 Smart Review。
type guardTaskCard struct {
	mu          sync.RWMutex
	task        string
	latestInput string
	decisions   []guardTaskDecision
}

type guardTaskDecision struct {
	tool     string
	risk     string
	approved bool
	reason   string
}

func (a *Agent) beginGuardTask(input string) {
	input = trimForGuardMiddle(input, guardReviewTaskLimit)
	if input == "" {
		return
	}
	if a.guardTask == nil {
		a.guardTask = &guardTaskCard{}
	}

	a.guardTask.mu.Lock()
	defer a.guardTask.mu.Unlock()
	// 每个新的用户输入都开始新的任务卡；不通过文本猜测它是否延续旧任务。
	// ResumeRun 不调用这里，因此会精确延续同一次未完成 Agent run。
	a.guardTask.task = input
	a.guardTask.latestInput = trimForGuardMiddle(input, guardReviewLatestInputLimit)
	a.guardTask.decisions = nil
}

func (a *Agent) recordGuardTaskDecision(tool string, result *guard.GuardResult, approved bool) {
	if a.guardTask == nil || result == nil {
		return
	}
	decision := guardTaskDecision{
		tool:     strings.TrimSpace(tool),
		risk:     guard.RiskString(result.Risk),
		approved: approved,
		reason:   trimForGuardMiddle(result.Reason, guardReviewDecisionReasonLimit),
	}
	a.guardTask.mu.Lock()
	defer a.guardTask.mu.Unlock()
	if a.guardTask.task == "" {
		return
	}
	a.guardTask.decisions = append(a.guardTask.decisions, decision)
	if len(a.guardTask.decisions) > guardTaskMaxDecisions {
		a.guardTask.decisions = append([]guardTaskDecision(nil), a.guardTask.decisions[len(a.guardTask.decisions)-guardTaskMaxDecisions:]...)
	}
}

func (a *Agent) guardTaskReviewContext() (task, latestInput, decisions string) {
	if a.guardTask == nil {
		return "", "", ""
	}
	a.guardTask.mu.RLock()
	defer a.guardTask.mu.RUnlock()
	task = a.guardTask.task
	latestInput = a.guardTask.latestInput
	selected := latestGuardTaskDecisions(a.guardTask.decisions)
	if len(selected) == 0 {
		return task, latestInput, ""
	}
	lines := make([]string, 0, len(selected))
	for _, decision := range selected {
		outcome := "approved"
		if !decision.approved {
			outcome = "rejected"
		}
		line := "- The user " + outcome + " a " + decision.risk + " risk " + decision.tool + " action"
		if decision.reason != "" {
			line += ": " + decision.reason
		}
		lines = append(lines, line)
	}
	return task, latestInput, strings.Join(lines, "\n")
}

func latestGuardTaskDecisions(all []guardTaskDecision) []guardTaskDecision {
	if len(all) <= guardReviewMaxDecisions {
		return append([]guardTaskDecision(nil), all...)
	}
	return append([]guardTaskDecision(nil), all[len(all)-guardReviewMaxDecisions:]...)
}

func trimForGuardMiddle(s string, max int) string {
	s = strings.TrimSpace(guard.MaskSensitiveContent(s))
	chars := []rune(s)
	if max <= 0 || len(chars) <= max {
		return s
	}
	if max <= 32 {
		return string(chars[:max])
	}
	first := (max - 18) * 2 / 3
	last := max - 18 - first
	return string(chars[:first]) + "...[omitted]..." + string(chars[len(chars)-last:])
}
