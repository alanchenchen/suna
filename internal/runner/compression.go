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

func (r *Runner) compactForRequest(ctx context.Context, working *memory.WorkingMemory, req *model.CompletionRequest, contextWindow int, sessionState string) (string, error) {
	if r.Compressor == nil || working == nil {
		return sessionState, nil
	}
	if !shouldCompactRequest(req, contextWindow) {
		return sessionState, nil
	}
	started := time.Now()
	msgs := working.Messages()
	before := working.EstimatedTokens()
	requestTokens := estimateRequestTokens(req)
	logging.Info("memory", "session_compact_start", logging.Event{"mode": "auto", "purpose": req.Purpose, "model": req.Model, "context_window": contextWindow, "before_tokens": before, "request_tokens": requestTokens, "messages": len(msgs)})
	recentBudget := compactRecentBudget(req, contextWindow)
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

func shouldCompactRequest(req *model.CompletionRequest, contextWindow int) bool {
	if req == nil || contextWindow <= 0 {
		return false
	}
	estimated := estimateInputTokens(req)
	return compactContextTokens(estimated) > usableInputBudget(contextWindow, req.MaxTokens)
}

func compactContextTokens(estimatedInputTokens int) int {
	if estimatedInputTokens <= 0 {
		return 0
	}
	return estimatedInputTokens + estimatorSafetyTokens(estimatedInputTokens)
}

func estimatorSafetyTokens(estimatedInputTokens int) int {
	if estimatedInputTokens <= 0 {
		return 0
	}
	// token 估算会随上下文结构变化产生偏差；后期代码、JSON、工具结果占比升高时
	// 当前轻量估算可能低估约 5%。压缩边界需要额外安全垫，避免接近模型窗口时
	// 因 raw estimate 偏低而触发过晚。UI 仍展示 raw estimate，不叠加该 safety。
	safety := estimatedInputTokens / 16
	if safety < 8192 {
		return 8192
	}
	return safety
}

func estimateRequestTokens(req *model.CompletionRequest) int {
	if req == nil {
		return 0
	}
	return estimateInputTokens(req) + req.MaxTokens
}

func estimateInputTokens(req *model.CompletionRequest) int {
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
	return total
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

func compactRecentBudget(req *model.CompletionRequest, contextWindow int) int {
	if req == nil || contextWindow <= 0 {
		return 0
	}
	fixed := model.EstimateTokens(req.System) + memory.SessionStateTokenBudget(contextWindow)
	if len(req.Tools) > 0 {
		if data, err := json.Marshal(req.Tools); err == nil {
			fixed += model.EstimateTokens(string(data))
		}
	}
	return recentMessageTokenBudget(contextWindow, req.MaxTokens, fixed)
}

func manualCompactRecentBudget(contextWindow, outputBudget int) int {
	if contextWindow <= 0 {
		return 0
	}
	return recentMessageTokenBudget(contextWindow, outputBudget, memory.SessionStateTokenBudget(contextWindow))
}

func recentMessageTokenBudget(contextWindow, outputBudget, fixedInputTokens int) int {
	budget := usableInputBudget(contextWindow, outputBudget)
	budget -= estimatorSafetyTokens(budget)
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
