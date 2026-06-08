package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/guard"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/runner"
	"github.com/alanchenchen/suna/internal/subtask"
	"github.com/alanchenchen/suna/internal/tools"
	"github.com/alanchenchen/suna/internal/tools/agenttools"
	"github.com/alanchenchen/suna/internal/tools/builtin"
)

func TestSubtaskReadFileBlocksSensitivePath(t *testing.T) {
	mgr := tools.NewManager()
	mgr.RegisterProvider(builtin.NewProvider())
	if err := mgr.Reload(context.Background()); err != nil {
		t.Fatalf("Reload tools: %v", err)
	}
	a := &Agent{guard: guard.NewGuardWithMode(nil, "test", guard.ModeAuto), tools: mgr}
	executor := subtaskExecutor{agent: a, allowedTools: map[string]bool{"readfile": true}}

	result := executor.ExecuteTool(context.Background(), runner.ToolExecution{ID: "call-1", Name: "readfile", Params: map[string]any{"path": ".env"}})
	if !result.IsError || result.Error == "" {
		t.Fatalf("subtask readfile .env result = %#v, want error", result)
	}
}

func TestSpawnToolResultMarksFailedSubtaskAsToolError(t *testing.T) {
	res := subtask.Result{Success: false, Text: "context deadline exceeded", Status: "context deadline exceeded"}
	out := spawnToolResult(`{"result":"context deadline exceeded","success":false,"status":"context deadline exceeded"}`, res)
	if !out.IsError {
		t.Fatalf("spawnToolResult IsError = false, want true")
	}
	if out.Error != "context deadline exceeded" {
		t.Fatalf("spawnToolResult Error = %q, want context deadline exceeded", out.Error)
	}
	if out.Content == "" || out.Content[0] != '{' {
		t.Fatalf("spawnToolResult Content = %q, want JSON payload preserved", out.Content)
	}
}

func TestSpawnToolSchemaDoesNotExposeTimeout(t *testing.T) {
	mgr := tools.NewManager()
	a := &Agent{tools: mgr}
	mgr.RegisterProvider(builtin.NewProvider())
	mgr.RegisterProvider(agenttools.NewProvider(a))
	if err := mgr.Reload(context.Background()); err != nil {
		t.Fatalf("Reload tools: %v", err)
	}

	var spawnDef *model.ToolDef
	for _, def := range a.buildToolDefs() {
		if def.Name == "spawn" {
			spawnDef = &def
			break
		}
	}
	if spawnDef == nil {
		t.Fatalf("spawn tool def not found")
	}
	props, ok := spawnDef.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("spawn properties missing")
	}
	if _, ok := props["timeout"]; ok {
		t.Fatalf("spawn schema exposes timeout, want no subtask-level timeout")
	}
}

func TestReadGuardReviewStreamTimesOutWithoutChunks(t *testing.T) {
	ch := make(chan model.Chunk)
	_, err := readGuardReviewStream(context.Background(), ch, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "guard review LLM stream timeout") {
		t.Fatalf("readGuardReviewStream error = %v, want timeout", err)
	}
}

func TestReadGuardReviewStreamResetsTimeoutOnChunk(t *testing.T) {
	ch := make(chan model.Chunk, 2)
	ch <- model.Chunk{Content: `{"decision":"approve"}`}
	ch <- model.Chunk{Done: true}

	got, err := readGuardReviewStream(context.Background(), ch, time.Second)
	if err != nil {
		t.Fatalf("readGuardReviewStream error = %v", err)
	}
	if got != `{"decision":"approve"}` {
		t.Fatalf("readGuardReviewStream = %q, want approve JSON", got)
	}
}

func TestBuildGuardReviewContextUsesToolExecutionWorkingMessages(t *testing.T) {
	a := &Agent{working: testWorkingMemory("main user request")}
	ctx := a.buildGuardReviewContext(runner.ToolExecution{
		Intent:           "edit delegated file",
		AssistantContext: "I will apply the delegated change.",
		WorkingMessages: []model.Message{
			model.NewTextMessage(model.RoleUser, "delegated subtask request"),
			model.NewTextMessage(model.RoleAssistant, "I inspected the delegated scope."),
		},
	})
	if ctx.UserRequest != "delegated subtask request" {
		t.Fatalf("UserRequest = %q, want subtask request", ctx.UserRequest)
	}
	if ctx.RecentContext == "" || ctx.RecentContext == "[user] main user request" {
		t.Fatalf("RecentContext = %q, want execution working context", ctx.RecentContext)
	}
}

