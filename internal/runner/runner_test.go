package runner

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/memory"
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
		ch <- model.Chunk{Done: true, Finish: &model.FinishInfo{Reason: "stop"}}
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

func TestDefaultRetryPolicyUsesTimeBudgetAndBackoff(t *testing.T) {
	p := DefaultRetryPolicy()
	if p.Budget != 2*time.Minute || p.InitialDelay != 2*time.Second || p.MaxDelay != 30*time.Second || p.JitterRatio != 0.2 {
		t.Fatalf("DefaultRetryPolicy() = %#v", p)
	}
	for i := 1; i <= 6; i++ {
		delay := p.delay(i)
		if delay <= 0 || delay > p.MaxDelay {
			t.Fatalf("delay(%d) = %s, want within (0,%s]", i, delay, p.MaxDelay)
		}
	}
}

func TestRetryableModelRequestErrorUsesStructuredStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "request timeout", err: &model.ModelError{Kind: model.ModelErrorHTTP, StatusCode: 408}, want: true},
		{name: "too early", err: &model.ModelError{Kind: model.ModelErrorHTTP, StatusCode: 425}, want: true},
		{name: "rate limit", err: &model.ModelError{Kind: model.ModelErrorHTTP, StatusCode: 429}, want: true},
		{name: "server unavailable", err: &model.ModelError{Kind: model.ModelErrorHTTP, StatusCode: 503}, want: true},
		{name: "cloudflare gateway", err: &model.ModelError{Kind: model.ModelErrorHTTP, StatusCode: 522}, want: true},
		{name: "overloaded", err: &model.ModelError{Kind: model.ModelErrorHTTP, StatusCode: 529}, want: true},
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

func TestReadStreamRequiresCompleteProviderFinish(t *testing.T) {
	cases := []struct {
		name   string
		finish *model.FinishInfo
		done   bool
		wantOK bool
	}{
		{name: "chat stop", finish: &model.FinishInfo{Reason: "stop"}, done: true, wantOK: true},
		{name: "chat tool calls", finish: &model.FinishInfo{Reason: "tool_calls"}, done: true, wantOK: true},
		{name: "anthropic end turn", finish: &model.FinishInfo{Reason: "end_turn"}, done: true, wantOK: true},
		{name: "anthropic stop sequence", finish: &model.FinishInfo{Reason: "stop_sequence", StopSequence: "</answer>"}, done: true, wantOK: true},
		{name: "anthropic tool use", finish: &model.FinishInfo{Reason: "tool_use"}, done: true, wantOK: true},
		{name: "anthropic refusal", finish: &model.FinishInfo{Reason: "refusal"}, done: true, wantOK: true},
		{name: "responses completed", finish: &model.FinishInfo{Status: "completed"}, done: true, wantOK: true},
		{name: "missing finish", done: true},
		{name: "empty finish", finish: &model.FinishInfo{}, done: true},
		{name: "closed without done", finish: &model.FinishInfo{Reason: "stop"}},
		{name: "chat truncated", finish: &model.FinishInfo{Reason: "length"}, done: true},
		{name: "anthropic truncated", finish: &model.FinishInfo{Reason: "max_tokens"}, done: true},
		{name: "anthropic pause is not final", finish: &model.FinishInfo{Reason: "pause_turn"}, done: true},
		{name: "responses incomplete", finish: &model.FinishInfo{Status: "incomplete", IncompleteReason: "max_output_tokens"}, done: true},
		{name: "responses failed", finish: &model.FinishInfo{Status: "failed"}, done: true},
		{name: "unknown reason", finish: &model.FinishInfo{Reason: "unknown"}, done: true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ch := make(chan model.Chunk, 2)
			ch <- model.Chunk{Content: "answer"}
			ch <- model.Chunk{Done: tt.done, Finish: tt.finish}
			close(ch)
			content, _, _, err, visible := (&Runner{}).readStream(context.Background(), ch, time.Second, false, Request{})
			if (err == nil) != tt.wantOK || content != "answer" || !visible {
				t.Fatalf("readStream = %q/%v/%v, want answer/success=%v/visible", content, err, visible, tt.wantOK)
			}
		})
	}
}

func TestRunnerValidatesToolArgumentsBeforeCheckpointOrExecution(t *testing.T) {
	cases := []struct {
		name      string
		arguments string
		wantOK    bool
	}{
		{name: "empty object", arguments: `{}`, wantOK: true},
		{name: "object with whitespace", arguments: " \n {\"value\":\"A\"} \t", wantOK: true},
		{name: "nested object", arguments: `{"value":{"items":[1,true,null]}}`, wantOK: true},
		{name: "truncated object", arguments: `{"value":`},
		{name: "missing arguments", arguments: ""},
		{name: "null is not an object", arguments: `null`},
		{name: "array is not an object", arguments: `[]`},
		{name: "multiple values", arguments: `{} {}`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			h := newCheckpointHarness(t, false)
			raw, err := json.Marshal(tt.arguments)
			if err != nil {
				t.Fatal(err)
			}
			response := strings.Replace(checkpointToolSSE, `"arguments":"{\"value\":\"A\"}"`, `"arguments":`+string(raw), 1)
			if response == checkpointToolSSE && tt.arguments != `{"value":"A"}` {
				t.Fatal("tool argument fixture was not replaced")
			}
			h.server.setResponse(response)
			checkpoints := 0
			h.runner.Hooks.Checkpoint = func(context.Context) error { checkpoints++; return nil }
			h.runner.Hooks.OnAssistantSaved = nil
			h.request.MaxTurns = 1
			h.start()
			done := h.finish(t)
			if tt.wantOK {
				if done.err == nil || done.err.Error() != "max turns exceeded (1)" || checkpoints != 3 || len(h.executor.snapshot()) != 2 {
					t.Fatalf("Run error=%v checkpoints=%d calls=%+v, want one completed tool turn", done.err, checkpoints, h.executor.snapshot())
				}
			} else if done.err == nil || done.err.Error() != "model returned an incomplete tool declaration" || checkpoints != 0 || len(h.executor.snapshot()) != 0 || h.working.Len() != 1 {
				t.Fatalf("Run error=%v checkpoints=%d calls=%+v history=%+v, want rejection before save/execution", done.err, checkpoints, h.executor.snapshot(), h.working.Messages())
			}
			if len(h.server.snapshot()) != 1 {
				t.Fatalf("physical requests = %d, want 1", len(h.server.snapshot()))
			}
		})
	}
}

