package runner

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/tool"
)

type delayedExecutor struct {
	delays map[string]time.Duration
}

func (e delayedExecutor) ExecuteTool(ctx context.Context, call ToolExecution) tool.Result {
	select {
	case <-time.After(e.delays[call.ID]):
	case <-ctx.Done():
		return tool.ErrorResult(ctx.Err().Error())
	}
	return tool.TextResult(call.ID + " done")
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
