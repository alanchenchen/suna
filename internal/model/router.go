package model

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

type RouteResult struct {
	ModelRef string
	Tools    []string
}

func (r *Router) RouteWithLLM(ctx context.Context, task string, explicitRef string) (*RouteResult, error) {
	if explicitRef != "" {
		if _, err := r.Provider(explicitRef); err == nil {
			return &RouteResult{ModelRef: explicitRef}, nil
		}
	}
	activeProvider := r.DefaultProvider()
	active := r.ActiveRef()
	if activeProvider == nil {
		return nil, fmt.Errorf("active provider not found")
	}
	if strings.TrimSpace(task) == "" || len(r.ListProviders()) <= 1 {
		return &RouteResult{ModelRef: active}, nil
	}
	if result := r.routeByLLM(ctx, task, activeProvider); result != nil {
		return result, nil
	}
	return &RouteResult{ModelRef: active}, nil
}

func (r *Router) routeByLLM(ctx context.Context, task string, activeProvider Provider) *RouteResult {
	strengths := r.modelStrengths()
	var userPrompt string
	if r.prompts != nil {
		rendered, err := r.prompts.RenderRoute(strengths, task)
		if err == nil && rendered != "" {
			userPrompt = rendered
		}
	}
	if userPrompt == "" {
		userPrompt = fmt.Sprintf("Select the best model.\n\nAvailable:\n%s\n\nTask: %s\n\nReply JSON: {\"model\":\"provider/model\",\"tools\":[...]}", strengths, task)
	}
	ch, err := activeProvider.Complete(ctx, &CompletionRequest{System: "Reply with JSON only.", Messages: []Message{NewTextMessage(RoleUser, userPrompt)}, MaxTokens: 100})
	if err != nil {
		return nil
	}
	var resp string
	for chunk := range ch {
		resp += chunk.Content
		if chunk.Done {
			break
		}
	}
	resp = strings.Trim(strings.TrimSpace(resp), "\"'`")
	var decision struct {
		Model string   `json:"model"`
		Tools []string `json:"tools"`
	}
	if err := json.Unmarshal([]byte(extractRouteJSON(resp)), &decision); err != nil {
		if _, perr := r.Provider(resp); perr == nil {
			return &RouteResult{ModelRef: resp}
		}
		return nil
	}
	if _, perr := r.Provider(decision.Model); perr != nil {
		return nil
	}
	result := &RouteResult{ModelRef: decision.Model, Tools: decision.Tools}
	return result
}

func extractRouteJSON(s string) string {
	s = strings.TrimSpace(s)
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
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
