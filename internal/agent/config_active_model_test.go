package agent

import (
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/config"
)

func TestNormalizeConfigForPublishRejectsUnknownDefaultModel(t *testing.T) {
	cfg := &config.Config{
		ActiveModel: "missing/model",
		Models: []config.ModelConfig{{
			Provider: "openai", Model: "gpt-4o-mini", BaseURL: "https://api.openai.com/v1", ContextWindow: 128000, MaxOutputTokens: 8192,
		}},
	}

	err := normalizeConfigForPublish(cfg)
	if err == nil {
		t.Fatal("normalizeConfigForPublish() error = nil, want unknown default model rejection")
	}
	if !strings.Contains(err.Error(), "active_model") {
		t.Fatalf("normalizeConfigForPublish() error = %v, want active_model context", err)
	}
}
