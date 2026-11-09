package model

import (
	"context"
	"testing"

	"github.com/alanchenchen/suna/internal/config"
)

type recordingAdapter struct {
	request CompletionRequest
}

func (a *recordingAdapter) Complete(_ context.Context, req CompletionRequest) (<-chan Chunk, error) {
	a.request = req
	ch := make(chan Chunk)
	close(ch)
	return ch, nil
}

func (*recordingAdapter) EstimateTokens(string) int { return 0 }
func (*recordingAdapter) ContextWindow() int        { return 0 }
func (*recordingAdapter) MaxOutputTokens() int      { return 0 }

func TestModelBindingCompletesWithBoundModel(t *testing.T) {
	adapter := &recordingAdapter{}
	binding := &ModelBinding{modelID: "bound-model", adapter: adapter, config: config.ModelConfig{Model: "bound-model"}}
	if _, err := binding.Complete(context.Background(), CompletionRequest{}); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if got, want := adapter.request.Model, "bound-model"; got != want {
		t.Fatalf("request Model = %q, want %q", got, want)
	}
}

func TestModelBindingRejectsDifferentRequestModel(t *testing.T) {
	binding := &ModelBinding{modelID: "bound-model", adapter: &recordingAdapter{}, config: config.ModelConfig{Model: "bound-model"}}
	if _, err := binding.Complete(context.Background(), CompletionRequest{Model: "other-model"}); err == nil {
		t.Fatal("Complete() error = nil, want bound model mismatch")
	}
}

func TestModelBindingSameRouteUsesAdapterLimiterAndConfig(t *testing.T) {
	const ref = "local/model"
	mc := config.ModelConfig{
		Provider: "local", Model: "model", Strengths: []string{"code"},
		Reasoning: map[string]any{"nested": map[string]any{"level": "high"}},
	}
	router := &Router{
		adapters:  map[string]Adapter{ref: &recordingAdapter{}},
		models:    map[string]config.ModelConfig{ref: mc},
		rateLimit: NewRateLimiter(100),
	}
	first, err := router.Bind(ref)
	if err != nil {
		t.Fatal(err)
	}
	second, err := router.Bind(ref)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !first.SameRoute(second) || !second.SameRoute(first) {
		t.Fatal("同一 Router 的独立快照应属于同一路由")
	}

	// 分别只改变一个维度，避免新 Router 同时改变多个身份而掩盖漏比。
	adapterChanged := *first
	adapterChanged.adapter = &recordingAdapter{}
	limiterChanged := *first
	limiterChanged.rateLimit = NewRateLimiter(100)
	configChanged := *first
	configChanged.config = cloneBindingConfig(first.config)
	configChanged.config.Reasoning["nested"].(map[string]any)["level"] = "low"
	refChanged := *first
	refChanged.ref = "other/model"
	var nilBinding *ModelBinding
	for name, other := range map[string]*ModelBinding{
		"adapter": &adapterChanged, "limiter": &limiterChanged,
		"config": &configChanged, "ref": &refChanged, "nil": nilBinding,
	} {
		if first.SameRoute(other) || other.SameRoute(first) {
			t.Errorf("%s 不同却被视为同一路由", name)
		}
	}
	if nilBinding.SameRoute(nilBinding) {
		t.Fatal("nil binding 不应被视为有效路由")
	}
}
