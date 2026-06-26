package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/alanchenchen/suna/internal/logging"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
)

const minContextMarginTokens = 2048

func (r *Runner) Compact(ctx context.Context, working *memory.WorkingMemory, sessionState string, contextWindow, outputBudget int) (before, after, turnsCompressed, truncated int, newSessionState string, err error) {
	if r.Compressor == nil || working == nil {
		return 0, 0, 0, 0, "", fmt.Errorf("compressor not initialized")
	}
	msgs := working.Messages()
	before = working.EstimatedTokens()
	if len(msgs) <= 1 {
		return before, before, 0, 0, sessionState, nil
	}
	recentBudget := manualCompactRecentBudget(contextWindow, outputBudget)
	compressed, state, folded, compErr := r.Compressor.CompressHistoryWithStateBudget(ctx, msgs, sessionState, contextWindow, recentBudget)
	if compErr != nil {
		return 0, 0, 0, 0, "", compErr
	}
	if state == "" && folded == 0 {
		return before, before, 0, 0, sessionState, nil
	}
	turnsCompressed = folded
	working.SetMessages(compressed)
	after = working.EstimatedTokens()
	truncated = countLargeToolOutputs(msgs)
	return before, after, turnsCompressed, truncated, state, nil
}

func (r *Runner) compactForRequest(ctx context.Context, working *memory.WorkingMemory, req *model.CompletionRequest, contextWindow int, sessionState string, coef float64, calibrated bool) (string, error) {
	if r.Compressor == nil || working == nil {
		return sessionState, nil
	}
	if !shouldCompactRequest(req, contextWindow, coef, calibrated) {
		return sessionState, nil
	}
	started := time.Now()
	msgs := working.Messages()
	before := working.EstimatedTokens()
	requestTokens := estimateRequestTokens(req, coef)
	logging.Info("memory", "session_compact_start", logging.Event{"mode": "auto", "purpose": req.Purpose, "model": req.Model, "context_window": contextWindow, "before_tokens": before, "request_tokens": requestTokens, "messages": len(msgs)})
	recentBudget := compactRecentBudget(req, contextWindow, coef, calibrated)
	compressed, state, folded, err := r.Compressor.CompressHistoryWithStateBudget(ctx, msgs, sessionState, contextWindow, recentBudget)
	if err != nil {
		logging.Error("memory", "session_compact_failed", err, logging.Event{"mode": "auto", "purpose": req.Purpose, "model": req.Model, "context_window": contextWindow, "before_tokens": before, "request_tokens": requestTokens, "duration_ms": time.Since(started).Milliseconds()})
		return sessionState, err
	}
	if state == "" && folded == 0 {
		logging.Info("memory", "session_compact_noop", logging.Event{"mode": "auto", "purpose": req.Purpose, "model": req.Model, "context_window": contextWindow, "before_tokens": before, "request_tokens": requestTokens, "messages": len(msgs), "duration_ms": time.Since(started).Milliseconds()})
		return sessionState, nil
	}
	working.SetMessages(compressed)
	after := working.EstimatedTokens()
	logging.Info("memory", "session_compact_success", logging.Event{"mode": "auto", "purpose": req.Purpose, "model": req.Model, "context_window": contextWindow, "before_tokens": before, "after_tokens": after, "request_tokens": requestTokens, "folded_messages": folded, "recent_messages": len(compressed), "truncated_tool_outputs": countLargeToolOutputs(msgs), "duration_ms": time.Since(started).Milliseconds()})
	return state, nil
}

func shouldCompactRequest(req *model.CompletionRequest, contextWindow int, coef float64, calibrated bool) bool {
	if req == nil || contextWindow <= 0 {
		return false
	}
	estimated := estimateInputTokens(req, coef)
	return compactContextTokens(estimated, calibrated) > usableInputBudget(contextWindow, req.MaxTokens)
}

func compactContextTokens(estimatedInputTokens int, calibrated bool) int {
	if estimatedInputTokens <= 0 {
		return 0
	}
	return estimatedInputTokens + estimatorSafetyTokens(estimatedInputTokens, calibrated)
}

