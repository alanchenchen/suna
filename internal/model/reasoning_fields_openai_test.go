package model

import "testing"

func TestOpenAIReasoningFieldOptionsAcceptsCustomFields(t *testing.T) {
	fields := map[string]any{"reasoning_effort": "high"}
	if _, err := openAIReasoningFieldOptions(fields, chatGeneratedKeys()); err != nil {
		t.Fatalf("openAIReasoningFieldOptions() error = %v", err)
	}
}

func TestOpenAIReasoningFieldOptionsRejectsGeneratedFields(t *testing.T) {
	fields := map[string]any{"model": "other"}
	if _, err := openAIReasoningFieldOptions(fields, chatGeneratedKeys()); err == nil {
		t.Fatal("openAIReasoningFieldOptions() error = nil, want non-nil")
	}
}
