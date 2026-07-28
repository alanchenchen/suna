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
