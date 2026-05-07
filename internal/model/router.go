package model

import (
	"context"
	"fmt"
	"regexp"
	"sync"

	"github.com/alanchenchen/suna/internal/config"
)

type Router struct {
	providers map[string]Provider
	config    *config.RouterConfig
	mu        sync.RWMutex
}

func NewRouter(cfg *config.Config) (*Router, error) {
	r := &Router{
		providers: make(map[string]Provider),
		config:    &cfg.Router,
	}
	for name, mc := range cfg.Models {
		p, err := createProvider(mc)
		if err != nil {
			return nil, fmt.Errorf("create provider %q: %w", name, err)
		}
		r.providers[name] = p
	}
	if _, ok := r.providers[cfg.Router.Default]; !ok {
		if _, ok := r.providers["default"]; !ok {
			return nil, fmt.Errorf("default model not found in providers")
		}
		r.config.Default = "default"
	}
	return r, nil
}

func (r *Router) Provider(name string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %q not found", name)
	}
	return p, nil
}

func (r *Router) DefaultProvider() Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.providers[r.config.Default]
}

func (r *Router) Route(ctx context.Context, task string) (Provider, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, rule := range r.config.Rules {
		re, err := regexp.Compile(rule.Pattern)
		if err != nil {
			continue
		}
		if re.MatchString(task) {
			if p, ok := r.providers[rule.Model]; ok {
				return p, rule.Model, nil
			}
		}
	}

	p, ok := r.providers[r.config.Default]
	if !ok {
		return nil, "", fmt.Errorf("default provider not found")
	}
	return p, r.config.Default, nil
}

func (r *Router) ModelConfig(name string) (*config.ModelConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.providers[name]; !ok {
		return nil, fmt.Errorf("model %q not found", name)
	}
	return nil, nil
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
	default:
		return NewOpenAIProvider(apiKey, mc.BaseURL, mc.Model, mc.ContextWindow), nil
	}
}
