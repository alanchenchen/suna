package model

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

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

func NewRouter(cfg *config.Config, resolver MediaResolver) (*Router, error) {
	r := &Router{
		providers: map[string]Provider{},
		models:    map[string]config.ModelConfig{},
		activeRef: cfg.ActiveModel,
		rateLimit: NewRateLimiter(cfg.GetMaxModelRPS()),
	}
	for _, mc := range cfg.Models {
		ref := mc.Ref()
		p, err := createProvider(mc, resolver)
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

// NewRoutedProvider 返回一个通过 Router.ActiveRef 调用模型的 Provider 适配器。
// 后台压缩、记忆整理等非 Runner 请求也必须经过 Router，才能复用限流、reasoning 配置和统一 LLM 日志。
func NewRoutedProvider(router *Router) Provider {
	if router == nil {
		return nil
	}
	return routedProvider{router: router}
}

// routedProvider 不持有底层 provider 快照，而是在每次调用时解析 active provider；
// 这样配置热更新后，后台任务不会继续使用旧模型实例。
type routedProvider struct {
	router *Router
}

func (p routedProvider) Complete(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error) {
	if p.router == nil {
		return nil, fmt.Errorf("model router is not configured")
	}
	return p.router.Complete(ctx, p.router.ActiveRef(), req)
}

func (p routedProvider) EstimateTokens(text string) int {
	provider := p.defaultProvider()
	if provider == nil {
		return EstimateTokens(text)
	}
	return provider.EstimateTokens(text)
}

func (p routedProvider) ContextWindow() int {
	if p.router == nil {
		return 0
	}
	return p.router.ActiveContextWindow()
}

func (p routedProvider) MaxOutputTokens() int {
	if p.router == nil {
		return 0
	}
	return p.router.ActiveMaxOutputTokens()
}

func (p routedProvider) defaultProvider() Provider {
	if p.router == nil {
		return nil
	}
	return p.router.DefaultProvider()
}

func (r *Router) ActiveRef() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.activeRef
}

func (r *Router) ActiveContextWindow() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	p := r.providers[r.activeRef]
	r.mu.RUnlock()
	if p == nil {
		return 0
	}
	return p.ContextWindow()
}

func (r *Router) ActiveMaxOutputTokens() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	p := r.providers[r.activeRef]
	r.mu.RUnlock()
	if p == nil {
		return 0
	}
	return p.MaxOutputTokens()
}

// Complete 调用指定 provider 的 Complete，自动执行 per-model 速率限制。
func (r *Router) Complete(ctx context.Context, ref string, req *CompletionRequest) (<-chan Chunk, error) {
	r.mu.RLock()
	p, ok := r.providers[ref]
	mc := r.models[ref]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("model %q not found", ref)
	}
	if req != nil {
		ensureRequestID(req)
		if len(req.Reasoning) == 0 && len(mc.Reasoning) > 0 {
			req.Reasoning = mc.Reasoning
		}
	}
	if err := r.rateLimit.Wait(ctx, ref); err != nil {
		return nil, err
	}
	started := time.Now()
	route := llmRoute{
		Provider: mc.Provider,
		Protocol: string(mc.ProtocolOrDefault()),
		ModelRef: ref,
		Model:    resolvedRequestModel(mc, req),
	}
	raw, err := p.Complete(ctx, req)
	if err != nil {
		fields := loggingFields(started, nil)
		fields["usage_received"] = false
		logRoutedLLMFailure(req, route, err, fields)
		return nil, err
	}
	return wrapLLMLogStream(raw, req, route, started), nil
}

func resolvedRequestModel(mc config.ModelConfig, req *CompletionRequest) string {
	if req != nil && req.Model != "" {
		return req.Model
	}
	return mc.Model
}

func wrapLLMLogStream(raw <-chan Chunk, req *CompletionRequest, route llmRoute, started time.Time) <-chan Chunk {
	out := make(chan Chunk, providerChunkBuffer)
	go func() {
		defer close(out)
		usage, stats, failed, modelErr := collectAndForwardLLMChunks(raw, out, started)
		fields := loggingFields(started, usage)
		fields["tool_calls"] = stats.toolCalls
		fields["chunk_count"] = stats.chunkCount
		fields["assistant_bytes"] = stats.assistantBytes
		fields["reasoning_bytes"] = stats.reasoningBytes
		fields["usage_received"] = stats.usageReceived
		if failed {
			fields["last_chunk_age_ms"] = time.Since(stats.lastChunkAt).Milliseconds()
			logRoutedLLMFailure(req, route, modelErr, fields)
			return
		}
		logRoutedLLMSuccess(req, route, fields)
	}()
	return out
}

func collectAndForwardLLMChunks(raw <-chan Chunk, out chan<- Chunk, started time.Time) (*Usage, llmStreamStats, bool, *ModelError) {
	stats := llmStreamStats{lastChunkAt: started}
	var usage *Usage
	for chunk := range raw {
		if chunk.Error != nil {
			out <- chunk
			return usage, stats, true, chunk.Error
		}
		stats.chunkCount++
		stats.lastChunkAt = time.Now()
		stats.assistantBytes += len(chunk.Content)
		stats.reasoningBytes += len(chunk.ReasoningContent)
		stats.toolCalls += len(chunk.ToolCalls)
		if chunk.Usage != nil {
			usage = chunk.Usage
			stats.usageReceived = true
		}
		out <- chunk
	}
	return usage, stats, false, nil
}

func (r *Router) MaxOutputTokens(ref string) int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	p := r.providers[ref]
	r.mu.RUnlock()
	if p == nil {
		return 0
	}
	return p.MaxOutputTokens()
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

// ListSpawnableModels 返回当前 active 模型可见的子任务模型；
// subtask_for 只影响候选列表，不改变模型 strengths 或实际工具授权。
func (r *Router) ListSpawnableModels() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	refs := make([]string, 0, len(r.models))
	for ref, mc := range r.models {
		if mc.AvailableAsSubtaskFor(r.activeRef) {
			refs = append(refs, ref)
		}
	}
	return refs
}

func (r *Router) IsSpawnableModel(ref string) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	mc, ok := r.models[ref]
	return ok && mc.AvailableAsSubtaskFor(r.activeRef)
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
