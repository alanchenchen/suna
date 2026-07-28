package model

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3/responses"
)

func TestMergeResponseToolCallAggregatesStreamByItemID(t *testing.T) {
	calls := map[string]*responseToolCall{}
	var order []string
	for _, raw := range []string{
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"item_1","call_id":"call_1","name":"askuser","arguments":"","status":"in_progress"}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"item_1","output_index":0,"delta":"{\"question\":"}`,
		`{"type":"response.function_call_arguments.delta","item_id":"item_1","output_index":0,"delta":"\"continue?\"}"}`,
		`{"type":"response.function_call_arguments.done","item_id":"item_1","output_index":0,"name":"askuser","arguments":"{\"question\":\"continue?\"}"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"item_1","call_id":"call_1","name":"askuser","arguments":"{\"question\":\"continue?\"}","status":"completed"}}`,
	} {
		var event responses.ResponseStreamEventUnion
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			t.Fatalf("Unmarshal(%s) error = %v", raw, err)
		}
		mergeResponseToolCall(event, calls, &order)
	}

	// response.completed 的输出快照不应把已处理的 item 再聚合为新调用。
	items := []responses.ResponseOutputItemUnion{{Type: "function_call"}}
	items[0].ID = "item_1"
	items[0].CallID = "call_1"
	items[0].Name = "askuser"
	items[0].Arguments.OfString = `{"question":"continue?"}`
	collectResponseOutputToolCalls(items, calls, &order)

	if got, want := len(calls), 1; got != want {
		t.Fatalf("len(calls) = %d, want %d", got, want)
	}
	if got, want := len(order), 1; got != want {
		t.Fatalf("len(order) = %d, want %d", got, want)
	}
	out := orderedResponseToolCalls(calls, order)
	if got, want := len(out), 1; got != want {
		t.Fatalf("len(tool calls) = %d, want %d", got, want)
	}
	if got, want := out[0], (ToolCall{ID: "call_1", Name: "askuser", Arguments: `{"question":"continue?"}`}); got != want {
		t.Fatalf("tool call = %#v, want %#v", got, want)
	}
}

func TestOrderedResponseToolCallsDeduplicatesCallID(t *testing.T) {
	calls := map[string]*responseToolCall{}
	var order []string
	upsertResponseToolCall(calls, &order, "item_1", "call_1", "readfile", `{"path":"a"}`, false)
	upsertResponseToolCall(calls, &order, "item_2", "call_1", "readfile", `{"path":"b"}`, false)

	out := orderedResponseToolCalls(calls, order)
	if got, want := len(out), 1; got != want {
		t.Fatalf("len(tool calls) = %d, want %d", got, want)
	}
	if got, want := out[0].ID, "call_1"; got != want {
		t.Fatalf("ID = %q, want %q", got, want)
	}
}

func TestCollectResponseOutputToolCallsCompletesMissingStreamFields(t *testing.T) {
	calls := map[string]*responseToolCall{}
	var order []string
	upsertResponseToolCall(calls, &order, "item_1", "", "", `{"path":"a"}`, false)
	items := []responses.ResponseOutputItemUnion{{Type: "function_call"}}
	items[0].ID = "item_1"
	items[0].CallID = "call_1"
	items[0].Name = "readfile"
	items[0].Arguments.OfString = `{"path":"other"}`

	collectResponseOutputToolCalls(items, calls, &order)
	out := orderedResponseToolCalls(calls, order)
	if got, want := len(out), 1; got != want {
		t.Fatalf("len(tool calls) = %d, want %d", got, want)
	}
	if got, want := out[0], (ToolCall{ID: "call_1", Name: "readfile", Arguments: `{"path":"a"}`}); got != want {
		t.Fatalf("tool call = %#v, want %#v", got, want)
	}
}

func TestMergeResponseToolCallKeepsInterleavedCallsSeparate(t *testing.T) {
	calls := map[string]*responseToolCall{}
	var order []string
	for _, raw := range []string{
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"item_1","call_id":"call_1","name":"readfile","arguments":"","status":"in_progress"}}`,
		`{"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"item_2","call_id":"call_2","name":"listdir","arguments":"","status":"in_progress"}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"item_1","output_index":0,"delta":"{\"path\":\"a"}`,
		`{"type":"response.function_call_arguments.delta","item_id":"item_2","output_index":1,"delta":"{\"path\":\"b"}`,
		`{"type":"response.function_call_arguments.done","item_id":"item_2","output_index":1,"name":"listdir","arguments":"{\"path\":\"b\"}"}`,
		`{"type":"response.function_call_arguments.done","item_id":"item_1","output_index":0,"name":"readfile","arguments":"{\"path\":\"a\"}"}`,
	} {
		var event responses.ResponseStreamEventUnion
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			t.Fatalf("Unmarshal(%s) error = %v", raw, err)
		}
		mergeResponseToolCall(event, calls, &order)
	}

	got := orderedResponseToolCalls(calls, order)
	want := []ToolCall{
		{ID: "call_1", Name: "readfile", Arguments: `{"path":"a"}`},
		{ID: "call_2", Name: "listdir", Arguments: `{"path":"b"}`},
	}
	if len(got) != len(want) {
		t.Fatalf("len(tool calls) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("toolCalls[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestOpenAIResponsesPromptCacheKeyIsStableAndScoped(t *testing.T) {
	adapter := &OpenAIResponsesAdapter{cacheNamespace: responseCacheNamespace(AdapterSpec{
		BaseURL: "https://api.example.com/v1",
		ModelID: "example-model",
	})}
	request := CompletionRequest{Purpose: "chat", Invocation: Invocation{SessionScope: SessionScope("session-a")}}
	got := adapter.promptCacheKey(request)
	if got == "" {
		t.Fatal("prompt cache key is empty")
	}
	if retry := adapter.promptCacheKey(request); retry != got {
		t.Fatalf("retry prompt cache key = %q, want %q", retry, got)
	}
	if other := adapter.promptCacheKey(CompletionRequest{Purpose: "compact", Invocation: request.Invocation}); other == got {
		t.Fatalf("compact prompt cache key = %q, must differ from chat key", other)
	}
	otherAdapter := &OpenAIResponsesAdapter{cacheNamespace: responseCacheNamespace(AdapterSpec{
		BaseURL: "https://api.other.example.com/v1",
		ModelID: "example-model",
	})}
	if other := otherAdapter.promptCacheKey(request); other == got {
		t.Fatalf("other endpoint prompt cache key = %q, must differ", other)
	}
	if other := adapter.promptCacheKey(CompletionRequest{Purpose: "chat", Invocation: Invocation{SessionScope: SessionScope("session-b")}}); other == got {
		t.Fatalf("other session prompt cache key = %q, must differ", other)
	}
	if strings.Contains(got, "session-a") {
		t.Fatalf("prompt cache key exposes session ID: %q", got)
	}
}

func TestSessionScopeIsStableAndAnonymous(t *testing.T) {
	got := SessionScope("session-a")
	if got == "" || got == "session-a" {
		t.Fatalf("SessionScope = %q, must be a non-empty anonymous value", got)
	}
	if retry := SessionScope("session-a"); retry != got {
		t.Fatalf("SessionScope retry = %q, want %q", retry, got)
	}
	if other := SessionScope("session-b"); other == got {
		t.Fatalf("different SessionScope = %q, must differ", other)
	}
}
