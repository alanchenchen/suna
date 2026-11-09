package runner

import (
	"encoding/json"

	"github.com/alanchenchen/suna/internal/model"
)

const minContextMarginTokens = 2048

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
