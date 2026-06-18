package runner

import (
	"strings"

	"github.com/alanchenchen/suna/internal/logging"
	"github.com/alanchenchen/suna/internal/model"
)

func logRequestPrepare(req Request, completionReq *model.CompletionRequest, contextWindow, turn int) int {
	if completionReq == nil {
		return 0
	}
	estimated := estimateInputTokens(completionReq)
	safety := estimatorSafetyTokens(estimated)
	compactTokens := estimated + safety
	inputLimit := usableInputBudget(contextWindow, completionReq.MaxTokens)
	logging.Info("llm", "request_prepare", logging.Event{
		"purpose":                  req.Purpose,
		"model":                    req.ModelID,
		"model_ref":                req.ModelRef,
		"request_id":               completionReq.RequestID,
		"turn":                     turn,
		"request_messages":         len(completionReq.Messages),
		"tool_defs":                len(completionReq.Tools),
		"estimated_context_tokens": estimated,
		"estimator_safety_tokens":  safety,
		"compact_context_tokens":   compactTokens,
		"input_limit":              inputLimit,
		"max_tokens":               completionReq.MaxTokens,
		"context_window":           contextWindow,
		"session_state_chars":      len(strings.TrimSpace(completionReq.SessionState)),
		"auto_compress":            req.AutoCompress,
	})
	return estimated
}
