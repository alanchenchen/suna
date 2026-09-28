package runner

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"time"

	"github.com/alanchenchen/suna/internal/model"
)

// RetryPolicy 描述一次物理模型请求的恢复预算与退避策略。
// 重试次数不作为策略约束，而由总预算、请求耗时和退避间隔共同决定。
type RetryPolicy struct {
	Budget        time.Duration
	InitialDelay  time.Duration
	BackoffFactor float64
	MaxDelay      time.Duration
	JitterRatio   float64
}

// DefaultRetryPolicy 返回主 Agent 与 Subtask 共用的默认请求恢复策略。
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		Budget:        2 * time.Minute,
		InitialDelay:  2 * time.Second,
		BackoffFactor: 2,
		MaxDelay:      30 * time.Second,
		JitterRatio:   0.2,
	}
}

func (p RetryPolicy) normalized() RetryPolicy {
	defaults := DefaultRetryPolicy()
	if p.Budget <= 0 {
		p.Budget = defaults.Budget
	}
	if p.InitialDelay <= 0 {
		p.InitialDelay = defaults.InitialDelay
	}
	if p.BackoffFactor < 1 {
		p.BackoffFactor = defaults.BackoffFactor
	}
	if p.MaxDelay <= 0 {
		p.MaxDelay = defaults.MaxDelay
	}
	if p.MaxDelay < p.InitialDelay {
		p.MaxDelay = p.InitialDelay
	}
	if p.JitterRatio < 0 {
		p.JitterRatio = 0
	}
	if p.JitterRatio > 1 {
		p.JitterRatio = 1
	}
	return p
}

func (p RetryPolicy) deadline(start time.Time) time.Time {
	return start.Add(p.normalized().Budget)
}

func (p RetryPolicy) delay(attempt int) time.Duration {
	p = p.normalized()
	if attempt < 1 {
		attempt = 1
	}
	base := float64(p.InitialDelay) * math.Pow(p.BackoffFactor, float64(attempt-1))
	if base > float64(p.MaxDelay) {
		base = float64(p.MaxDelay)
	}
	jitter := 1 + ((rand.Float64()*2)-1)*p.JitterRatio
	delay := time.Duration(base * jitter)
	if delay > p.MaxDelay {
		delay = p.MaxDelay
	}
	return delay
}

func retryableModelRequestError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	me := model.NewModelError(err)
	if me == nil {
		return false
	}
	if me.Kind == model.ModelErrorNetwork {
		return true
	}
	if me.Kind != model.ModelErrorHTTP {
		return false
	}
	switch me.StatusCode {
	case 408, 425, 429,
		500, 502, 503, 504,
		520, 521, 522, 523, 524, 529:
		return true
	default:
		return false
	}
}
