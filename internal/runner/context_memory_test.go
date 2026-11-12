package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/prompt"
)

func newBackgroundMemoryTest(t *testing.T, release <-chan struct{}) (*ContextMemory, *memory.Compressor, CompactRequest, <-chan struct{}, *atomic.Int32) {
	t.Helper()
	started := make(chan struct{}, 1)
	requests := new(atomic.Int32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		payload, _ := json.Marshal(map[string]any{"observations": []map[string]any{{"id": 0, "keep": false, "text": "Saved exact evidence; work remains unverified."}}})
		content, _ := json.Marshal(string(payload))
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"id\":\"local\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"offline\",\"choices\":[{\"index\":0,\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", content)
	}))
	t.Cleanup(server.Close)
	router, err := model.NewRouter(&config.Config{MaxModelRPS: 100, Models: []config.ModelConfig{{Provider: "custom", Protocol: config.ModelProtocolOpenAIChat, Model: "offline", BaseURL: server.URL, APIKey: "test-api-key", ContextWindow: 100000, MaxOutputTokens: 32000}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := router.Bind("custom/offline")
	if err != nil {
		t.Fatal(err)
	}
	loader, err := prompt.New()
	if err != nil {
		t.Fatal(err)
	}
	compressor := memory.NewCompressor()
	compressor.SetPrompts(loader)
	working := memory.NewWorkingMemory()
	messages := []memory.Message{{Role: model.RoleUser, Origin: memory.OriginUser, TextContent: "inspect source", Consumed: true}, {Role: model.RoleAssistant, TextContent: "Original prose.", Consumed: true, ToolCalls: []memory.ToolCall{{ID: "call", Name: "readfile", Arguments: `{"path":"example.go"}`}}}, {Role: model.RoleTool, ToolCallID: "call", TextContent: strings.Repeat("source evidence ", 12000), ToolStatus: "completed", Consumed: true}, {Role: model.RoleAssistant, TextContent: "Explained results.", Consumed: true}, {Role: model.RoleUser, Origin: memory.OriginUser, TextContent: strings.Repeat("recent discussion ", 3000), Consumed: true}, {Role: model.RoleAssistant, TextContent: "Recent answer.", Consumed: true}}
	working.SetMessages(messages)
	state := NewContextMemory("")
	q := CompactRequest{Binding: binding, Working: working, Request: model.CompletionRequest{MaxTokens: 32000}, Coefficient: 1, Calibrated: true}
	return state, compressor, q, started, requests
}
func awaitCompact(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("compression did not reach expected boundary")
	}
}
func TestBackgroundCandidateIsReadOnlyUntilSafeCommitAndPersistsBeforePublish(t *testing.T) {
	release := make(chan struct{})
	state, compressor, q, started, count := newBackgroundMemoryTest(t, release)
	ready := make(chan struct{}, 1)
	state.Ready = func() { ready <- struct{}{} }
	original := q.Working.Messages()
	if err := state.Maintain(context.Background(), compressor, q, false); err != nil {
		t.Fatal(err)
	}
	awaitCompact(t, started)
	if len(q.Working.Messages()[1].ToolCalls) != 1 || state.Snapshot().State != "preparing" {
		t.Fatal("preparing changed live source")
	}
	close(release)
	awaitCompact(t, ready)
	if len(q.Working.Messages()[1].ToolCalls) != 1 || state.Snapshot().State != "ready" {
		t.Fatal("ready changed live source")
	}
	suffix := memory.NewTextMessage(model.RoleUser, "new correction")
	suffix.Origin = memory.OriginUser
	q.Working.AddMessage(suffix)
	saves := 0
	q.Commit = func(_ context.Context, _ string, messages []memory.Message) error {
		saves++
		if len(q.Working.Messages()[1].ToolCalls) != 1 || len(messages[1].ToolCalls) != 0 {
			t.Fatal("RAM changed before durable commit")
		}
		return nil
	}
	// 新输入未被模型消费时暂缓采用，仍保留已经付费生成的候选。
	if err := state.Maintain(context.Background(), compressor, q, false); err != nil {
		t.Fatal(err)
	}
	if saves != 0 || state.Snapshot().State != "ready" {
		t.Fatal("pending input did not defer adoption")
	}
	q.Working.MarkConsumed()
	if err := state.Maintain(context.Background(), compressor, q, false); err != nil {
		t.Fatal(err)
	}
	got := q.Working.Messages()
	if saves != 1 || count.Load() != 1 || len(got) != len(original)-1+1 || got[len(got)-1].Text() != "new correction" || state.Snapshot().State != "applied" {
		t.Fatalf("saves=%d requests=%d messages=%d state=%s", saves, count.Load(), len(got), state.Snapshot().State)
	}
}
func TestHardLimitWaitsForSameBackgroundRequest(t *testing.T) {
	release := make(chan struct{})
	state, compressor, q, started, count := newBackgroundMemoryTest(t, release)
	if err := state.Maintain(context.Background(), compressor, q, false); err != nil {
		t.Fatal(err)
	}
	awaitCompact(t, started)
	q.Working.AddMessage(memory.NewTextMessage(model.RoleUser, strings.Repeat("new material ", 14000)))
	waiting := make(chan struct{}, 1)
	state.Notify = func(s CompactState) {
		if s.Wait == "hard_limit" {
			select {
			case waiting <- struct{}{}:
			default:
			}
		}
	}
	done := make(chan error, 1)
	go func() { done <- state.Maintain(context.Background(), compressor, q, false) }()
	awaitCompact(t, waiting)
	if count.Load() != 1 {
		t.Fatal("hard limit started a replacement request")
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("hard wait did not finish")
	}
	if count.Load() != 1 || state.Snapshot().State != "applied" {
		t.Fatal("hard wait did not reuse candidate")
	}
}
func TestCancelInFlightDoesNotPermitLateCommit(t *testing.T) {
	release := make(chan struct{})
	state, compressor, q, started, count := newBackgroundMemoryTest(t, release)
	if err := state.Maintain(context.Background(), compressor, q, false); err != nil {
		t.Fatal(err)
	}
	awaitCompact(t, started)
	state.Cancel()
	close(release)
	if err := state.ApplyReady(context.Background(), compressor); err != nil {
		t.Fatal(err)
	}
	if state.Snapshot().State != "cancelled" || len(q.Working.Messages()[1].ToolCalls) != 1 || count.Load() != 1 {
		t.Fatal("cancelled candidate changed state")
	}
}

func TestReadyCandidateAppliesOnceAndPreservesNewSuffix(t *testing.T) {
	working := memory.NewWorkingMemory()
	messages := []memory.Message{{Role: model.RoleUser, TextContent: "request", Consumed: true}, {Role: model.RoleAssistant, TextContent: "original", Consumed: true, ToolCalls: []memory.ToolCall{{ID: "call", Name: "readfile", Arguments: `{}`}}}, {Role: model.RoleTool, ToolCallID: "call", TextContent: strings.Repeat("data", 8000), Consumed: true}, {Role: model.RoleAssistant, TextContent: "explained", Consumed: true}, {Role: model.RoleUser, TextContent: strings.Repeat("tail", 1200), Consumed: true}}
	working.SetMessages(messages)
	plan, err := memory.PlanCompact(messages, "", memory.CompactTools, 10000, 100000, 32000, 1, true)
	if err != nil || plan == nil {
		t.Fatalf("plan=%v err=%v", plan, err)
	}
	state := NewContextMemory("")
	c := &compactCandidate{plan: plan, output: memory.CompactOutput{Observations: []memory.ToolObservation{{ID: 1, Text: "readfile evidence"}}}, done: make(chan struct{}), cancel: func() {}, state: CompactState{ID: "one", State: "ready", Kind: memory.CompactTools}, started: time.Now()}
	close(c.done)
	state.candidate = c
	state.current = c.state
	suffix := memory.NewTextMessage(model.RoleUser, "new input")
	suffix.Consumed = true
	working.AddMessage(suffix)
	// 使用离线binding，候选已ready，不应产生新的模型请求。
	binding := testContextBinding(t)
	saves := 0
	q := CompactRequest{Binding: binding, Working: working, Request: model.CompletionRequest{MaxTokens: 1000}, Coefficient: 1, Calibrated: true, Commit: func(context.Context, string, []memory.Message) error { saves++; return nil }}
	if err = state.Maintain(context.Background(), memory.NewCompressor(), q, true); err != nil {
		t.Fatal(err)
	}
	got := working.Messages()
	if saves != 1 || got[len(got)-1].Text() != "new input" || len(got[1].ToolCalls) != 0 {
		t.Fatalf("saves=%d messages=%#v", saves, got)
	}
	if state.Snapshot().State != "applied" {
		t.Fatal("candidate not applied")
	}
}
func TestCancelReadyCandidateNeverCommits(t *testing.T) {
	state := NewContextMemory("")
	c := &compactCandidate{cancel: func() {}, state: CompactState{ID: "one", State: "ready"}, started: time.Now()}
	state.candidate = c
	state.Cancel()
	if state.Snapshot().State != "cancelled" || state.candidate != nil {
		t.Fatal("candidate remained active")
	}
}

func testContextBinding(t *testing.T) *model.ModelBinding {
	t.Helper()
	router, err := model.NewRouter(&config.Config{Models: []config.ModelConfig{{Provider: "custom", Model: "test", BaseURL: "https://api.example.com/v1", APIKey: "test-api-key", ContextWindow: 100000, MaxOutputTokens: 32000}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := router.Bind("custom/test")
	if err != nil {
		t.Fatal(err)
	}
	return binding
}

func TestManualReservationDoesNotReplaceCandidateThatFailedBeforeExecution(t *testing.T) {
	state := NewContextMemory("")
	candidate := &compactCandidate{plan: &memory.CompactPlan{Kind: memory.CompactTools}, cancel: func() {}, state: CompactState{ID: "reserved", Kind: memory.CompactTools, State: "preparing"}}
	state.candidate = candidate
	state.current = candidate.state
	id := state.ReserveManual()
	state.finish(candidate, "failed", "", fmt.Errorf("offline generation failed"))
	q := CompactRequest{Binding: testContextBinding(t), Working: memory.NewWorkingMemory()}
	if err := state.Maintain(context.Background(), memory.NewCompressor(), q, true); err != nil {
		t.Fatal(err)
	}
	got := state.Snapshot()
	if id != "reserved" || got.ID != id || got.State != "failed" {
		t.Fatalf("id=%q state=%+v", id, got)
	}
}
