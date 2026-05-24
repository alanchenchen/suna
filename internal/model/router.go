package model

import (
	"context"
	"fmt"
	"sync"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/prompt"
)

type Router struct {
	providers map[string]Provider
	models    map[string]config.ModelConfig
	activeRef string
	mu        sync.RWMutex
	prompts   *prompt.Loader
	rateLimit *RateLimiter
}

func NewRouter(cfg *config.Config) (*Router, error) {
	r := &Router{
		providers: map[string]Provider{},
		models:    map[string]config.ModelConfig{},
		activeRef: cfg.ActiveModel,
		rateLimit: NewRateLimiter(cfg.GetMaxModelRPS()),
	}
	for _, mc := range cfg.Models {
		ref := mc.Ref()
		p, err := createProvider(mc)
		if err != nil {
			return nil, fmt.Errorf("create provider %q: %w", ref, err)
		}
		r.providers[ref] = p
		r.models[ref] = mc
	}
	if _, ok := r.providers[r.activeRef]; !ok {
		return nil, fmt.Errorf("active model %q not found", r.activeRef)
	}
	return r, nil
}

func (r *Router) SetPrompts(p *prompt.Loader) {
	r.prompts = p
}

func (r *Router) Provider(ref string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[ref]
	if !ok {
		return nil, fmt.Errorf("model %q not found", ref)
	}
	return p, nil
}

func (r *Router) DefaultProvider() Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.providers[r.activeRef]
}

func (r *Router) ActiveRef() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.activeRef
}

// Complete 调用指定 provider 的 Complete，自动执行 per-model 速率限制
func (r *Router) Complete(ctx context.Context, ref string, req *CompletionRequest) (<-chan Chunk, error) {
	r.mu.RLock()
	p, ok := r.providers[ref]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("model %q not found", ref)
	}
	if err := r.rateLimit.Wait(ctx, ref); err != nil {
		return nil, err
	}
	return p.Complete(ctx, req)
}

func (r *Router) Route(ctx context.Context, task string) (Provider, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if p, ok := r.providers[r.activeRef]; ok {
		return p, r.activeRef, nil
	}
	return nil, "", fmt.Errorf("active provider not found")
}

func (r *Router) ModelConfig(ref string) (*config.ModelConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	mc, ok := r.models[ref]
	if !ok {
		return nil, fmt.Errorf("model %q not found", ref)
	}
	return &mc, nil
}

func (r *Router) ListProviders() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.providers))
	for k := range r.providers {
		names = append(names, k)
	}
	return names
}

func createProvider(mc config.ModelConfig) (Provider, error) {
	apiKey, err := mc.ResolveAPIKey()
	if err != nil {
		return nil, fmt.Errorf("resolve API key: %w", err)
	}
	switch {
	case mc.IsAnthropic():
		return NewAnthropicProvider(apiKey, mc.Model, mc.ContextWindow), nil
	case mc.IsOpenAI():
		// 内置 openai 固定走官方 Responses API；不要把自定义 base_url 混进这条路径。
		return NewOpenAIResponsesProvider(apiKey, mc.Model, mc.ContextWindow), nil
	default:
		// 其他 provider 视为 OpenAI-compatible，走 Chat Completions 协议并要求配置 base_url。
		return NewOpenAIChatProvider(apiKey, mc.BaseURL, mc.Model, mc.ContextWindow), nil
	}
}
