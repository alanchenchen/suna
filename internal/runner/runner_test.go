package runner

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/tools"
)

type delayedExecutor struct {
	delays map[string]time.Duration
}

func (e delayedExecutor) ExecuteTool(ctx context.Context, call ToolExecution) tools.Result {
	select {
	case <-time.After(e.delays[call.ID]):
	case <-ctx.Done():
		return tools.ErrorResult(ctx.Err().Error())
	}
	return tools.TextResult(call.ID + " done")
}

func TestExecuteToolCallsNotifiesResultsByCompletionAndReturnsOriginalOrder(t *testing.T) {
	r := &Runner{Executor: delayedExecutor{delays: map[string]time.Duration{
		"a": 80 * time.Millisecond,
		"b": 10 * time.Millisecond,
		"c": 30 * time.Millisecond,
	}}}
	calls := []preparedToolCall{
		{tc: model.ToolCall{ID: "a", Name: "tool_a"}},
		{tc: model.ToolCall{ID: "b", Name: "tool_b"}},
		{tc: model.ToolCall{ID: "c", Name: "tool_c"}},
	}

	var mu sync.Mutex
	var notified []string
	results := r.executeToolCalls(context.Background(), calls, nil, func(res toolExecResult) {
		mu.Lock()
		defer mu.Unlock()
		notified = append(notified, res.tc.ID)
	})

	gotResults := make([]string, 0, len(results))
	for _, res := range results {
		gotResults = append(gotResults, res.tc.ID)
	}
	if want := []string{"a", "b", "c"}; !reflect.DeepEqual(gotResults, want) {
		t.Fatalf("executeToolCalls result order = %v, want %v", gotResults, want)
	}
	if want := []string{"b", "c", "a"}; !reflect.DeepEqual(notified, want) {
		t.Fatalf("executeToolCalls notification order = %v, want completion order %v", notified, want)
	}
}

func TestReadStreamExtendsTimeoutAfterReasoningChunk(t *testing.T) {
	t.Parallel()
	ch := make(chan model.Chunk, 2)
	ch <- model.Chunk{ReasoningContent: "thinking"}
	go func() {
		time.Sleep(20 * time.Millisecond)
		ch <- model.Chunk{Done: true}
		close(ch)
	}()

	_, _, _, err, _ := (&Runner{}).readStream(context.Background(), ch, 5*time.Millisecond, true, Request{})
	if err != nil {
		t.Fatalf("readStream() error = %v, want nil", err)
	}
}

func TestReadStreamKeepsChatTimeoutWithoutReasoningChunk(t *testing.T) {
	t.Parallel()
	ch := make(chan model.Chunk, 1)
	ch <- model.Chunk{Content: "hello"}

	_, _, _, err, visible := (&Runner{}).readStream(context.Background(), ch, 5*time.Millisecond, true, Request{})
	if err == nil {
		t.Fatal("readStream() error = nil, want idle timeout")
	}
	if !visible {
		t.Fatal("visible = false after content chunk, want true")
	}
}

func TestReadStreamReportsNoVisibleOutputBeforeInitialError(t *testing.T) {
	t.Parallel()
	ch := make(chan model.Chunk, 1)
	ch <- model.Chunk{Error: &model.ModelError{Kind: model.ModelErrorHTTP, StatusCode: 503, Message: "unavailable"}}
	close(ch)

	_, _, _, err, visible := (&Runner{}).readStream(context.Background(), ch, time.Second, false, Request{})
	if err == nil {
		t.Fatal("readStream() error = nil, want model error")
	}
	if visible {
		t.Fatal("visible = true before content, want false")
	}
}

func TestRetryableModelRequestErrorUsesStructuredStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "rate limit", err: &model.ModelError{Kind: model.ModelErrorHTTP, StatusCode: 429}, want: true},
		{name: "server unavailable", err: &model.ModelError{Kind: model.ModelErrorHTTP, StatusCode: 503}, want: true},
		{name: "bad request", err: &model.ModelError{Kind: model.ModelErrorHTTP, StatusCode: 400}, want: false},
		{name: "network", err: &model.ModelError{Kind: model.ModelErrorNetwork, Message: "temporary network failure"}, want: true},
		{name: "unknown", err: &model.ModelError{Kind: model.ModelErrorUnknown, Message: "temporarily unavailable"}, want: false},
	}
	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if got := retryableModelRequestError(tt.err); got != tt.want {
				t.Fatalf("retryableModelRequestError() = %v, want %v", got, tt.want)
			}
		})
	}
}