func TestRunnerSavesChangedImageInjectionAndStopsOnSaveFailure(t *testing.T) {
	h := newCheckpointHarness(t, true)
	image := imageBlock("example.png")
	h.executor.results = map[string]tools.Result{
		"call-a": {Content: "result-call-a", Images: []model.ContentBlock{image}},
	}
	h.start()
	declaration := h.nextCheckpoint(t)
	assertCheckpointHistory(t, declaration.messages, nil, false)
	declaration.reply <- nil
	assertCheckpointStarted(t, h)
	close(h.executor.gates["call-b"])
	first := h.nextCheckpoint(t)
	assertCheckpointHistory(t, first.messages, []string{"call-b"}, false)
	first.reply <- nil
	h.nextResult(t)
	close(h.executor.gates["call-a"])
	second := h.nextCheckpoint(t)
	if len(second.messages) != 4 {
		t.Fatalf("result checkpoint = %+v, want declaration and two real results", second.messages)
	}
	tool := second.messages[2]
	wantReference := memory.ImageReference(*image.Media)
	if tool.ToolCallID != "call-a" || !strings.Contains(tool.Text(), wantReference) {
		t.Fatalf("image result = %+v, want source %q before image injection", tool, wantReference)
	}
	second.reply <- nil
	h.nextResult(t)
	injection := h.nextCheckpoint(t)
	last := injection.messages[len(injection.messages)-1]
	if len(injection.messages) != 5 || last.Role != model.RoleUser || last.Origin != memory.OriginTool || len(last.Content) != 1 || last.Content[0].Type != model.ContentImage {
		t.Fatalf("injected history = %+v, want new tool-origin image message", injection.messages)
	}
	failure := errors.New("image checkpoint failed")
	injection.reply <- failure
	done := h.finish(t)
	if !errors.Is(done.err, failure) || !done.result.HadToolCall || len(h.executor.snapshot()) != 2 || len(h.server.snapshot()) != 1 {
		t.Fatalf("Run = %+v/%v calls=%d requests=%d, want save failure without new work", done.result, done.err, len(h.executor.snapshot()), len(h.server.snapshot()))
	}
	if attempts, saved := h.store.counts(); attempts != 4 || saved != 3 {
		t.Fatalf("checkpoint attempts=%d saved=%d, want 4/3", attempts, saved)
	}
}

func TestInjectToolImagesReportsOnlyRealChanges(t *testing.T) {
	working := memory.NewWorkingMemory()
	r := &Runner{toolImages: []model.ContentBlock{{Type: model.ContentText, Text: "not an image"}}}
	if r.injectToolImages(working) || working.Len() != 0 || len(r.toolImages) != 0 {
		t.Fatalf("invalid image batch changed history or remained queued: messages=%+v images=%+v", working.Messages(), r.toolImages)
	}
	r.toolImages = []model.ContentBlock{imageBlock("example.png")}
	if !r.injectToolImages(working) || working.Len() != 1 {
		t.Fatal("real image did not report a history change")
	}
	if r.injectToolImages(working) || working.Len() != 1 {
		t.Fatal("empty batch reported a change or reinjected an image")
	}
}

func TestRunnerRejectsIncompleteStreamBeforeCheckpoint(t *testing.T) {
	h := newCheckpointHarness(t, false)
	h.server.setResponse(strings.Replace(checkpointTextSSE, `"finish_reason":"stop"`, `"finish_reason":"length"`, 1))
	checkpoints := 0
	h.runner.Hooks.Checkpoint = func(context.Context) error { checkpoints++; return nil }
	h.start()
	done := h.finish(t)
	if done.err == nil || checkpoints != 0 || h.working.Len() != 1 || len(h.executor.snapshot()) != 0 {
		t.Fatalf("Run error=%v checkpoints=%d history=%+v calls=%+v, want incomplete response not saved", done.err, checkpoints, h.working.Messages(), h.executor.snapshot())
	}
	if records := h.server.snapshot(); len(records) != 1 {
		t.Fatalf("requests = %d, want no retry after visible incomplete output", len(records))
	}
}

func TestEmptyCompleteResponseSavesConsumptionWithoutEmptyAssistant(t *testing.T) {
	h := newCheckpointHarness(t, false)
	h.server.setResponse(strings.Replace(checkpointTextSSE, `"content":"complete"`, `"content":""`, 1))
	h.runner.Hooks.OnAssistantSaved = nil
	saves := 0
	h.runner.Hooks.Checkpoint = func(context.Context) error {
		saves++
		messages := h.working.Messages()
		if len(messages) != 1 || !messages[0].Consumed {
			t.Fatalf("checkpoint history = %+v, want consumed user only", messages)
		}
		return nil
	}
	h.start()
	if done := h.finish(t); done.err != nil || saves != 1 || h.working.Len() != 1 {
		t.Fatalf("Run error = %v, saves = %d, messages = %d", done.err, saves, h.working.Len())
	}
}