func TestSubtaskGuardEventsUseNamespacedToolID(t *testing.T) {
	mgr := tools.NewManager()
	mgr.RegisterProvider(builtin.NewProvider())
	if err := mgr.Reload(context.Background()); err != nil {
		t.Fatalf("Reload tools: %v", err)
	}
	a := &Agent{guard: guard.NewGuardWithMode(nil, "test", guard.ModeSmart), tools: mgr}
	a.guard.SetLLMReviewer(func(ctx context.Context, req guard.ReviewRequest) (string, error) {
		return `{"decision":"modify","reason":"too broad","suggestion":"narrow it"}`, nil
	})
	events := make(chan Event, 2)
	executor := subtaskExecutor{agent: a, events: events, allowedTools: map[string]bool{"writefile": true}, spawnID: "spawn-1"}

	result := executor.ExecuteTool(context.Background(), runner.ToolExecution{ID: "call-1", Name: "writefile", Params: map[string]any{"path": "out.txt", "content": "hello"}})
	if !result.IsError || result.Error == "" {
		t.Fatalf("result = %#v, want modify error", result)
	}
	select {
	case evt := <-events:
		if evt.Type != EventToolGuard {
			t.Fatalf("event type = %v, want EventToolGuard", evt.Type)
		}
		if evt.GuardToolCallID != "spawn:spawn-1:call-1" {
			t.Fatalf("GuardToolCallID = %q, want namespaced id", evt.GuardToolCallID)
		}
	case <-time.After(time.Second):
		t.Fatalf("guard event received = false, want true")
	}
}

func TestBuildSubtaskToolDefsIncludesOnlyAllowedTools(t *testing.T) {
	mgr := tools.NewManager()
	a := &Agent{tools: mgr}
	mgr.RegisterProvider(builtin.NewProvider())
	mgr.RegisterProvider(agenttools.NewProvider(a))
	if err := mgr.Reload(context.Background()); err != nil {
		t.Fatalf("Reload tools: %v", err)
	}

	defs := a.buildSubtaskToolDefs(map[string]bool{"readfile": true})
	if len(defs) != 1 || defs[0].Name != "readfile" {
		t.Fatalf("subtask tool defs = %#v, want only readfile", defs)
	}
	props, ok := defs[0].Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("readfile properties missing")
	}
	if _, ok := props["intent"]; !ok {
		t.Fatalf("readfile schema missing intent parameter")
	}

	defs = a.buildSubtaskToolDefs(map[string]bool{})
	if len(defs) != 0 {
		t.Fatalf("empty allowed tools produced defs = %#v", defs)
	}
}

func TestBuildToolDefsStableAndIncludesAgentTools(t *testing.T) {
	mgr := tools.NewManager()
	a := &Agent{tools: mgr}
	mgr.RegisterProvider(builtin.NewProvider())
	mgr.RegisterProvider(agenttools.NewProvider(a))
	if err := mgr.Reload(context.Background()); err != nil {
		t.Fatalf("Reload tools: %v", err)
	}

	defs := a.buildToolDefs()
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		names = append(names, def.Name)
	}
	want := []string{"askuser", "editfile", "exec", "listdir", "readfile", "readhttp", "spawn", "writefile", "writehttp"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("tool schema order = %#v, want %#v", names, want)
	}

	again := a.buildToolDefs()
	firstJSON, _ := json.Marshal(defs)
	secondJSON, _ := json.Marshal(again)
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("tool schema is not stable across builds")
	}
}

func testWorkingMemory(userText string) *memory.WorkingMemory {
	w := memory.NewWorkingMemory()
	w.AddMessage(model.NewTextMessage(model.RoleUser, userText))
	return w
}
