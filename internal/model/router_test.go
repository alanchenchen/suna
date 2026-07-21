package model

import (
	"errors"
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/config"
)

func TestRouterBindReturnsStructuredModelNotFoundError(t *testing.T) {
	cfg := &config.Config{Models: []config.ModelConfig{{
		Provider: "vendor", Model: "model", BaseURL: "https://api.example.com/v1", ContextWindow: 128000, MaxOutputTokens: 8192, APIKey: "test-api-key",
	}}}
	router, err := NewRouter(cfg, nil)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	_, err = router.Bind("vendor/missing")
	var bindingErr *BindingError
	if !errors.As(err, &bindingErr) {
		t.Fatalf("Bind() error = %T %v, want *BindingError", err, err)
	}
	if bindingErr.Kind != BindingErrorModelNotFound || bindingErr.Ref != "vendor/missing" {
		t.Fatalf("binding error = %#v, want missing model ref", bindingErr)
	}
}

func TestRouterAndBindingDeepCopyModelConfig(t *testing.T) {
	reasoning := map[string]any{
		"nested": map[string]any{"values": []any{map[string]any{"level": "original"}}},
	}
	cfg := &config.Config{Models: []config.ModelConfig{{
		Provider: "vendor", Model: "model", BaseURL: "https://api.example.com/v1", ContextWindow: 128000, MaxOutputTokens: 8192, APIKey: "test-api-key",
		Strengths: []string{"code"}, SubtaskFor: []string{"vendor/**"}, Reasoning: reasoning,
	}}}
	router, err := NewRouter(cfg, nil)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	// 修改构造 Router 后的源配置不得影响 Router。
	cfg.Models[0].Strengths[0] = "changed"
	cfg.Models[0].SubtaskFor[0] = "changed/**"
	reasoning["nested"].(map[string]any)["values"].([]any)[0].(map[string]any)["level"] = "changed"

	binding, err := router.Bind("vendor/model")
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	got := binding.Config()
	if got.Strengths[0] != "code" || got.SubtaskFor[0] != "vendor/**" {
		t.Fatalf("binding config = %#v, Router retained source slice mutation", got)
	}
	if level := got.Reasoning["nested"].(map[string]any)["values"].([]any)[0].(map[string]any)["level"]; level != "original" {
		t.Fatalf("binding reasoning level = %q, want original", level)
	}

	// Config() 返回值同样必须是深副本，不能反向污染 Binding。
	got.Reasoning["nested"].(map[string]any)["values"].([]any)[0].(map[string]any)["level"] = "mutated-binding-copy"
	got.Strengths[0] = "mutated-binding-copy"
	again := binding.Config()
	if level := again.Reasoning["nested"].(map[string]any)["values"].([]any)[0].(map[string]any)["level"]; level != "original" {
		t.Fatalf("subsequent Config() reasoning level = %q, want original", level)
	}
	if again.Strengths[0] != "code" {
		t.Fatalf("subsequent Config() strengths = %#v, want immutable snapshot", again.Strengths)
	}

	modelConfig, err := router.ModelConfig("vendor/model")
	if err != nil {
		t.Fatal(err)
	}
	modelConfig.Reasoning["nested"].(map[string]any)["values"].([]any)[0].(map[string]any)["level"] = "mutated-router-copy"
	if level := binding.Config().Reasoning["nested"].(map[string]any)["values"].([]any)[0].(map[string]any)["level"]; level != "original" {
		t.Fatalf("router ModelConfig() mutated binding snapshot: %q", level)
	}
}

func TestCreateProviderUsesProtocolNotProviderName(t *testing.T) {
	mc := config.ModelConfig{Provider: "vendor", Protocol: config.ModelProtocolAnthropic, Model: "claude", BaseURL: "https://api.example.com", ContextWindow: 200000, MaxOutputTokens: 8192, APIKey: "test-api-key"}
	p, err := createProvider(mc, nil)
	if err != nil {
		t.Fatalf("createProvider() error = %v", err)
	}
	if _, ok := p.(*AnthropicProvider); !ok {
		t.Fatalf("createProvider() = %T, want *AnthropicProvider", p)
	}
}

func TestCreateProviderDefaultsMissingProtocolToOpenAIChat(t *testing.T) {
	mc := config.ModelConfig{Provider: "openai", Model: "gpt", BaseURL: "https://api.example.com/v1", ContextWindow: 128000, MaxOutputTokens: 8192, APIKey: "test-api-key"}
	p, err := createProvider(mc, nil)
	if err != nil {
		t.Fatalf("createProvider() error = %v", err)
	}
	if _, ok := p.(*OpenAIChatProvider); !ok {
		t.Fatalf("createProvider() = %T, want *OpenAIChatProvider", p)
	}
}

func TestValidateToolResultPairsRejectsOrphanToolResult(t *testing.T) {
	err := validateToolResultPairs([]Message{{Role: RoleTool, ToolCallID: "call-1", TextContent: "result"}})
	if err == nil {
		t.Fatal("validateToolResultPairs() error = nil, want orphan tool result error")
	}
	if !strings.Contains(err.Error(), "orphan tool result") || !strings.Contains(err.Error(), "call-1") {
		t.Fatalf("validateToolResultPairs() error = %v, want orphan call_id detail", err)
	}
}

func TestValidateToolResultPairsAllowsMatchedToolResult(t *testing.T) {
	err := validateToolResultPairs([]Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call-1", Name: "readfile", Arguments: `{"path":"a"}`}}},
		{Role: RoleTool, ToolCallID: "call-1", TextContent: "result"},
	})
	if err != nil {
		t.Fatalf("validateToolResultPairs() error = %v, want nil", err)
	}
}
