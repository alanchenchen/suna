package runner

import (
	"strings"

	"github.com/alanchenchen/suna/internal/logging"
	"github.com/alanchenchen/suna/internal/model"
)

// logRequestPrepare 记录请求准备日志，并返回（原始估算, 校准后估算）。
// 原始估算用于回喂校准器（必须是未乘系数的值），校准后估算用于 UI 显示和压缩判断口径。
func logRequestPrepare(req Request, completionReq *model.CompletionRequest, contextWindow, turn int, coef float64, isCalibrated bool) (int, int) {
	if completionReq == nil {
		return 0, 0
	}
	raw := estimateInputTokens(completionReq, 1.0)
	calibrated := model.ApplyCoefficient(raw, coef)
	safety := estimatorSafetyTokens(calibrated, isCalibrated)
	compactTokens := calibrated + safety
	inputLimit := usableInputBudget(contextWindow, completionReq.MaxTokens)
	logging.Info("llm", "request_prepare", logging.Event{
		"purpose":                  req.Purpose,
		"model":                    completionReq.Model,
		"model_ref":                bindingRef(req),
		"request_id":               completionReq.RequestID,
		"turn":                     turn,
		"request_messages":         len(completionReq.Messages),
		"tool_defs":                len(completionReq.Tools),
		"estimated_context_tokens": calibrated,
		"raw_estimated_tokens":     raw,
		"calibration_coef":         coef,
		"estimator_safety_tokens":  safety,
		"compact_context_tokens":   compactTokens,
		"input_limit":              inputLimit,
		"max_tokens":               completionReq.MaxTokens,
		"context_window":           contextWindow,
		"session_state_chars":      len(strings.TrimSpace(completionReq.SessionState)),
		"auto_compress":            req.AutoCompress,
	})
	return raw, calibrated
}

func bindingRef(req Request) string {
	if req.Binding == nil {
		return ""
	}
	return req.Binding.Ref()
}
