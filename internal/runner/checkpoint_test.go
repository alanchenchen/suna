package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/tools"
)

// 首轮一次声明两个完整调用，次轮返回文字；两轮均带正常的结束原因。
const checkpointToolSSE = `data: {"id":"local","object":"chat.completion.chunk","created":1,"model":"offline","choices":[{"index":0,"delta":{"role":"assistant","content":"prepare","tool_calls":[{"index":0,"id":"call-a","type":"function","function":{"name":"alpha","arguments":"{\"value\":\"A\"}"}},{"index":1,"id":"call-b","type":"function","function":{"name":"beta","arguments":"{\"value\":\"B\"}"}}]},"finish_reason":"tool_calls"}]}` + "\n\ndata: [DONE]\n\n"

const checkpointTextSSE = `data: {"id":"local","object":"chat.completion.chunk","created":1,"model":"offline","choices":[{"index":0,"delta":{"role":"assistant","content":"complete"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"

type checkpointWireCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type checkpointWireMessage struct {
	Role       string               `json:"role"`
	Content    json.RawMessage      `json:"content"`
	ToolCallID string               `json:"tool_call_id"`
	ToolCalls  []checkpointWireCall `json:"tool_calls"`
}

type checkpointWireRequest struct {
	Model    string                  `json:"model"`
	Messages []checkpointWireMessage `json:"messages"`
	Tools    []struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	} `json:"tools"`
}

type checkpointHTTPRecord struct {
	method    string
	request   checkpointWireRequest
	decodeErr error
	saved     []memory.Message
}

type checkpointHTTPServer struct {
	mu       sync.Mutex
	records  []checkpointHTTPRecord
	store    *checkpointStore
	response string
}

func (s *checkpointHTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var request checkpointWireRequest
	err := json.NewDecoder(r.Body).Decode(&request)
	record := checkpointHTTPRecord{method: r.Method, request: request, decodeErr: err, saved: s.store.latest()}
	s.mu.Lock()
	s.records = append(s.records, record)
	n := len(s.records)
	response := s.response
	s.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	if response != "" {
		_, _ = fmt.Fprint(w, response)
		return
	}
	if n == 1 {
		_, _ = fmt.Fprint(w, checkpointToolSSE)
		return
	}
	_, _ = fmt.Fprint(w, checkpointTextSSE)
}

func (s *checkpointHTTPServer) setResponse(response string) {
	s.mu.Lock()
	s.response = response
	s.mu.Unlock()
}

func (s *checkpointHTTPServer) snapshot() []checkpointHTTPRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]checkpointHTTPRecord(nil), s.records...)
}

type checkpointAttempt struct {
	messages []memory.Message
	reply    chan error
}

type checkpointStore struct {
	mu        sync.Mutex
	attempts  int
	snapshots [][]memory.Message
}

func (s *checkpointStore) checkpoint(ctx context.Context, working *memory.WorkingMemory, entered chan<- checkpointAttempt) error {
	attempt := checkpointAttempt{messages: working.Messages(), reply: make(chan error, 1)}
	s.mu.Lock()
	s.attempts++
	s.mu.Unlock()
	select {
	case entered <- attempt:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-attempt.reply:
		if err == nil {
			s.mu.Lock()
			s.snapshots = append(s.snapshots, attempt.messages)
			s.mu.Unlock()
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *checkpointStore) latest() []memory.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.snapshots) == 0 {
		return nil
	}
	return append([]memory.Message(nil), s.snapshots[len(s.snapshots)-1]...)
}

func (s *checkpointStore) counts() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attempts, len(s.snapshots)
}

type checkpointCallNotice struct {
	event ToolCallEvent
	saved []memory.Message
}

type checkpointResultNotice struct {
	event ToolResultEvent
	saved []memory.Message
}

type checkpointSink struct {
	mu         sync.Mutex
	store      *checkpointStore
	calls      []checkpointCallNotice
	results    []checkpointResultNotice
	assistants [][]memory.Message
	resultCh   chan checkpointResultNotice
}

func (*checkpointSink) Status(StatusEvent) {}
func (*checkpointSink) Stream(string)      {}
func (*checkpointSink) Reasoning(string)   {}
func (*checkpointSink) Usage(UsageEvent)   {}

func (s *checkpointSink) ToolCall(event ToolCallEvent) {
	notice := checkpointCallNotice{event: event, saved: s.store.latest()}
	s.mu.Lock()
	s.calls = append(s.calls, notice)
	s.mu.Unlock()
}

func (s *checkpointSink) ToolResult(event ToolResultEvent) {
	notice := checkpointResultNotice{event: event, saved: s.store.latest()}
	s.mu.Lock()
	s.results = append(s.results, notice)
	s.mu.Unlock()
	select {
	case s.resultCh <- notice:
	default:
	}
}

func (s *checkpointSink) assistantSaved() {
	saved := s.store.latest()
	s.mu.Lock()
	s.assistants = append(s.assistants, saved)
	s.mu.Unlock()
}

func (s *checkpointSink) snapshot() ([]checkpointCallNotice, []checkpointResultNotice, [][]memory.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]checkpointCallNotice(nil), s.calls...), append([]checkpointResultNotice(nil), s.results...), append([][]memory.Message(nil), s.assistants...)
}

type checkpointExecutor struct {
	mu      sync.Mutex
	calls   []ToolExecution
	started chan ToolExecution
	gates   map[string]chan struct{}
	results map[string]tools.Result
}

func (e *checkpointExecutor) ExecuteTool(ctx context.Context, call ToolExecution) tools.Result {
	e.mu.Lock()
	e.calls = append(e.calls, call)
	e.mu.Unlock()
	select {
	case e.started <- call:
	case <-ctx.Done():
		return tools.ErrorResult(ctx.Err().Error())
	}
	gate, ok := e.gates[call.ID]
	if !ok {
		return tools.ErrorResult("unexpected call")
	}
	select {
	case <-gate:
		if result, ok := e.results[call.ID]; ok {
			return result
		}
		return tools.TextResult("result-" + call.ID)
	case <-ctx.Done():
		return tools.ErrorResult(ctx.Err().Error())
	}
}

func (e *checkpointExecutor) snapshot() []ToolExecution {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]ToolExecution(nil), e.calls...)
}

type checkpointRunResponse struct {
	result Result
	err    error
}

type checkpointHarness struct {
	ctx      context.Context
	working  *memory.WorkingMemory
	store    *checkpointStore
	sink     *checkpointSink
	executor *checkpointExecutor
	server   *checkpointHTTPServer
	entered  chan checkpointAttempt
	done     chan checkpointRunResponse
	runner   *Runner
	request  Request
}

func newCheckpointHarness(t *testing.T, blockedTools bool) *checkpointHarness {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	store := &checkpointStore{}
	server := &checkpointHTTPServer{store: store}
	httpServer := httptest.NewServer(server)
	t.Cleanup(func() {
		cancel()
		httpServer.Close()
	})
	cfg := &config.Config{MaxModelRPS: 100, Models: []config.ModelConfig{{
		Provider:        "custom",
		Protocol:        config.ModelProtocolOpenAIChat,
		Model:           "offline",
		BaseURL:         httpServer.URL,
		ContextWindow:   128000,
		MaxOutputTokens: 1024,
		APIKey:          "unused",
	}}}
	router, err := model.NewRouter(cfg, nil)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	binding, err := router.Bind("custom/offline")
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	working := memory.NewWorkingMemory()
	working.AddMessage(memory.NewTextMessage(model.RoleUser, "start"))
	executor := &checkpointExecutor{
		started: make(chan ToolExecution, 8),
		gates:   map[string]chan struct{}{"call-a": make(chan struct{}), "call-b": make(chan struct{})},
	}
	if !blockedTools {
		close(executor.gates["call-a"])
		close(executor.gates["call-b"])
	}
	sink := &checkpointSink{store: store, resultCh: make(chan checkpointResultNotice, 8)}
	entered := make(chan checkpointAttempt, 8)
	r := &Runner{Executor: executor, Sink: sink, Hooks: Hooks{
		Checkpoint:       func(ctx context.Context) error { return store.checkpoint(ctx, working, entered) },
		OnAssistantSaved: func(context.Context) { sink.assistantSaved() },
	}}
	defs := []model.ToolDef{
		{Name: "alpha", Parameters: map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}}},
		{Name: "beta", Parameters: map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}}},
	}
	return &checkpointHarness{
		ctx: ctx, working: working, store: store, sink: sink, executor: executor, server: server,
		entered: entered, done: make(chan checkpointRunResponse, 1), runner: r,
		request: Request{Binding: binding, Working: working, ToolDefs: func() []model.ToolDef { return defs }, MaxTurns: 3},
	}
}

func (h *checkpointHarness) start() {
	go func() {
		result, err := h.runner.Run(h.ctx, h.request)
		h.done <- checkpointRunResponse{result: result, err: err}
	}()
}

func (h *checkpointHarness) nextCheckpoint(t *testing.T) checkpointAttempt {
	t.Helper()
	select {
	case attempt := <-h.entered:
		return attempt
	case done := <-h.done:
		t.Fatalf("Run returned before checkpoint: result=%+v err=%v", done.result, done.err)
	case <-h.ctx.Done():
		t.Fatalf("waiting for checkpoint: %v", h.ctx.Err())
	}
	return checkpointAttempt{}
}

func (h *checkpointHarness) nextStarted(t *testing.T) ToolExecution {
	t.Helper()
	select {
	case call := <-h.executor.started:
		return call
	case done := <-h.done:
		t.Fatalf("Run returned before tool started: result=%+v err=%v", done.result, done.err)
	case <-h.ctx.Done():
		t.Fatalf("waiting for tool: %v", h.ctx.Err())
	}
	return ToolExecution{}
}

func (h *checkpointHarness) nextResult(t *testing.T) checkpointResultNotice {
	t.Helper()
	select {
	case notice := <-h.sink.resultCh:
		return notice
	case done := <-h.done:
		t.Fatalf("Run returned before tool notification: result=%+v err=%v", done.result, done.err)
	case <-h.ctx.Done():
		t.Fatalf("waiting for tool notification: %v", h.ctx.Err())
	}
	return checkpointResultNotice{}
}

func (h *checkpointHarness) finish(t *testing.T) checkpointRunResponse {
	t.Helper()
	select {
	case done := <-h.done:
		return done
	case attempt := <-h.entered:
		t.Fatalf("unexpected checkpoint: %+v", attempt.messages)
	case <-h.ctx.Done():
		t.Fatalf("waiting for Run: %v", h.ctx.Err())
	}
	return checkpointRunResponse{}
}

func assertCheckpointArguments(t *testing.T, raw, want string) {
	t.Helper()
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil || !reflect.DeepEqual(args, map[string]any{"value": want}) {
		t.Fatalf("arguments = %q, decoded = %#v, err = %v; want value %q", raw, args, err, want)
	}
}

// 每个快照都检查完整声明和真实结果，避免只凭调用次数判断保存边界。
func assertCheckpointHistory(t *testing.T, messages []memory.Message, ids []string, final bool) {
	t.Helper()
	wantLen := 2 + len(ids)
	if final {
		wantLen++
	}
	if len(messages) != wantLen {
		t.Fatalf("history length = %d, want %d: %+v", len(messages), wantLen, messages)
	}
	if messages[0].Role != model.RoleUser || messages[0].Text() != "start" {
		t.Fatalf("initial message = %+v", messages[0])
	}
	assistant := messages[1]
	if assistant.Role != model.RoleAssistant || assistant.Text() != "prepare" || len(assistant.ToolCalls) != 2 {
		t.Fatalf("assistant declaration = %+v", assistant)
	}
	for i, want := range []struct{ id, name, value string }{{"call-a", "alpha", "A"}, {"call-b", "beta", "B"}} {
		call := assistant.ToolCalls[i]
		if call.ID != want.id || call.Name != want.name {
			t.Fatalf("declaration call[%d] = %+v, want %s/%s", i, call, want.id, want.name)
		}
		assertCheckpointArguments(t, call.Arguments, want.value)
	}
	for i, id := range ids {
		message := messages[2+i]
		if message.Role != model.RoleTool || message.ToolCallID != id || message.Text() != "result-"+id || message.ToolStatus != "completed" {
			t.Fatalf("tool message[%d] = %+v, want completed result for %s", i, message, id)
		}
	}
	if final {
		last := messages[len(messages)-1]
		if last.Role != model.RoleAssistant || last.Text() != "complete" {
			t.Fatalf("final message = %+v", last)
		}
	}
}

func assertCheckpointStarted(t *testing.T, h *checkpointHarness) {
	t.Helper()
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		call := h.nextStarted(t)
		wantName, wantValue := "alpha", "A"
		if call.ID == "call-b" {
			wantName, wantValue = "beta", "B"
		} else if call.ID != "call-a" {
			t.Fatalf("unexpected execution = %+v", call)
		}
		if seen[call.ID] || call.Name != wantName || !reflect.DeepEqual(call.Params, map[string]any{"value": wantValue}) {
			t.Fatalf("duplicate or malformed execution = %+v", call)
		}
		seen[call.ID] = true
		if !reflect.DeepEqual(call.WorkingMessages, memory.ModelMessages(h.working.Messages())) {
			t.Fatalf("execution request projection = %+v, want exact saved declaration", call.WorkingMessages)
		}
	}
	calls, results, assistants := h.sink.snapshot()
	if len(calls) != 2 || len(results) != 0 || len(assistants) != 1 {
		t.Fatalf("notices before results: calls=%+v results=%+v assistants=%d", calls, results, len(assistants))
	}
	for i, id := range []string{"call-a", "call-b"} {
		if calls[i].event.ID != id {
			t.Fatalf("tool call notices = %+v, want declaration order", calls)
		}
		assertCheckpointHistory(t, calls[i].saved, nil, false)
	}
	assertCheckpointHistory(t, assistants[0], nil, false)
}

func assertCheckpointResult(t *testing.T, notice checkpointResultNotice, id string, savedIDs []string) {
	t.Helper()
	name := "alpha"
	if id == "call-b" {
		name = "beta"
	}
	if notice.event.ID != id || notice.event.Name != name || notice.event.Result != "result-"+id || notice.event.Error {
		t.Fatalf("result notice = %+v, want successful %s", notice.event, id)
	}
	assertCheckpointHistory(t, notice.saved, savedIDs, false)
}

func assertCheckpointRequests(t *testing.T, records []checkpointHTTPRecord, count int) {
	t.Helper()
	if len(records) != count {
		t.Fatalf("model request count = %d, want %d", len(records), count)
	}
	for i, record := range records {
		if record.decodeErr != nil || record.method != http.MethodPost || record.request.Model != "offline" {
			t.Fatalf("request[%d] = %+v", i, record)
		}
		if len(record.request.Tools) != 2 || record.request.Tools[0].Type != "function" || record.request.Tools[0].Function.Name != "alpha" || record.request.Tools[1].Type != "function" || record.request.Tools[1].Function.Name != "beta" {
			t.Fatalf("request[%d] tools = %+v", i, record.request.Tools)
		}
	}
	if count == 0 {
		return
	}
	first := records[0].request.Messages
	if len(first) != 1 || first[0].Role != "user" {
		t.Fatalf("first request messages = %+v", first)
	}
	if count < 2 {
		return
	}
	second := records[1].request.Messages
	if len(second) != 4 || second[0].Role != "user" || second[1].Role != "assistant" || len(second[1].ToolCalls) != 2 {
		t.Fatalf("second request messages = %+v", second)
	}
	for i, want := range []struct{ id, name, value string }{{"call-a", "alpha", "A"}, {"call-b", "beta", "B"}} {
		call := second[1].ToolCalls[i]
		if call.ID != want.id || call.Type != "function" || call.Function.Name != want.name {
			t.Fatalf("second request declaration[%d] = %+v", i, call)
		}
		assertCheckpointArguments(t, call.Function.Arguments, want.value)
		tool := second[2+i]
		var content string
		if err := json.Unmarshal(tool.Content, &content); err != nil || tool.Role != "tool" || tool.ToolCallID != want.id || content != "result-"+want.id {
			t.Fatalf("second request result[%d] = %+v, content=%q err=%v", i, tool, content, err)
		}
	}
	assertCheckpointHistory(t, records[1].saved, []string{"call-a", "call-b"}, false)
}

func TestCheckpointDeclarationFailureStopsBeforeTools(t *testing.T) {
	h := newCheckpointHarness(t, false)
	h.start()
	declaration := h.nextCheckpoint(t)
	assertCheckpointHistory(t, declaration.messages, nil, false)
	calls, results, assistants := h.sink.snapshot()
	if len(h.executor.snapshot()) != 0 || len(calls) != 0 || len(results) != 0 || len(assistants) != 0 {
		t.Fatalf("tool or notification visible during declaration checkpoint: calls=%+v results=%+v assistants=%d", calls, results, len(assistants))
	}
	failure := errors.New("declaration checkpoint failed")
	declaration.reply <- failure
	done := h.finish(t)
	if !errors.Is(done.err, failure) || done.result.HadToolCall {
		t.Fatalf("Run = %+v, err = %v, want declaration failure before execution", done.result, done.err)
	}
	calls, results, assistants = h.sink.snapshot()
	if len(h.executor.snapshot()) != 0 || len(calls) != 0 || len(results) != 0 || len(assistants) != 0 {
		t.Fatalf("failure exposed execution or notifications: calls=%+v results=%+v assistants=%d", calls, results, len(assistants))
	}
	if attempts, saved := h.store.counts(); attempts != 1 || saved != 0 {
		t.Fatalf("checkpoint attempts=%d saved=%d, want 1/0", attempts, saved)
	}
	assertCheckpointHistory(t, h.working.Messages(), nil, false)
	assertCheckpointRequests(t, h.server.snapshot(), 1)
}

func TestCheckpointFailureDrainsParallelResultsAndPreservesMemory(t *testing.T) {
	h := newCheckpointHarness(t, true)
	h.start()
	declaration := h.nextCheckpoint(t)
	assertCheckpointHistory(t, declaration.messages, nil, false)
	declaration.reply <- nil
	assertCheckpointStarted(t, h)

	// 两个调用均已启动；先完成声明顺序靠后的调用，并让其保存失败。
	close(h.executor.gates["call-b"])
	first := h.nextCheckpoint(t)
	assertCheckpointHistory(t, first.messages, []string{"call-b"}, false)
	_, results, _ := h.sink.snapshot()
	if len(results) != 0 {
		t.Fatalf("result visible before failed checkpoint returns: %+v", results)
	}
	failure := errors.New("result checkpoint failed")
	first.reply <- failure
	close(h.executor.gates["call-a"])

	// 即使已有保存错误，也必须收集另一个真实结果；成功覆盖前仍不得通知。
	second := h.nextCheckpoint(t)
	assertCheckpointHistory(t, second.messages, []string{"call-a", "call-b"}, false)
	_, results, _ = h.sink.snapshot()
	if len(results) != 0 {
		t.Fatalf("unsaved result was notified: %+v", results)
	}
	assertCheckpointRequests(t, h.server.snapshot(), 1)
	second.reply <- nil
	done := h.finish(t)
	if !errors.Is(done.err, failure) || !done.result.HadToolCall {
		t.Fatalf("Run = %+v, err = %v, want original checkpoint failure", done.result, done.err)
	}
	if len(h.executor.snapshot()) != 2 {
		t.Fatalf("executions = %+v, want both calls", h.executor.snapshot())
	}
	assertCheckpointHistory(t, h.working.Messages(), []string{"call-a", "call-b"}, false)
	calls, results, assistants := h.sink.snapshot()
	if len(calls) != 2 || len(results) != 2 || len(assistants) != 1 {
		t.Fatalf("notices after drain: calls=%+v results=%+v assistants=%d", calls, results, len(assistants))
	}
	assertCheckpointResult(t, results[0], "call-b", []string{"call-a", "call-b"})
	assertCheckpointResult(t, results[1], "call-a", []string{"call-a", "call-b"})
	if attempts, saved := h.store.counts(); attempts != 3 || saved != 2 {
		t.Fatalf("checkpoint attempts=%d saved=%d, want 3/2", attempts, saved)
	}
	assertCheckpointRequests(t, h.server.snapshot(), 1)
}

func TestCheckpointResultsBecomeVisibleOnlyAfterEachSave(t *testing.T) {
	h := newCheckpointHarness(t, true)
	h.start()
	declaration := h.nextCheckpoint(t)
	assertCheckpointHistory(t, declaration.messages, nil, false)
	declaration.reply <- nil
	assertCheckpointStarted(t, h)

	close(h.executor.gates["call-b"])
	first := h.nextCheckpoint(t)
	assertCheckpointHistory(t, first.messages, []string{"call-b"}, false)
	_, results, _ := h.sink.snapshot()
	if len(results) != 0 {
		t.Fatalf("first result visible while checkpoint is blocked: %+v", results)
	}
	assertCheckpointRequests(t, h.server.snapshot(), 1)
	first.reply <- nil
	assertCheckpointResult(t, h.nextResult(t), "call-b", []string{"call-b"})

	close(h.executor.gates["call-a"])
	second := h.nextCheckpoint(t)
	assertCheckpointHistory(t, second.messages, []string{"call-a", "call-b"}, false)
	_, results, _ = h.sink.snapshot()
	if len(results) != 1 {
		t.Fatalf("second result visible while checkpoint is blocked: %+v", results)
	}
	assertCheckpointRequests(t, h.server.snapshot(), 1)
	second.reply <- nil
	assertCheckpointResult(t, h.nextResult(t), "call-a", []string{"call-a", "call-b"})

	// 无图片就没有新的有效历史；下一检查点必须直接是完整最终回答。
	final := h.nextCheckpoint(t)
	assertCheckpointHistory(t, final.messages, []string{"call-a", "call-b"}, true)
	final.reply <- nil
	done := h.finish(t)
	if done.err != nil || done.result.FinalText != "complete" || !done.result.HadToolCall || done.result.HadToolError {
		t.Fatalf("Run = %+v, err = %v", done.result, done.err)
	}
	if attempts, saved := h.store.counts(); attempts != 4 || saved != 4 {
		t.Fatalf("checkpoint attempts=%d saved=%d, want 4/4", attempts, saved)
	}
	assertCheckpointHistory(t, h.working.Messages(), []string{"call-a", "call-b"}, true)
	calls, results, assistants := h.sink.snapshot()
	if len(calls) != 2 || len(results) != 2 || len(assistants) != 2 {
		t.Fatalf("final notices: calls=%+v results=%+v assistants=%d", calls, results, len(assistants))
	}
	assertCheckpointResult(t, results[0], "call-b", []string{"call-b"})
	assertCheckpointResult(t, results[1], "call-a", []string{"call-a", "call-b"})
	assertCheckpointHistory(t, assistants[0], nil, false)
	assertCheckpointHistory(t, assistants[1], []string{"call-a", "call-b"}, true)
	assertCheckpointRequests(t, h.server.snapshot(), 2)
}

func TestCheckpointAllResultSavesFailWithoutPublishingResults(t *testing.T) {
	h := newCheckpointHarness(t, true)
	h.start()
	declaration := h.nextCheckpoint(t)
	assertCheckpointHistory(t, declaration.messages, nil, false)
	declaration.reply <- nil
	assertCheckpointStarted(t, h)

	close(h.executor.gates["call-b"])
	first := h.nextCheckpoint(t)
	assertCheckpointHistory(t, first.messages, []string{"call-b"}, false)
	failure := errors.New("result checkpoint unavailable")
	first.reply <- failure
	close(h.executor.gates["call-a"])
	second := h.nextCheckpoint(t)
	assertCheckpointHistory(t, second.messages, []string{"call-a", "call-b"}, false)
	_, results, _ := h.sink.snapshot()
	if len(results) != 0 {
		t.Fatalf("results visible before any successful result save: %+v", results)
	}
	second.reply <- errors.New("second result checkpoint also failed")
	done := h.finish(t)
	if !errors.Is(done.err, failure) || !done.result.HadToolCall {
		t.Fatalf("Run = %+v, err = %v, want result checkpoint failure", done.result, done.err)
	}
	if len(h.executor.snapshot()) != 2 {
		t.Fatalf("executions = %+v, want both calls", h.executor.snapshot())
	}
	// 持久化仍只有声明，但内存必须保留两个真实结果且不对外发布。
	assertCheckpointHistory(t, h.store.latest(), nil, false)
	assertCheckpointHistory(t, h.working.Messages(), []string{"call-a", "call-b"}, false)
	calls, results, assistants := h.sink.snapshot()
	if len(calls) != 2 || len(results) != 0 || len(assistants) != 1 {
		t.Fatalf("notices after failed saves: calls=%+v results=%+v assistants=%d", calls, results, len(assistants))
	}
	if attempts, saved := h.store.counts(); attempts != 3 || saved != 1 {
		t.Fatalf("checkpoint attempts=%d saved=%d, want 3/1", attempts, saved)
	}
	assertCheckpointRequests(t, h.server.snapshot(), 1)
}
