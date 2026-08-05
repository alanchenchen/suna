package agent

import (
	"fmt"
	"strings"
	"sync"

	"github.com/alanchenchen/suna/internal/guard"
	"github.com/alanchenchen/suna/internal/runner"
)

const (
	guardTaskMaxReceipts        = 12
	guardReviewTaskLimit        = 700
	guardReviewLatestInputLimit = 320
	guardReviewReceiptLimit     = 760
	guardReviewMaxReceipts      = 2
)

// guardTaskCard 仅保存当前 session agent 内正在执行任务的用户意图事实。
// 它不是授权缓存；每个中高风险调用仍必须经过 Smart Review。
type guardTaskCard struct {
	mu          sync.RWMutex
	task        string
	latestInput string
	receipts    []guardTaskReceipt
}

// guardTaskSnapshot 是上一次中断或完成任务的只读证据，供下一次 review 判断是否存在连续性。
// 代码不根据输入文本推断续作；旧任务不会自动提供授权。
type guardTaskSnapshot struct {
	task        string
	latestInput string
	receipts    []guardTaskReceipt
}

type guardTaskReceipt struct {
	tool      string
	risk      string
	target    string
	summary   string
	rationale string
	approved  bool
}

func (a *Agent) beginGuardTask(input string) {
	input = trimForGuardMiddle(input, guardReviewTaskLimit)
	if input == "" {
		return
	}
	a.guardTaskMu.Lock()
	defer a.guardTaskMu.Unlock()
	if a.guardTask == nil {
		a.guardTask = &guardTaskCard{}
	}

	a.guardTask.mu.Lock()
	if a.guardTask.task != "" {
		a.priorGuardTask = snapshotGuardTaskLocked(a.guardTask)
	}
	// 每个新用户输入创建新的活动任务。旧任务只作为 review 的背景事实，
	// 是否续作由 LLM 根据当前 action 和用户输入判断，不能由字符串匹配决定。
	a.guardTask.task = input
	a.guardTask.latestInput = trimForGuardMiddle(input, guardReviewLatestInputLimit)
	a.guardTask.receipts = nil
	a.guardTask.mu.Unlock()
}

func (a *Agent) recordGuardTaskReceipt(call runner.ToolExecution, result *guard.GuardResult, approved bool) {
	if result == nil {
		return
	}
	a.guardTaskMu.Lock()
	defer a.guardTaskMu.Unlock()
	if a.guardTask == nil {
		return
	}
	receipt := guardTaskReceipt{
		tool:      strings.TrimSpace(call.Name),
		risk:      guard.RiskString(result.Risk),
		target:    trimForGuardMiddle(guard.SafeTarget(call.Name, call.Params), guardReviewReceiptLimit),
		summary:   trimForGuardMiddle(guard.SafeOperationSummary(call.Name, call.Params), guardReviewReceiptLimit),
		rationale: trimForGuardMiddle(firstNonEmpty(call.Intent, call.AssistantContext), guardReviewReceiptLimit),
		approved:  approved,
	}
	a.guardTask.mu.Lock()
	defer a.guardTask.mu.Unlock()
	if a.guardTask.task == "" {
		return
	}
	a.guardTask.receipts = append(a.guardTask.receipts, receipt)
	if len(a.guardTask.receipts) > guardTaskMaxReceipts {
		a.guardTask.receipts = append([]guardTaskReceipt(nil), a.guardTask.receipts[len(a.guardTask.receipts)-guardTaskMaxReceipts:]...)
	}
}

func (a *Agent) guardTaskReviewContext() (task, latestInput, receipts, priorTask string) {
	a.guardTaskMu.Lock()
	defer a.guardTaskMu.Unlock()
	if a.guardTask == nil {
		return "", "", "", formatPriorGuardTask(a.priorGuardTask)
	}
	a.guardTask.mu.RLock()
	task = a.guardTask.task
	latestInput = a.guardTask.latestInput
	receipts = formatGuardTaskReceipts(latestGuardTaskReceipts(a.guardTask.receipts))
	a.guardTask.mu.RUnlock()
	return task, latestInput, receipts, formatPriorGuardTask(a.priorGuardTask)
}

func snapshotGuardTaskLocked(card *guardTaskCard) *guardTaskSnapshot {
	if card == nil || card.task == "" {
		return nil
	}
	return &guardTaskSnapshot{
		task:        card.task,
		latestInput: card.latestInput,
		receipts:    append([]guardTaskReceipt(nil), card.receipts...),
	}
}

func latestGuardTaskReceipts(all []guardTaskReceipt) []guardTaskReceipt {
	if len(all) <= guardReviewMaxReceipts {
		return append([]guardTaskReceipt(nil), all...)
	}
	return append([]guardTaskReceipt(nil), all[len(all)-guardReviewMaxReceipts:]...)
}

func formatGuardTaskReceipts(receipts []guardTaskReceipt) string {
	if len(receipts) == 0 {
		return ""
	}
	lines := make([]string, 0, len(receipts))
	for _, receipt := range receipts {
		outcome := "approved"
		if !receipt.approved {
			outcome = "rejected"
		}
		line := fmt.Sprintf("- The user %s a %s risk %s action", outcome, receipt.risk, receipt.tool)
		if receipt.target != "" {
			line += "; target: " + receipt.target
		}
		if receipt.summary != "" {
			line += "; scope: " + receipt.summary
		}
		if receipt.rationale != "" {
			line += "; agent rationale: " + receipt.rationale
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func formatPriorGuardTask(snapshot *guardTaskSnapshot) string {
	if snapshot == nil || snapshot.task == "" {
		return ""
	}
	receipts := formatGuardTaskReceipts(latestGuardTaskReceipts(snapshot.receipts))
	if receipts == "" {
		return "Previous task (background only, not authorization): " + snapshot.task
	}
	return "Previous task (background only, not authorization): " + snapshot.task + "\nPrior user decisions:\n" + receipts
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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
