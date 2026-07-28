package model

import (
	"testing"

	"github.com/alanchenchen/suna/internal/config"
)

func TestBuiltinAdapterRegistryCreatesEachSupportedProtocol(t *testing.T) {
	tests := []struct {
		name     string
		protocol config.ModelProtocol
		wantType any
	}{
		{name: "openai responses", protocol: config.ModelProtocolOpenAIResponses, wantType: &OpenAIResponsesAdapter{}},
		{name: "openai chat", protocol: config.ModelProtocolOpenAIChat, wantType: &OpenAIChatAdapter{}},
		{name: "anthropic", protocol: config.ModelProtocolAnthropic, wantType: &AnthropicAdapter{}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			spec := testAdapterSpec("example-model")
			spec.Protocol = tt.protocol
			adapter, err := builtinAdapterRegistry().Create(spec, AdapterDependencies{})
			if err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			switch tt.wantType.(type) {
			case *OpenAIResponsesAdapter:
				if _, ok := adapter.(*OpenAIResponsesAdapter); !ok {
					t.Fatalf("adapter = %T, want *OpenAIResponsesAdapter", adapter)
				}
			case *OpenAIChatAdapter:
				if _, ok := adapter.(*OpenAIChatAdapter); !ok {
					t.Fatalf("adapter = %T, want *OpenAIChatAdapter", adapter)
				}
			case *AnthropicAdapter:
				if _, ok := adapter.(*AnthropicAdapter); !ok {
					t.Fatalf("adapter = %T, want *AnthropicAdapter", adapter)
				}
			}
		})
	}
}

func TestAdapterRegistryRejectsUnsupportedProtocol(t *testing.T) {
	spec := testAdapterSpec("example-model")
	spec.Protocol = config.ModelProtocol("unknown")
	_, err := builtinAdapterRegistry().Create(spec, AdapterDependencies{})
	if err == nil {
		t.Fatal("Create() error = nil, want unsupported protocol error")
	}
}
