package model

import (
	"math"
	"sync"
)

// TokenCalibrator 用模型返回的真实 input token 校准本地估算。
// 本地估算公式按通用 tokenizer 调参，不同 provider（如 Claude）会系统性偏差；
// 校准系数 = 真实 input / 本地估算，>1 表示本地低估，需要在压缩判断时抬高估算。
//
// 设计要点（都为防止压缩触发被错误数据带偏）：
//   - 硬区间过滤：明显离谱的 ratio（如中转站 usage 回传错误）直接丢弃。
//   - 相对离群过滤：已有稳定系数后，单次 ratio 偏离当前系数过大视为抖动，跳过本次更新。
//   - EMA 平滑：即使异常值漏过前两道，单次也只能挪动系数一小步，后续正常请求会迅速拉回。
//   - 兜底默认 1.0：无校准数据时行为与未校准完全一致。
type TokenCalibrator struct {
	mu    sync.RWMutex
	coefs map[string]float64
}

const (
	// 硬区间：真实/估算比值落在此范围外，视为不可信数据直接丢弃。
	calibrationMinRatio = 0.25
	calibrationMaxRatio = 4.0
	// 相对离群：已有稳定系数后，ratio 相对当前系数的允许波动倍率。
	// 例如当前系数 2.0，则只接受 [1.0, 4.0] 的 ratio，挡住“这次错、下次对”的抖动。
	calibrationOutlierLow  = 0.5
	calibrationOutlierHigh = 2.0
	// EMA 平滑权重：新观测占比，越小越稳。
	calibrationAlpha = 0.25
)

func NewTokenCalibrator() *TokenCalibrator {
	return &TokenCalibrator{coefs: map[string]float64{}}
}

// Coefficient 返回指定模型的校准系数；无校准数据时返回 1.0（等价未校准）。
func (c *TokenCalibrator) Coefficient(modelRef string) float64 {
	if c == nil || modelRef == "" {
		return 1.0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if coef, ok := c.coefs[modelRef]; ok && coef > 0 {
		return coef
	}
	return 1.0
}

// Calibrated 表示指定模型是否已有有效校准数据。
// 用于决定安全垫大小：已校准说明估算已贴近真实，安全垫可收小；
// 不依赖 Coefficient 是否等于 1.0，因为校准准的模型（如 GLM）系数本就可能接近 1.0。
func (c *TokenCalibrator) Calibrated(modelRef string) bool {
	if c == nil || modelRef == "" {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	coef, ok := c.coefs[modelRef]
	return ok && coef > 0
}

// Observe 用一次请求的真实 input token 与对应的原始本地估算更新校准系数。
// estimatedTokens 必须是未经校准的原始估算，actualTokens 是 provider 返回的真实 input。
func (c *TokenCalibrator) Observe(modelRef string, estimatedTokens, actualTokens int) {
	if c == nil || modelRef == "" || estimatedTokens <= 0 || actualTokens <= 0 {
		return
	}
	ratio := float64(actualTokens) / float64(estimatedTokens)
	// 第一道：硬区间过滤，挡住完全离谱的回传值。
	if ratio < calibrationMinRatio || ratio > calibrationMaxRatio {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	current, ok := c.coefs[modelRef]
	if !ok || current <= 0 {
		// 冷启动：首个落在硬区间内的观测直接作为初值，不做相对离群判断。
		c.coefs[modelRef] = ratio
		return
	}
	// 第二道：相对离群过滤，已有稳定系数时拒绝单次大幅偏离（典型为中转站 usage 抖动）。
	if ratio < current*calibrationOutlierLow || ratio > current*calibrationOutlierHigh {
		return
	}
	// 第三道：EMA 平滑，单次观测只挪动系数一小步。
	c.coefs[modelRef] = current*(1-calibrationAlpha) + ratio*calibrationAlpha
}

// ApplyCoefficient 将估算 token 按校准系数折算为真实尺度（向上取整避免低估边界）。
func ApplyCoefficient(tokens int, coef float64) int {
	if tokens <= 0 || coef <= 0 || coef == 1.0 {
		return tokens
	}
	return int(math.Ceil(float64(tokens) * coef))
}
