package model

import (
	"context"
	"strings"
	"testing"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

func BenchmarkReadStreamTextWithIdleLongStream(b *testing.B) {
	chunk := strings.Repeat("abcdefghijklmnopqrstuvwxyz", 256)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ch := make(chan Chunk, 256)
		for j := 0; j < 128; j++ {
			ch <- Chunk{Content: chunk}
		}
		ch <- Chunk{Done: true}
		close(ch)
		got, err := ReadStreamTextWithIdle(context.Background(), ch, time.Second, "")
		if err != nil {
			b.Fatal(err)
		}
		if len(got) != len(chunk)*128 {
			b.Fatalf("len(got) = %d, want %d", len(got), len(chunk)*128)
		}
	}
}

func BenchmarkOpenAIChatToolCallAccumLongArgs(b *testing.B) {
	chunk := strings.Repeat("x", 512)
	parts := make([]openai.ChatCompletionChunkChoiceDeltaToolCall, 128)
	for i := range parts {
		parts[i].Index = int64(0)
		parts[i].Function.Name = "writefile"
		parts[i].Function.Arguments = chunk
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var acc map[int]*chatToolCallAccum
		mergeChatToolDeltas(parts, &acc)
		calls := accumulateChatToolCalls(acc)
		if got, want := len(calls), 1; got != want {
			b.Fatalf("len(calls) = %d, want %d", got, want)
		}
		if got, want := len(calls[0].Arguments), len(chunk)*len(parts); got != want {
			b.Fatalf("len(args) = %d, want %d", got, want)
		}
	}
}

func BenchmarkOpenAIResponsesToolCallAccumLongArgs(b *testing.B) {
	chunk := strings.Repeat("x", 512)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		calls := map[string]*responseToolCall{}
		var order []string
		for j := 0; j < 128; j++ {
			upsertResponseToolCall(calls, &order, "item-1", "", "writefile", chunk, true)
		}
		out := orderedResponseToolCalls(calls, order)
		if got, want := len(out), 1; got != want {
			b.Fatalf("len(out) = %d, want %d", got, want)
		}
		if got, want := len(out[0].Arguments), len(chunk)*128; got != want {
			b.Fatalf("len(args) = %d, want %d", got, want)
		}
	}
}

func TestResponseToolCallDoneReplacesDeltaArguments(t *testing.T) {
	calls := map[string]*responseToolCall{}
	var order []string
	upsertResponseToolCall(calls, &order, "item-1", "", "writefile", "partial", true)
	upsertResponseToolCall(calls, &order, "item-1", "call-1", "writefile", `{"path":"a"}`, false)

	out := orderedResponseToolCalls(calls, order)
	if got, want := len(out), 1; got != want {
		t.Fatalf("len(out) = %d, want %d", got, want)
	}
	if got, want := out[0].ID, "call-1"; got != want {
		t.Fatalf("ID = %q, want %q", got, want)
	}
	if got, want := out[0].Arguments, `{"path":"a"}`; got != want {
		t.Fatalf("Arguments = %q, want %q", got, want)
	}
}

func TestCollectResponseOutputToolCallsUsesBuilderAccumulator(t *testing.T) {
	calls := map[string]*responseToolCall{}
	var order []string
	items := []responses.ResponseOutputItemUnion{{Type: "function_call"}}
	items[0].ID = "item-1"
	items[0].CallID = "call-1"
	items[0].Name = "readfile"
	items[0].Arguments.OfString = `{"path":"a"}`

	collectResponseOutputToolCalls(items, calls, &order)
	out := orderedResponseToolCalls(calls, order)
	if got, want := len(out), 1; got != want {
		t.Fatalf("len(out) = %d, want %d", got, want)
	}
	if got, want := out[0].Arguments, `{"path":"a"}`; got != want {
		t.Fatalf("Arguments = %q, want %q", got, want)
	}
}
