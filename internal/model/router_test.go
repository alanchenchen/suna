package model

import (
	"testing"

	"github.com/alanchenchen/suna/internal/config"
)

func TestCreateProviderUsesProtocolNotProviderName(t *testing.T) {
	mc := config.ModelConfig{Provider: "vendor", Protocol: config.ModelProtocolAnthropic, Model: "claude", BaseURL: "https://api.example.com", ContextWindow: 200000, MaxOutputTokens: 8192, APIKey: "sk-test"}
	p, err := createProvider(mc, nil)
	if err != nil {
		t.Fatalf("createProvider() error = %v", err)
	}
	if _, ok := p.(*AnthropicProvider); !ok {
		t.Fatalf("createProvider() = %T, want *AnthropicProvider", p)
	}
}

func TestCreateProviderDefaultsMissingProtocolToOpenAIChat(t *testing.T) {
	mc := config.ModelConfig{Provider: "openai", Model: "gpt", BaseURL: "https://api.example.com/v1", ContextWindow: 128000, MaxOutputTokens: 8192, APIKey: "sk-test"}
	p, err := createProvider(mc, nil)
	if err != nil {
		t.Fatalf("createProvider() error = %v", err)
	}
	if _, ok := p.(*OpenAIChatProvider); !ok {
		t.Fatalf("createProvider() = %T, want *OpenAIChatProvider", p)
	}
}
