package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/guard"
	"github.com/alanchenchen/suna/internal/media"
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
	res := subtask.Result{Status: subtask.StatusFailed, Error: "context deadline exceeded", SideEffects: subtask.SideEffects{Status: subtask.SideEffectsUnknown}}
	payload := spawnResultPayload(res)
	outBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal spawn result: %v", err)
	}
	out := spawnToolResult(string(outBytes), res)
	if !out.IsError {
		t.Fatalf("spawnToolResult IsError = false, want true")
	}
	if out.Error != "context deadline exceeded" {
		t.Fatalf("spawnToolResult Error = %q, want context deadline exceeded", out.Error)
	}
	if out.Content == "" || out.Content[0] != '{' {
		t.Fatalf("spawnToolResult Content = %q, want JSON payload preserved", out.Content)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out.Content), &decoded); err != nil {
		t.Fatalf("spawnToolResult Content JSON: %v", err)
	}
	if _, ok := decoded["success"]; ok {
		t.Fatalf("spawn result contains success = %v, want field removed", decoded["success"])
	}
	if decoded["status"] != string(subtask.StatusFailed) {
		t.Fatalf("status = %v, want %s", decoded["status"], subtask.StatusFailed)
	}
	if decoded["error"] != "context deadline exceeded" {
		t.Fatalf("error = %v, want context deadline exceeded", decoded["error"])
	}
	if decoded["side_effects"] == nil {
		t.Fatalf("side_effects missing in %#v", decoded)
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

func TestToolIntentSchemaUsesCurrentUserLanguage(t *testing.T) {
	description, _ := toolIntentSchema()["description"].(string)
	for _, want := range []string{"user-facing", "language of the user's current request", "Do not include file contents, secrets, or raw parameters"} {
		if !strings.Contains(description, want) {
			t.Fatalf("intent description missing %q: %q", want, description)
		}
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
	if ctx.Task != "delegated subtask request" {
		t.Fatalf("Task = %q, want delegated subtask request", ctx.Task)
	}
	if ctx.LatestUserInput != "delegated subtask request" {
		t.Fatalf("LatestUserInput = %q, want delegated subtask request", ctx.LatestUserInput)
	}
	if ctx.UserDecisions != "" {
		t.Fatalf("UserDecisions = %q, want empty", ctx.UserDecisions)
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

func TestBuildToolDefsSupportsComposedExecSchemaAndCleaning(t *testing.T) {
	mgr := tools.NewManager()
	a := &Agent{tools: mgr}
	mgr.RegisterProvider(builtin.NewProvider())
	if err := mgr.Reload(context.Background()); err != nil {
		t.Fatalf("Reload tools: %v", err)
	}

	defs := a.buildToolDefs()
	var execDef *model.ToolDef
	for index := range defs {
		if defs[index].Name == "exec" {
			execDef = &defs[index]
			break
		}
	}
	if execDef == nil {
		t.Fatal("exec tool definition missing")
	}
	branches, ok := execDef.Parameters["oneOf"].([]any)
	if !ok || len(branches) != 4 {
		t.Fatalf("exec oneOf = %#v, want four branches", execDef.Parameters["oneOf"])
	}
	for index, branch := range branches {
		object, ok := branch.(map[string]any)
		if !ok {
			t.Fatalf("exec branch %d = %#v", index, branch)
		}
		props, ok := object["properties"].(map[string]any)
		if !ok {
			t.Fatalf("exec branch %d properties missing", index)
		}
		if _, ok := props["intent"]; !ok {
			t.Fatalf("exec branch %d intent missing", index)
		}
	}

	for _, params := range []map[string]any{
		{"action": "run", "command": "printf ok", "background": true, "scope": "run", "intent": "run a managed command", "unknown": true},
		{"action": "status", "job_id": "job-1", "cursor": float64(10), "intent": "read output", "unknown": true},
		{"action": "stop", "job_id": "job-1", "intent": "stop the job", "unknown": true},
	} {
		cleaned, intent := a.cleanToolParams("exec", params)
		if intent == "" || cleaned["action"] == nil {
			t.Fatalf("cleaned params/intent = %#v/%q", cleaned, intent)
		}
		if _, exists := cleaned["unknown"]; exists {
			t.Fatalf("unknown field survived cleaning: %#v", cleaned)
		}
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
	want := []string{"askuser", "editfile", "exec", "filesystem", "http", "listdir", "readfile", "search", "spawn", "writefile"}
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

func TestExecuteSpawnToolRejectsModelHiddenBySubtaskFor(t *testing.T) {
	cfg := &config.Config{
		ActiveModel: "openai/gpt-4.1",
		Models: []config.ModelConfig{
			{Provider: "openai", Model: "gpt-4.1", BaseURL: "https://api.example.com/v1", ContextWindow: 400000, MaxOutputTokens: 8192, APIKey: "test-api-key"},
			{Provider: "anthropic", Model: "claude-sonnet-4", BaseURL: "https://api.anthropic.com", ContextWindow: 200000, MaxOutputTokens: 8192, APIKey: "test-api-key", SubtaskFor: []string{"anthropic/**"}},
		},
	}
	router, err := model.NewRouter(cfg, media.NewStore(t.TempDir()))
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	a := &Agent{cfg: cfg, router: router, modelRef: "openai/gpt-4.1"}
	events := make(chan Event, 1)
	var sink chan<- Event = events
	ctx := agenttools.WithEvents(context.Background(), sink)

	result := a.ExecuteSpawnTool(ctx, "spawn-1", map[string]any{
		"task":  "check something",
		"model": "anthropic/claude-sonnet-4",
		"tools": []any{},
	})
	if !result.IsError {
		t.Fatalf("ExecuteSpawnTool() IsError = false, want true")
	}
	if !strings.Contains(result.Error, "not available for session model") {
		t.Fatalf("ExecuteSpawnTool() error = %q, want availability message", result.Error)
	}
	if strings.Contains(result.Error, "anthropic/claude-sonnet-4") && strings.Contains(result.Error, "Choose one of: anthropic/claude-sonnet-4") {
		t.Fatalf("ExecuteSpawnTool() error = %q, should not list hidden model as choice", result.Error)
	}
}

func TestMainGuardGateMakesApprovedReceiptVisibleToNextReview(t *testing.T) {
	mgr := tools.NewManager()
	mgr.RegisterProvider(builtin.NewProvider())
	if err := mgr.Reload(context.Background()); err != nil {
		t.Fatalf("Reload tools: %v", err)
	}
	a := &Agent{guard: guard.NewGuardWithMode(nil, "test", guard.ModeSmart), tools: mgr}
	a.beginGuardTask("Update two related files for the active fix.")

	var reviewCount int
	var secondContext guard.ReviewContext
	a.guard.SetLLMReviewer(func(ctx context.Context, req guard.ReviewRequest) (string, error) {
		reviewCount++
		if reviewCount == 1 {
			return `{"decision":"confirm","reason":"need user confirmation","suggestion":""}`, nil
		}
		secondContext = req.Context
		return `{"decision":"approve","reason":"approved continuation","suggestion":""}`, nil
	})

	events := make(chan Event, 4)
	call := func(id, path string) <-chan tools.Result {
		result := make(chan tools.Result, 1)
		go func() {
			result <- a.executeTool(context.Background(), runner.ToolExecution{ID: id, Name: "writefile", Params: map[string]any{"path": path, "content": "updated"}, Intent: "apply the active related fix"}, events)
		}()
		return result
	}
	first := call("first", t.TempDir()+"/first.txt")
	second := call("second", t.TempDir()+"/second.txt")

	for {
		select {
		case event := <-events:
			if event.Type != EventGuardConfirm {
				continue
			}
			event.Reply <- "approve"
			goto approved
		case <-time.After(time.Second):
			t.Fatal("first guard confirmation was not emitted")
		}
	}

approved:
	for _, result := range []<-chan tools.Result{first, second} {
		select {
		case got := <-result:
			if got.IsError {
				t.Fatalf("tool result = %#v, want success", got)
			}
		case <-time.After(time.Second):
			t.Fatal("tool execution did not complete")
		}
	}
	if reviewCount != 2 {
		t.Fatalf("review count = %d, want 2", reviewCount)
	}
	if !strings.Contains(secondContext.UserDecisions, "approved") || !strings.Contains(secondContext.UserDecisions, "writefile") {
		t.Fatalf("second review decisions = %q, want approved receipt", secondContext.UserDecisions)
	}
}

func TestGuardTaskCardStartsNewTaskForEveryNewUserInput(t *testing.T) {
	a := &Agent{}
	a.beginGuardTask("Fix the Gateway reconnect flow and add regression tests.")
	a.recordGuardTaskReceipt(runner.ToolExecution{Name: "editfile", Params: map[string]any{"path": "gateway/bridge.go"}, Intent: "fix reconnect"}, &guard.GuardResult{Risk: guard.RiskMedium}, true)

	a.beginGuardTask("please continue with the regression test")
	task, latest, receipts, prior := a.guardTaskReviewContext()
	if task != "please continue with the regression test" {
		t.Fatalf("Task = %q, want new user input", task)
	}
	if latest != "please continue with the regression test" {
		t.Fatalf("LatestUserInput = %q, want new user input", latest)
	}
	if receipts != "" {
		t.Fatalf("UserDecisions = %q, want cleared decisions", receipts)
	}
	if !strings.Contains(prior, "gateway/bridge.go") || !strings.Contains(prior, "approved") {
		t.Fatalf("PreviousTask = %q, want prior approval receipt", prior)
	}
}

func TestGuardTaskCardRecordsSafeOperationReceipt(t *testing.T) {
	a := &Agent{}
	a.beginGuardTask("Fix the Gateway reconnect flow.")
	a.recordGuardTaskReceipt(runner.ToolExecution{
		Name:   "editfile",
		Params: map[string]any{"path": "gateway/bridge.go", "edits": []any{map[string]any{"old_string": "secret-old", "new_string": "secret-new"}}},
		Intent: "fix reconnect lifecycle",
	}, &guard.GuardResult{Risk: guard.RiskMedium}, true)

	_, _, receipts, _ := a.guardTaskReviewContext()
	if !strings.Contains(receipts, "gateway/bridge.go") || !strings.Contains(receipts, "fix reconnect lifecycle") {
		t.Fatalf("UserDecisions = %q, want target and rationale", receipts)
	}
	if strings.Contains(receipts, "secret-old") || strings.Contains(receipts, "secret-new") {
		t.Fatalf("UserDecisions = %q, must not expose edit contents", receipts)
	}
}

func TestTrimForGuardMiddlePreservesUTF8(t *testing.T) {
	got := trimForGuardMiddle(strings.Repeat("继续修复", 100), 31)
	if !utf8.ValidString(got) {
		t.Fatalf("trimForGuardMiddle() = %q, want valid UTF-8", got)
	}
}

func TestBuildSubtaskGuardReviewContextDoesNotUseMainTaskCard(t *testing.T) {
	a := &Agent{working: testWorkingMemory("main task")}
	a.beginGuardTask("main task")
	a.recordGuardTaskReceipt(runner.ToolExecution{Name: "editfile", Params: map[string]any{"path": "main.go"}, Intent: "main approval"}, &guard.GuardResult{Risk: guard.RiskMedium}, true)

	ctx := a.buildSubtaskGuardReviewContext(runner.ToolExecution{
		WorkingMessages: []model.Message{model.NewTextMessage(model.RoleUser, "delegated task")},
	})
	if ctx.Task != "delegated task" || ctx.LatestUserInput != "delegated task" {
		t.Fatalf("subtask task/input = %q/%q, want delegated task", ctx.Task, ctx.LatestUserInput)
	}
	if ctx.UserDecisions != "" {
		t.Fatalf("subtask UserDecisions = %q, want empty", ctx.UserDecisions)
	}
}

func TestTruncateGuardReviewParamsKeepsStructuredSummary(t *testing.T) {
	complete, completeTruncated := truncateGuardReviewParams(`{"path":"report.md"}`)
	if completeTruncated {
		t.Fatal("structured params marked truncated")
	}
	if complete != `{"path":"report.md"}` {
		t.Fatalf("structured params = %q, want unchanged", complete)
	}
}
