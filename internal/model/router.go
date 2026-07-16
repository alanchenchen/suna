package model

import (
	"fmt"
	"strings"
	"sync"

	"github.com/alanchenchen/suna/internal/config"
)

// Router 只负责保存已构建的模型并按 ref 创建显式 binding；它不拥有“当前模型”。
type Router struct {
	providers map[string]Provider
	models    map[string]config.ModelConfig
	mu        sync.RWMutex
	rateLimit *RateLimiter
}

func NewRouter(cfg *config.Config, resolver MediaResolver) (*Router, error) {
	r := &Router{
		providers: map[string]Provider{},
		models:    map[string]config.ModelConfig{},
		rateLimit: NewRateLimiter(cfg.GetMaxModelRPS()),
	}
	for _, mc := range cfg.Models {
		// Router 必须持有配置的深拷贝，调用方后续修改 cfg 不得改变路由快照。
		mc = cloneBindingConfig(mc)
		ref := mc.Ref()
		p, err := createProvider(mc, resolver)
		if err != nil {
			return nil, fmt.Errorf("create provider %q: %w", ref, err)
		}
		r.providers[ref] = p
		r.models[ref] = mc
	}
	return r, nil
}

// Bind 解析 ref 当前的 provider 和配置快照。返回的 binding 不会随 Router 后续更新而改变。
func (r *Router) Bind(ref string) (*ModelBinding, error) {
	if r == nil {
		return nil, &BindingError{Kind: BindingErrorRouterUnavailable}
	}
	r.mu.RLock()
	provider, ok := r.providers[ref]
	mc, modelOK := r.models[ref]
	rateLimit := r.rateLimit
	r.mu.RUnlock()
	if !ok || !modelOK {
		return nil, &BindingError{Kind: BindingErrorModelNotFound, Ref: ref}
	}
	configSnapshot := cloneBindingConfig(mc)
	return &ModelBinding{ref: ref, modelID: configSnapshot.Model, provider: provider, config: configSnapshot, rateLimit: rateLimit}, nil
}

func validateToolResultPairs(messages []Message) error {
	seen := make(map[string]struct{})
	for _, m := range messages {
		switch m.Role {
		case RoleAssistant:
			for _, tc := range m.ToolCalls {
				id := strings.TrimSpace(tc.ID)
				if id != "" {
					seen[id] = struct{}{}
				}
			}
		case RoleTool:
			id := strings.TrimSpace(m.ToolCallID)
			if id == "" {
				return fmt.Errorf("model request contains tool result without tool_call_id")
			}
			if _, ok := seen[id]; !ok {
				return fmt.Errorf("model request contains orphan tool result call_id %q; compact the session or start a new session", id)
			}
		}
	}
	return nil
}

func (r *Router) ModelConfig(ref string) (*config.ModelConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	mc, ok := r.models[ref]
	if !ok {
		return nil, fmt.Errorf("model %q not found", ref)
	}
	returnMC := cloneBindingConfig(mc)
	return &returnMC, nil
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

// ListSpawnableModels 返回 parentRef 可见的子任务模型。父模型必须由调用方显式传入。
func (r *Router) ListSpawnableModels(parentRef string) []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	refs := make([]string, 0, len(r.models))
	for ref, mc := range r.models {
		if mc.AvailableAsSubtaskFor(parentRef) {
			refs = append(refs, ref)
		}
	}
	return refs
}

func (r *Router) IsSpawnableModel(parentRef, ref string) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	mc, ok := r.models[ref]
	return ok && mc.AvailableAsSubtaskFor(parentRef)
}

func createProvider(mc config.ModelConfig, resolver MediaResolver) (Provider, error) {
	apiKey, err := mc.ResolveAPIKey()
	if err != nil {
		return nil, fmt.Errorf("resolve API key: %w", err)
	}
	if strings.TrimSpace(mc.BaseURL) == "" {
		return nil, fmt.Errorf("provider %q requires base_url", mc.Provider)
	}
	switch mc.ProtocolOrDefault() {
	case config.ModelProtocolAnthropic:
		return NewAnthropicProvider(apiKey, mc.BaseURL, mc.Model, mc.ContextWindow, mc.MaxOutputTokens, resolver), nil
	case config.ModelProtocolOpenAIResponses:
		return NewOpenAIResponsesProvider(apiKey, mc.BaseURL, mc.Model, mc.ContextWindow, mc.MaxOutputTokens, resolver), nil
	case config.ModelProtocolOpenAIChat:
		return NewOpenAIChatProvider(apiKey, mc.BaseURL, mc.Model, mc.ContextWindow, mc.MaxOutputTokens, resolver), nil
	default:
		return nil, fmt.Errorf("protocol %q is not supported", mc.Protocol)
	}
}
