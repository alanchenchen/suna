package config

import (
	"testing"

	coreconfig "github.com/alanchenchen/suna/internal/config"
)

func TestProviderFormDefaultsToOpenAIChatWithoutTemplate(t *testing.T) {
	m := &Model{}
	spec := m.ProviderFormSpec(ProviderFormLabels{}, nil)

	if got, want := spec.Values[0], ""; got != want {
		t.Fatalf("provider default = %q, want %q", got, want)
	}
	if got, want := spec.Values[ProviderFormProtocolIndex], string(coreconfig.ModelProtocolOpenAIChat); got != want {
		t.Fatalf("protocol default = %q, want %q", got, want)
	}
}

func TestProviderFormPreservesExistingModelProtocol(t *testing.T) {
	m := &Model{}
	spec := m.ProviderFormSpec(ProviderFormLabels{}, &ModelConfig{
		Provider: "anthropic",
		Protocol: coreconfig.ModelProtocolAnthropic,
		Model:    "claude-sonnet",
	})

	if got, want := spec.Values[ProviderFormProtocolIndex], string(coreconfig.ModelProtocolAnthropic); got != want {
		t.Fatalf("protocol value = %q, want %q", got, want)
	}
}
