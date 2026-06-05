package model

import (
	"context"
	"strings"
	"testing"
)

func TestAnthropicBuildMessagesGroupsConsecutiveToolResults(t *testing.T) {
	p := NewAnthropicProvider("test-key", "", "claude-test", 0, nil)
	req := &CompletionRequest{Messages: []Message{
		{
			Role:        RoleAssistant,
			TextContent: "",
			ToolCalls: []ToolCall{
				{ID: "call_a", Name: "readfile", Arguments: `{"path":"a"}`},
				{ID: "call_b", Name: "readfile", Arguments: `{"path":"b"}`},
			},
		},
		{Role: RoleTool, ToolCallID: "call_a", TextContent: "result a"},
		{Role: RoleTool, ToolCallID: "call_b", TextContent: "result b"},
		{Role: RoleUser, TextContent: "continue"},
		{Role: RoleTool, ToolCallID: "call_c", TextContent: "result c"},
	}}

	msgs, err := p.buildMessages(context.Background(), req)
	if err != nil {
		t.Fatalf("buildMessages() error = %v", err)
	}
	if got, want := len(msgs), 4; got != want {
		t.Fatalf("len(msgs) = %d, want %d", got, want)
	}

	firstToolResults, err := msgs[1].MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON(first tool results) error = %v", err)
	}
	first := string(firstToolResults)
	if !strings.Contains(first, "call_a") || !strings.Contains(first, "call_b") {
		t.Fatalf("first tool result message = %s, want both call_a and call_b", first)
	}
	if strings.Contains(first, "call_c") {
		t.Fatalf("first tool result message = %s, should not include non-consecutive call_c", first)
	}

	secondToolResultsBytes, err := msgs[3].MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON(second tool results) error = %v", err)
	}
	secondToolResults := string(secondToolResultsBytes)
	if !strings.Contains(secondToolResults, "call_c") {
		t.Fatalf("second tool result message = %s, want call_c", secondToolResults)
	}
}
