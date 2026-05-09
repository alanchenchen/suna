package model

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/alanchenchen/suna/internal/config"
)

type Router struct {
	providers map[string]Provider
	models    map[string]config.ModelConfig
	activeRef string
	mu        sync.RWMutex
}

func NewRouter(cfg *config.Config) (*Router, error) {
	r := &Router{providers: map[string]Provider{}, models: map[string]config.ModelConfig{}, activeRef: cfg.ActiveModel}
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

func (r *Router) Route(ctx context.Context, task string) (Provider, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if p, ok := r.providers[r.activeRef]; ok {
		return p, r.activeRef, nil
	}
	return nil, "", fmt.Errorf("active provider not found")
}

func (r *Router) RouteWithLLM(ctx context.Context, task string, explicitRef string) (Provider, string, error) {
	if explicitRef != "" {
		if p, err := r.Provider(explicitRef); err == nil {
			return p, explicitRef, nil
		}
	}
	activeProvider := r.DefaultProvider()
	active := r.ActiveRef()
	if activeProvider == nil {
		return nil, "", fmt.Errorf("active provider not found")
	}
	if strings.TrimSpace(task) == "" || len(r.ListProviders()) <= 1 {
		return activeProvider, active, nil
	}
	if ref := r.routeByLLM(ctx, task, activeProvider); ref != "" {
		if p, err := r.Provider(ref); err == nil {
			return p, ref, nil
		}
	}
	return activeProvider, active, nil
}

func (r *Router) routeByLLM(ctx context.Context, task string, activeProvider Provider) string {
	prompt := fmt.Sprintf("根据以下任务描述，选择最合适的模型。\n\n可用模型:\n%s\n\n任务: %s\n\n只回复 provider/model，不要解释。", r.modelStrengths(), task)
	ch, err := activeProvider.Complete(ctx, &CompletionRequest{System: "你是模型路由器，根据任务选择最合适的模型。", Messages: []Message{NewTextMessage(RoleUser, prompt)}, MaxTokens: 30})
	if err != nil {
		return ""
	}
	var resp string
	for chunk := range ch {
		resp += chunk.Content
		if chunk.Done {
			break
		}
	}
	resp = strings.Trim(strings.TrimSpace(resp), "\"'`")
	if _, err := r.Provider(resp); err == nil {
		return resp
	}
	return ""
}

func (r *Router) modelStrengths() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var lines []string
	for ref, mc := range r.models {
		strengths := strings.Join(mc.Strengths, ", ")
		if strengths == "" {
			strengths = "通用"
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", ref, strengths))
	}
	return strings.Join(lines, "\n")
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

func (r *Router) EmbeddingProvider() (Provider, string) {
	r.mu.RLock()
	refs := make([]string, 0, len(r.providers))
	if r.activeRef != "" {
		refs = append(refs, r.activeRef)
	}
	for ref := range r.providers {
		if ref != r.activeRef {
			refs = append(refs, ref)
		}
	}
	providers := make(map[string]Provider, len(r.providers))
	for ref, p := range r.providers {
		providers[ref] = p
	}
	r.mu.RUnlock()
	for _, ref := range refs {
		if p := providers[ref]; p != nil && p.SupportsEmbedding() {
			return p, ref
		}
	}
	return nil, ""
}

func createProvider(mc config.ModelConfig) (Provider, error) {
	apiKey, err := mc.ResolveAPIKey()
	if err != nil {
		return nil, fmt.Errorf("resolve API key: %w", err)
	}
	switch {
	case mc.IsAnthropic():
		return NewAnthropicProvider(apiKey, mc.Model, mc.ContextWindow), nil
	default:
		return NewOpenAIProvider(apiKey, mc.EffectiveBaseURL(), mc.Model, mc.ContextWindow), nil
	}
}
