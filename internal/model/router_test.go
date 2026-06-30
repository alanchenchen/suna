package model

import (
	"strings"
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
