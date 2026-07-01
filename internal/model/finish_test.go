package model

import (
	"testing"

	"github.com/alanchenchen/suna/internal/logging"
	"github.com/openai/openai-go/v3/responses"
)

func TestInferFinishShapeClassifiesStreamStats(t *testing.T) {
	tests := []struct {
		name   string
		stats  llmRequestStreamStats
		failed bool
		want   string
	}{
		{name: "failed", failed: true, want: "failed"},
		{name: "text and tool", stats: llmRequestStreamStats{assistantBytes: 1, toolCalls: 1}, want: "text_tool_call"},
		{name: "text", stats: llmRequestStreamStats{assistantBytes: 1}, want: "text"},
		{name: "tool", stats: llmRequestStreamStats{toolCalls: 1}, want: "tool_call"},
		{name: "reasoning only", stats: llmRequestStreamStats{reasoningBytes: 1}, want: "reasoning_only"},
		{name: "empty", want: "empty"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if got := inferLLMFinishShape(tt.stats, tt.failed); got != tt.want {
				t.Fatalf("inferLLMFinishShape() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAddLLMRequestFinishFieldsCopiesNonEmptyValues(t *testing.T) {
	fields := logging.Event{}
	addLLMRequestFinishFields(fields, &FinishInfo{Reason: "end_turn", NativeReason: "stop", Status: "completed", IncompleteReason: "max_output_tokens", StopSequence: "</stop>"})

	checks := map[string]any{
		"finish_reason":        "end_turn",
		"native_finish_reason": "stop",
		"finish_status":        "completed",
		"incomplete_reason":    "max_output_tokens",
		"stop_sequence":        "</stop>",
	}
	for key, want := range checks {
		if got := fields[key]; got != want {
			t.Fatalf("fields[%q] = %#v, want %#v", key, got, want)
		}
	}
}

func TestResponseFinishInfoMapsStatusAndIncompleteReason(t *testing.T) {
	finish := responseFinishInfo(responses.ResponseStatusIncomplete, "max_output_tokens")
	if finish == nil {
		t.Fatal("responseFinishInfo() = nil, want finish info")
	}
	if got, want := finish.Status, "incomplete"; got != want {
		t.Fatalf("Status = %q, want %q", got, want)
	}
	if got, want := finish.Reason, "max_output_tokens"; got != want {
		t.Fatalf("Reason = %q, want %q", got, want)
	}
	if got, want := finish.IncompleteReason, "max_output_tokens"; got != want {
		t.Fatalf("IncompleteReason = %q, want %q", got, want)
	}
}
