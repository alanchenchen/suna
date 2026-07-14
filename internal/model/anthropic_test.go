package model

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestAnthropicBuildMessagesGroupsConsecutiveToolResults(t *testing.T) {
	p := NewAnthropicProvider("test-key", "", "claude-test", 128000, 8192, nil)
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

func TestAnthropicAccumulatedToolCallsOrdersByContentBlockIndex(t *testing.T) {
	acc := map[int64]*anthropicToolCallAccum{
		3: {ID: "call_b", Name: "writefile", Arguments: *newStringsBuilder(`{"path":"b"}`)},
		1: {ID: "call_a", Name: "readfile", Arguments: *newStringsBuilder(`{"path":"a"}`)},
	}

	calls := anthropicAccumulatedToolCalls(acc)
	if got, want := len(calls), 2; got != want {
		t.Fatalf("len(calls) = %d, want %d", got, want)
	}
	if got, want := calls[0].ID, "call_a"; got != want {
		t.Fatalf("calls[0].ID = %q, want %q", got, want)
	}
	if got, want := calls[1].ID, "call_b"; got != want {
		t.Fatalf("calls[1].ID = %q, want %q", got, want)
	}
}

func TestAnthropicAccumulatedToolCallsUsesInitialArgumentsAndDefault(t *testing.T) {
	calls := anthropicAccumulatedToolCalls(map[int64]*anthropicToolCallAccum{
		0: {ID: "call_empty", Name: "listdir"},
		1: {ID: "call_initial", Name: "readfile", InitialArguments: `{"path":"a"}`},
	})
	if got, want := len(calls), 2; got != want {
		t.Fatalf("len(calls) = %d, want %d", got, want)
	}
	if got, want := calls[0].Arguments, "{}"; got != want {
		t.Fatalf("calls[0].Arguments = %q, want %q", got, want)
	}
	if got, want := calls[1].Arguments, `{"path":"a"}`; got != want {
		t.Fatalf("calls[1].Arguments = %q, want %q", got, want)
	}
}

func TestAnthropicUsageIncludesCacheTokensInInputTotal(t *testing.T) {
	u := anthropicUsage(10, 3, 4, 5)
	if got, want := u.InputTokens, 17; got != want {
		t.Fatalf("InputTokens = %d, want %d", got, want)
	}
	if got, want := u.CachedTokens, 4; got != want {
		t.Fatalf("CachedTokens = %d, want %d", got, want)
	}
	if got, want := u.OutputTokens, 5; got != want {
		t.Fatalf("OutputTokens = %d, want %d", got, want)
	}
	if got, want := u.TotalTokens, 22; got != want {
		t.Fatalf("TotalTokens = %d, want %d", got, want)
	}
}

func TestAnthropicBuildAssistantToolUseInputIsObject(t *testing.T) {
	p := NewAnthropicProvider("test-key", "", "claude-test", 128000, 8192, nil)
	blocks, err := p.buildAssistantBlocks(context.Background(), Message{
		Role: RoleAssistant,
		ToolCalls: []ToolCall{
			{ID: "call_a", Name: "readfile", Arguments: `{"path":"a"}`},
		},
	})
	if err != nil {
		t.Fatalf("buildAssistantBlocks() error = %v", err)
	}
	b, err := blocks[0].MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}
	var got struct {
		Input any `json:"input"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if _, ok := got.Input.(map[string]any); !ok {
		t.Fatalf("input type = %T, want object; json = %s", got.Input, string(b))
	}
}

func newStringsBuilder(s string) *strings.Builder {
	var b strings.Builder
	b.WriteString(s)
	return &b
}

func TestMergeAnthropicUsagePreservesStartInputForOutputOnlyDelta(t *testing.T) {
	start := anthropicUsage(10, 3, 4, 0)
	delta := anthropicUsage(0, 0, 0, 5)
	merged := mergeAnthropicUsage(start, delta)
	if got, want := merged.InputTokens, 17; got != want {
		t.Fatalf("InputTokens = %d, want %d", got, want)
	}
	if got, want := merged.CachedTokens, 4; got != want {
		t.Fatalf("CachedTokens = %d, want %d", got, want)
	}
	if got, want := merged.OutputTokens, 5; got != want {
		t.Fatalf("OutputTokens = %d, want %d", got, want)
	}
	if got, want := merged.TotalTokens, 22; got != want {
		t.Fatalf("TotalTokens = %d, want %d", got, want)
	}
}

func TestAnthropicToolInputSchemaPreservesRequiredAndProperties(t *testing.T) {
	p := NewAnthropicProvider("test-key", "", "claude-test", 128000, 8192, nil)
	tools := p.buildTools([]ToolDef{{
		Name:        "readfile",
		Description: "read a file",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":     map[string]any{"type": "string"},
				"encoding": map[string]any{"type": "string", "enum": []string{"text", "base64"}},
			},
			"required":             []string{"path"},
			"additionalProperties": false,
		},
	}})
	if got, want := len(tools), 1; got != want {
		t.Fatalf("len(tools) = %d, want %d", got, want)
	}
	b, err := tools[0].MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}
	var got struct {
		InputSchema struct {
			Type                 string         `json:"type"`
			Properties           map[string]any `json:"properties"`
			Required             []string       `json:"required"`
			AdditionalProperties bool           `json:"additionalProperties"`
		} `json:"input_schema"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v; json = %s", err, string(b))
	}
	if got.InputSchema.Type != "object" {
		t.Fatalf("input_schema.type = %q, want object; json = %s", got.InputSchema.Type, string(b))
	}
	if _, ok := got.InputSchema.Properties["path"]; !ok {
		t.Fatalf("properties missing path; json = %s", string(b))
	}
	if _, nested := got.InputSchema.Properties["properties"]; nested {
		t.Fatalf("properties contains nested root schema; json = %s", string(b))
	}
	if got.InputSchema.Required == nil || len(got.InputSchema.Required) != 1 || got.InputSchema.Required[0] != "path" {
		t.Fatalf("required = %v, want [path]; json = %s", got.InputSchema.Required, string(b))
	}
	if got.InputSchema.AdditionalProperties {
		t.Fatalf("additionalProperties = true, want false; json = %s", string(b))
	}
}
