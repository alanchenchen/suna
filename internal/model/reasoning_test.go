package model

import (
	"encoding/json"
	"testing"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

func TestMergeReasoningFields(t *testing.T) {
	body := map[string]any{"model": "m"}
	reasoning := map[string]any{
		"reasoning_effort": "high",
		"thinking":         map[string]any{"type": "enabled"},
	}
	if err := mergeReasoningFields(body, reasoning); err != nil {
		t.Fatalf("mergeReasoningFields error: %v", err)
	}
	if body["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort not merged: %#v", body)
	}
	thinking, ok := body["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" {
		t.Fatalf("thinking not preserved: %#v", body["thinking"])
	}
}

func TestMergeReasoningFieldsRejectsConflict(t *testing.T) {
	body := map[string]any{"model": "m"}
	if err := mergeReasoningFields(body, map[string]any{"model": "other"}); err == nil {
		t.Fatalf("mergeReasoningFields conflict succeeded, want error")
	}
}

func TestChatReasoningContentReadsExtraField(t *testing.T) {
	var chunk openai.ChatCompletionChunk
	raw := []byte(`{"choices":[{"delta":{"reasoning_content":"thinking","content":""}}]}`)
	if err := json.Unmarshal(raw, &chunk); err != nil {
		t.Fatalf("unmarshal chunk: %v", err)
	}
	if len(chunk.Choices) != 1 {
		t.Fatalf("choices = %d", len(chunk.Choices))
	}
	if got := chatReasoningContent(chunk.Choices[0].Delta); got != "thinking" {
		t.Fatalf("reasoning_content = %q", got)
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
		t.Run(tt.name, func(t *testing.T) {
			var event responses.ResponseStreamEventUnion
			if err := json.Unmarshal([]byte(tt.raw), &event); err != nil {
				t.Fatalf("unmarshal event: %v", err)
			}
			if got := responseReasoningContent(event); got != tt.want {
				t.Fatalf("response reasoning = %q, want %q", got, tt.want)
			}
		})
	}
}
