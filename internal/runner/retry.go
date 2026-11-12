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

// retryableModelRequestError 判定一次物理请求失败是否值得自动重试。
// 判据是用户按 Enter 重试能否恢复：上游暂时性故障和协议不完整都能恢复，
// 而请求本身的问题、用户主动取消、已产出内容后的失败都不能。
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
	switch me.Kind {
	case model.ModelErrorNetwork:
		return true
	case model.ModelErrorCancelled, model.ModelErrorInternal:
		// 用户主动取消不能重试；Internal 目前没有产生点，保持不重试。
		return false
	case model.ModelErrorHTTP:
		// 4xx 是请求本身的问题，重试得到同样结果；408/425/429 是暂时性例外。
		if me.StatusCode >= 400 && me.StatusCode < 500 {
			switch me.StatusCode {
			case 408, 425, 429:
				return true
			}
			return false
		}
		return me.StatusCode >= 500 && me.StatusCode < 600
	default:
		// 未标记的上游失败多来自协议不完整（缺完成标记、流提前关闭、空闲超时），
		// 用户按 Enter 通常能恢复，自动重试替用户完成这一步。
		//
		// 已知取舍：Adapter 在发出请求前的本地失败（如 max_output_tokens 未配置、
		// 图片解析失败）也归为 Unknown，会被重试并白白消耗一次预算。这类错误极少见，
		// 排除它们需要给三个 Adapter 分别加哨兵，目前不值得；真遇到时再按错误来源区分。
		//
		// default 同时覆盖未来新增的 Kind：默认偏向重试，因为漏重试会让用户
		// 手动按 Enter，而多试一次只消耗预算。新增确定不可恢复的 Kind 时必须在此显式排除。
		return true
	}
}
