package config

import "testing"

func TestGPTReasoningResponsesIncludesAutoSummary(t *testing.T) {
	got := GPTReasoning("openai_responses", "high")
	reasoning, ok := got["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("GPTReasoning()[reasoning] = %#v, want map", got["reasoning"])
	}
	if got, want := reasoning["effort"], "high"; got != want {
		t.Fatalf("reasoning.effort = %#v, want %q", got, want)
	}
	if got, want := reasoning["summary"], "auto"; got != want {
		t.Fatalf("reasoning.summary = %#v, want %q", got, want)
	}
}

func TestGPTReasoningCompatibleDoesNotSetResponsesSummary(t *testing.T) {
	got := GPTReasoning("openai", "low")
	if got, want := got["reasoning_effort"], "low"; got != want {
		t.Fatalf("reasoning_effort = %#v, want %q", got, want)
	}
	if _, ok := got["reasoning"]; ok {
		t.Fatalf("reasoning = %#v, want absent for compatible protocol", got["reasoning"])
	}
}