// estimatorSafetyTokens 返回压缩边界的额外安全垫，补偿估算偏差。
// calibrated 为 true 时（该模型已有稳定校准数据），估算已贴近真实，
// 安全垫从 1/16（6.25%）收到 1/40（2.5%），释放出更多可用上下文；
// 未校准或校准刚回退（中转站异常）时维持原有较厚垫作为兑底。
// 该安全垫只补偿 calibrator 管不到的瞬时偏差（本轮新增、单次波动、系数回退），
// 不能归零。UI 仅展示 raw estimate，不叠加该 safety。
func estimatorSafetyTokens(estimatedInputTokens int, calibrated bool) int {
	if estimatedInputTokens <= 0 {
		return 0
	}
	if calibrated {
		safety := estimatedInputTokens / 40
		if safety < 2048 {
			return 2048
		}
		return safety
	}
	safety := estimatedInputTokens / 16
	if safety < 8192 {
		return 8192
	}
	return safety
}

func estimateRequestTokens(req *model.CompletionRequest, coef float64) int {
	if req == nil {
		return 0
	}
	return estimateInputTokens(req, coef) + req.MaxTokens
}

// estimateInputTokens 返回校准后的 input token 估算；coef 为校准系数（1.0 等价未校准）。
// 压缩触发判断需要物理尺度（贴近模型真实计数），因此在此处乘上系数。
func estimateInputTokens(req *model.CompletionRequest, coef float64) int {
	if req == nil {
		return 0
	}
	total := model.EstimateTokens(req.System)
	total += model.EstimateTokens(model.FormatSessionStateForModel(req.SessionState))
	total += model.EstimateMessagesTokens(req.Messages)
	if len(req.Tools) > 0 {
		if data, err := json.Marshal(req.Tools); err == nil {
			total += model.EstimateTokens(string(data))
		}
	}
	return model.ApplyCoefficient(total, coef)
}

func usableInputBudget(contextWindow, outputBudget int) int {
	budget := contextWindow - outputBudget - contextMargin(contextWindow)
	if budget < 1 {
		return 1
	}
	return budget
}

func contextMargin(contextWindow int) int {
	margin := contextWindow / 200
	if margin < minContextMarginTokens {
		return minContextMarginTokens
	}
	return margin
}

func compactRecentBudget(req *model.CompletionRequest, contextWindow int, coef float64, calibrated bool) int {
	if req == nil || contextWindow <= 0 {
		return 0
	}
	fixed := model.EstimateTokens(req.System) + memory.SessionStateTokenBudget(contextWindow)
	if len(req.Tools) > 0 {
		if data, err := json.Marshal(req.Tools); err == nil {
			fixed += model.EstimateTokens(string(data))
		}
	}
	return recentMessageTokenBudget(contextWindow, req.MaxTokens, fixed, coef, calibrated)
}

func manualCompactRecentBudget(contextWindow, outputBudget int) int {
	if contextWindow <= 0 {
		return 0
	}
	// 手动 compact 不依赖校准状态，保守使用未校准口径（coef=1.0、calibrated=false）。
	return recentMessageTokenBudget(contextWindow, outputBudget, memory.SessionStateTokenBudget(contextWindow), 1.0, false)
}

// recentMessageTokenBudget 返回传给压缩器的 recent 窗口预算。
// 可用空间按物理尺度计算，但压缩器内部用未校准估算填预算，
// 因此需除以系数转回估算尺度，fixed 本就是估算值不参与换算。
func recentMessageTokenBudget(contextWindow, outputBudget, fixedInputTokens int, coef float64, calibrated bool) int {
	budget := usableInputBudget(contextWindow, outputBudget)
	budget -= estimatorSafetyTokens(budget, calibrated)
	if coef > 1.0 {
		budget = int(float64(budget) / coef)
	}
	budget -= fixedInputTokens
	if budget < 1 {
		return 1
	}
	return budget
}

func countLargeToolOutputs(messages []model.Message) int {
	count := 0
	for _, m := range messages {
		if m.Role == model.RoleTool && len(m.Text()) > 50*1024 {
			count++
		}
	}
	return count
}

func trimToolResultsForContext(messages []model.Message) []model.Message {
	out := make([]model.Message, len(messages))
	copy(out, messages)
	for i, m := range out {
		if m.Role != model.RoleTool {
			continue
		}
		text := memory.TruncateToolOutputForContext(m.Text())
		if strings.TrimSpace(text) == strings.TrimSpace(m.Text()) {
			continue
		}
		out[i] = model.Message{
			Role:        model.RoleTool,
			ToolCallID:  m.ToolCallID,
			TextContent: text,
			Content:     []model.ContentBlock{{Type: model.ContentText, Text: text}},
		}
	}
	return out
}
