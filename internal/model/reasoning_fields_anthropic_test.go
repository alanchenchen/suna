package model

import "testing"

func TestAnthropicReasoningFieldOptionsAcceptsCustomFields(t *testing.T) {
	reasoning := map[string]any{
		"thinking":      map[string]any{"type": "adaptive"},
		"output_config": map[string]any{"effort": "xhigh"},
	}
	if _, err := anthropicReasoningFieldOptions(reasoning, anthropicGeneratedKeys(false)); err != nil {
		t.Fatalf("anthropicReasoningFieldOptions() error = %v", err)
	}
}

func TestAnthropicReasoningFieldOptionsRejectsGeneratedFields(t *testing.T) {
	reasoning := map[string]any{"model": "other"}
	if _, err := anthropicReasoningFieldOptions(reasoning, anthropicGeneratedKeys(false)); err == nil {
		t.Fatal("anthropicReasoningFieldOptions() error = nil, want non-nil")
	}
}

func TestAnthropicGeneratedKeysOnlyProtectsTemperatureWhenGenerated(t *testing.T) {
	withTemperature := anthropicGeneratedKeys(true)
	if !withTemperature["temperature"] {
		t.Fatal("anthropicGeneratedKeys(true)[temperature] = false, want true")
	}
	withoutTemperature := anthropicGeneratedKeys(false)
	if withoutTemperature["temperature"] {
		t.Fatal("anthropicGeneratedKeys(false)[temperature] = true, want false")
	}
}
