package model

import (
	"encoding/json"
	"testing"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

func TestReasoningFieldsMergeIntoRequestBody(t *testing.T) {
	body := map[string]any{"model": "m"}
	reasoning := map[string]any{
		"reasoning_effort": "high",
		"thinking":         map[string]any{"type": "enabled"},
	}

	if err := mergeReasoningFields(body, reasoning); err != nil {
		t.Fatalf("mergeReasoningFields() error = %v", err)
	}
	if got := body["reasoning_effort"]; got != "high" {
		t.Fatalf("body[reasoning_effort] = %#v, want %q", got, "high")
	}
	thinking, ok := body["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("body[thinking] = %#v, want map", body["thinking"])
	}
	if got := thinking["type"]; got != "enabled" {
		t.Fatalf("body[thinking][type] = %#v, want %q", got, "enabled")
	}
}

func TestReasoningFieldsRejectConflictingKeys(t *testing.T) {
	body := map[string]any{"model": "m"}
	if err := mergeReasoningFields(body, map[string]any{"model": "other"}); err == nil {
		t.Fatalf("mergeReasoningFields() error = nil, want non-nil")
	}
}

func TestChatReasoningContentReadsExtraField(t *testing.T) {
	var chunk openai.ChatCompletionChunk
	raw := []byte(`{"choices":[{"delta":{"reasoning_content":"thinking","content":""}}]}`)
	if err := json.Unmarshal(raw, &chunk); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got := len(chunk.Choices); got != 1 {
		t.Fatalf("len(Choices) = %d, want %d", got, 1)
	}
	if got := chatReasoningContent(chunk.Choices[0].Delta); got != "thinking" {
		t.Fatalf("chatReasoningContent() = %q, want %q", got, "thinking")
	}
}

func TestChatReasoningContentReadsMiniMaxReasoningDetails(t *testing.T) {
	var chunk openai.ChatCompletionChunk
	raw := []byte(`{"choices":[{"delta":{"reasoning_details":[{"type":"reasoning.text","text":"think-1"},{"type":"reasoning.text","text":"think-2"}],"content":""}}]}`)
	if err := json.Unmarshal(raw, &chunk); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got := chatReasoningContent(chunk.Choices[0].Delta); got != "think-1think-2" {
		t.Fatalf("chatReasoningContent() = %q, want %q", got, "think-1think-2")
	}
}

func TestFloat64PtrPreservesZero(t *testing.T) {
	value := Float64Ptr(0)
	if value == nil {
		t.Fatal("Float64Ptr(0) = nil, want non-nil")
	}
	if got, want := *value, 0.0; got != want {
		t.Fatalf("*Float64Ptr(0) = %v, want %v", got, want)
	}
}

func TestFloat64PtrPreservesNonZero(t *testing.T) {
	value := Float64Ptr(0.7)
	if value == nil {
		t.Fatal("Float64Ptr(0.7) = nil, want non-nil")
	}
	if got, want := *value, 0.7; got != want {
		t.Fatalf("*Float64Ptr(0.7) = %v, want %v", got, want)
	}
}
func TestResponseReasoningContentReadsDeltas(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "reasoning text",
			raw:  `{"type":"response.reasoning_text.delta","delta":"thinking","item_id":"rs_1","output_index":0,"content_index":0,"sequence_number":1}`,
			want: "thinking",
		},
		{
			name: "reasoning summary",
			raw:  `{"type":"response.reasoning_summary_text.delta","delta":"summary","item_id":"rs_1","output_index":0,"summary_index":0,"sequence_number":1}`,
			want: "summary",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			var event responses.ResponseStreamEventUnion
			if err := json.Unmarshal([]byte(tt.raw), &event); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			if got := responseReasoningContent(event); got != tt.want {
				t.Fatalf("responseReasoningContent() = %q, want %q", got, tt.want)
			}
		})
	}
}
