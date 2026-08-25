package agent

import (
	"fmt"
	"strings"
	"sync"

	"github.com/alanchenchen/suna/internal/guard"
	"github.com/alanchenchen/suna/internal/runner"
)

const (
	guardTaskMaxReceipts    = 12
	guardReviewReceiptLimit = 240
	guardReviewMaxReceipts  = 2
)

// guardTaskCard 只保存最近风险确认结果；它不是授权缓存。
type guardTaskCard struct {
	mu       sync.RWMutex
	receipts []guardTaskReceipt
}

type guardTaskReceipt struct {
	tool     string
	risk     string
	target   string
	approved bool
}

func (a *Agent) recordGuardTaskReceipt(call runner.ToolExecution, result *guard.GuardResult, approved bool) {
	if result == nil {
		return
	}
	a.guardTaskMu.Lock()
	defer a.guardTaskMu.Unlock()
	if a.guardTask == nil {
		a.guardTask = &guardTaskCard{}
	}
	receipt := guardTaskReceipt{
		tool:     strings.TrimSpace(call.Name),
		risk:     guard.RiskString(result.Risk),
		target:   trimForGuardMiddle(guard.SafeTarget(call.Name, call.Params), guardReviewReceiptLimit),
		approved: approved,
	}
	a.guardTask.mu.Lock()
	defer a.guardTask.mu.Unlock()
	a.guardTask.receipts = append(a.guardTask.receipts, receipt)
	if len(a.guardTask.receipts) > guardTaskMaxReceipts {
		a.guardTask.receipts = append([]guardTaskReceipt(nil), a.guardTask.receipts[len(a.guardTask.receipts)-guardTaskMaxReceipts:]...)
	}
}

func (a *Agent) recentGuardRiskDecisions() []string {
	a.guardTaskMu.Lock()
	defer a.guardTaskMu.Unlock()
	if a.guardTask == nil {
		return nil
	}
	a.guardTask.mu.RLock()
	receipts := append([]guardTaskReceipt(nil), a.guardTask.receipts...)
	a.guardTask.mu.RUnlock()
	if len(receipts) > guardReviewMaxReceipts {
		receipts = receipts[len(receipts)-guardReviewMaxReceipts:]
	}
	out := make([]string, 0, len(receipts))
	for _, receipt := range receipts {
		out = append(out, formatGuardRiskDecision(receipt))
	}
	return out
}

func formatGuardRiskDecision(receipt guardTaskReceipt) string {
	outcome := "Approved"
	if !receipt.approved {
		outcome = "Rejected"
	}
	line := fmt.Sprintf("%s: tool=%s; risk=%s", outcome, receipt.tool, receipt.risk)
	if receipt.target != "" {
		line += "; target=" + receipt.target
	}
	return line
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
