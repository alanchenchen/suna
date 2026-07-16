package model

import (
	"context"
	"fmt"
	"time"

	"github.com/alanchenchen/suna/internal/config"
)

// ModelBinding 是一次显式模型选择的不可变快照。它实现 Provider，所有调用均通过
// Router 共享的限流、reasoning 注入、LLM 日志和工具调用配对校验。
type ModelBinding struct {
	ref       string
	modelID   string
	provider  Provider
	config    config.ModelConfig
	rateLimit *RateLimiter
}

type modelBindingContextKey struct{}

// WithBinding 将当前 run 的不可变模型绑定放入上下文，供 Guard、Skill 等同轮辅助请求复用。
func WithBinding(ctx context.Context, binding *ModelBinding) context.Context {
	return context.WithValue(ctx, modelBindingContextKey{}, binding)
}

// BindingFromContext 返回当前 run 的模型绑定；不存在时调用方必须显式失败，不能回退到默认模型。
func BindingFromContext(ctx context.Context) *ModelBinding {
	binding, _ := ctx.Value(modelBindingContextKey{}).(*ModelBinding)
	return binding
}

func (b *ModelBinding) Ref() string { return b.ref }

func (b *ModelBinding) ModelID() string { return b.modelID }

func (b *ModelBinding) Config() config.ModelConfig {
	if b == nil {
		return config.ModelConfig{}
	}
	return cloneBindingConfig(b.config)
}

func cloneBindingConfig(mc config.ModelConfig) config.ModelConfig {
	mc.Strengths = append([]string(nil), mc.Strengths...)
	mc.SubtaskFor = append([]string(nil), mc.SubtaskFor...)
	mc.Reasoning = cloneBindingOptions(mc.Reasoning)
	return mc
}

// cloneBindingOptions 深拷贝 Reasoning 的 JSON-like 值（map[string]any、[]any、标量和 nil），
// 保证 Binding 内外不存在可变引用共享。
func cloneBindingOptions(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneBindingOptionValue(value)
	}
	return out
}

func cloneBindingOptionValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return cloneBindingOptions(value)
	case []any:
		if value == nil {
			return nil
		}
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = cloneBindingOptionValue(item)
		}
		return out
	default:
		return value
	}
}

func (b *ModelBinding) EstimateTokens(text string) int {
	if b == nil || b.provider == nil {
		return EstimateTokens(text)
	}
	return b.provider.EstimateTokens(text)
}

func (b *ModelBinding) ContextWindow() int {
	if b == nil || b.provider == nil {
		return 0
	}
	return b.provider.ContextWindow()
}

func (b *ModelBinding) MaxOutputTokens() int {
	if b == nil || b.provider == nil {
		return 0
	}
	return b.provider.MaxOutputTokens()
}

func (b *ModelBinding) Complete(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error) {
	if b == nil || b.provider == nil {
		return nil, fmt.Errorf("model binding is not configured")
	}
	if req != nil {
		ensureRequestID(req)
		if len(req.Reasoning) == 0 && len(b.config.Reasoning) > 0 {
			req.Reasoning = cloneBindingOptions(b.config.Reasoning)
		}
		if err := validateToolResultPairs(req.Messages); err != nil {
			return nil, err
		}
	}
	if b.rateLimit != nil {
		if err := b.rateLimit.Wait(ctx, b.ref); err != nil {
			return nil, err
		}
	}
	started := time.Now()
	route := newLLMRoute(b.ref, b.config, req)
	raw, err := b.provider.Complete(ctx, req)
	if err != nil {
		logLLMRequestStartFailure(req, route, started, err)
		return nil, err
	}
	return logAndForwardLLMRequestStream(raw, req, route, started), nil
}
