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

func TestBuildGuardEvidenceIncludesBoundedSafeRecentFacts(t *testing.T) {
	messages := []model.Message{
		model.NewTextMessage(model.RoleUser, "original task"),
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "ask-1", Name: agenttools.ToolAskUser, Arguments: `{"question":"Which scope?"}`}}},
		{Role: model.RoleTool, ToolCallID: "ask-1", TextContent: `{"answer":"current file"}`},
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "write-1", Name: "writefile", Arguments: `{"path":"report.md","content":"private body"}`}}},
		{Role: model.RoleTool, ToolCallID: "write-1", TextContent: "PRIVATE TOOL OUTPUT"},
		model.NewTextMessage(model.RoleUser, "do not change protocol"),
	}
	got := buildGuardEvidence(messages, "apply the bounded edit")
	for _, want := range []string{
		"Latest direct user message", "do not change protocol",
		"Question: Which scope?; Answer: current file",
		"apply the bounded edit",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("evidence missing %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"PRIVATE TOOL OUTPUT", "private body", "original task", "Earlier recent user messages", "Recent completed agent actions", "Session continuity constraints"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("evidence contains %q:\n%s", unwanted, got)
		}
	}
}

func TestBuildGuardEvidenceEnforcesSharedBudgetAndPriority(t *testing.T) {
	messages := []model.Message{
		model.NewTextMessage(model.RoleUser, "older-a "+strings.Repeat("a", 300)),
		model.NewTextMessage(model.RoleUser, "older-b "+strings.Repeat("b", 300)),
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "ask-1", Name: agenttools.ToolAskUser, Arguments: `{"question":"choose scope"}`}}},
		{Role: model.RoleTool, ToolCallID: "ask-1", TextContent: `{"answer":"chosen scope"}`},
		model.NewTextMessage(model.RoleUser, "LATEST-USER "+strings.Repeat("c", 300)),
	}
	got := buildGuardEvidence(messages, "RATIONALE-LOW "+strings.Repeat("q", 800))
	if runes := len([]rune(got)); runes > guardEvidenceBudget {
		t.Fatalf("evidence runes = %d, want <= %d", runes, guardEvidenceBudget)
	}
	for _, want := range []string{"LATEST-USER", "Question: choose scope; Answer: chosen scope"} {
		if !strings.Contains(got, want) {
			t.Fatalf("priority evidence missing %q:\n%s", want, got)
		}
	}
}

func TestGuardEvidenceIgnoresQueuedSteeringUntilAppliedMessageExists(t *testing.T) {
	a := &Agent{working: testWorkingMemory("original task")}
	a.setSteeringMailbox("run-1")
	if _, _, err := a.EnqueueSteering("run-1", "client-1", "do not delete"); err != nil {
		t.Fatalf("EnqueueSteering() error = %v", err)
	}
	ctx := a.buildGuardReviewContext(runner.ToolExecution{WorkingMessages: a.working.Messages()})
	if strings.Contains(ctx.Evidence, "do not delete") {
		t.Fatalf("queued steering entered evidence: %q", ctx.Evidence)
	}
	a.working.AddMessage(model.NewTextMessage(model.RoleUser, "do not delete"))
	ctx = a.buildGuardReviewContext(runner.ToolExecution{WorkingMessages: a.working.Messages()})
	if !strings.Contains(ctx.Evidence, "do not delete") {
		t.Fatalf("applied steering missing from evidence: %q", ctx.Evidence)
	}
}

func TestGuardEvidenceIgnoresIncompleteAskUser(t *testing.T) {
	messages := []model.Message{
		model.NewTextMessage(model.RoleUser, "original task"),
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "ask-1", Name: agenttools.ToolAskUser, Arguments: `{"question":"Which scope?"}`}}},
	}
	got := buildGuardEvidence(messages, "")
	if strings.Contains(got, "Which scope?") || strings.Contains(got, "Resolved AskUser") {
		t.Fatalf("incomplete AskUser entered evidence: %q", got)
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
	if !strings.Contains(ctx.Evidence, "delegated subtask request") || strings.Contains(ctx.Evidence, "main user request") {
		t.Fatalf("review evidence = %q, want execution snapshot only", ctx.Evidence)
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
		return `{"decision":"reject","reason":"not allowed"}`, nil
	})
	events := make(chan Event, 2)
	executor := subtaskExecutor{agent: a, events: events, allowedTools: map[string]bool{"exec": true}, spawnID: "spawn-1"}

	result := executor.ExecuteTool(context.Background(), runner.ToolExecution{ID: "call-1", Name: "exec", Params: map[string]any{"command": "touch x"}})
	if !result.IsError || result.Error == "" {
		t.Fatalf("result = %#v, want reject error", result)
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

	var reviewCount int
	a.guard.SetLLMReviewer(func(ctx context.Context, req guard.ReviewRequest) (string, error) {
		reviewCount++
		return `{"decision":"approve","reason":"approved continuation"}`, nil
	})

	events := make(chan Event, 4)
	call := func(id, path string) <-chan tools.Result {
		result := make(chan tools.Result, 1)
		go func() {
			result <- a.executeTool(context.Background(), runner.ToolExecution{ID: id, Name: "exec", Params: map[string]any{"command": "touch " + path}, Intent: "apply the active related fix"}, events)
		}()
		return result
	}
	first := call("first", t.TempDir()+"/first.txt")
	second := call("second", t.TempDir()+"/second.txt")

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
}

func TestTrimForGuardMiddlePreservesUTF8(t *testing.T) {
	got := trimForGuardMiddle(strings.Repeat("继续修复", 100), 31)
	if !utf8.ValidString(got) {
		t.Fatalf("trimForGuardMiddle() = %q, want valid UTF-8", got)
	}
}

func TestBuildSubtaskGuardReviewContextDoesNotUseMainTaskCard(t *testing.T) {
	a := &Agent{working: testWorkingMemory("main task")}

	// subtask 上下文第一条 user 消息就是任务描述本身（与注入文本同源），
	// 前缀匹配去重后只保留一份；主任务内容不得进入 subtask Evidence。
	ctx := a.buildSubtaskGuardReviewContext(runner.ToolExecution{
		WorkingMessages: []model.Message{model.NewTextMessage(model.RoleUser, "subtask task description")},
	}, "subtask task description")
	if !strings.Contains(ctx.Evidence, "subtask task description") {
		t.Fatalf("subtask evidence = %q, want subtask task description", ctx.Evidence)
	}
	for _, unwanted := range []string{"main task", "main.go", "main approval"} {
		if strings.Contains(ctx.Evidence, unwanted) {
			t.Fatalf("subtask evidence = %q, must not contain %q", ctx.Evidence, unwanted)
		}
	}
}

// 长任务（>64 条消息）时任务描述被扫描窗口挤出，但注入的任务文本仍出现在 Evidence。
func TestBuildSubtaskGuardReviewContextKeepsTaskOnLongRun(t *testing.T) {
	a := &Agent{}
	msgs := make([]model.Message, 0, 70)
	// 第一条是任务描述（user），后续 69 条是 assistant/tool 消息，超过 64 条扫描窗口。
	msgs = append(msgs, model.NewTextMessage(model.RoleUser, "original task in working"))
	for i := 0; i < 69; i++ {
		msgs = append(msgs, model.NewTextMessage(model.RoleAssistant, "step"))
	}
	ctx := a.buildSubtaskGuardReviewContext(runner.ToolExecution{
		WorkingMessages: msgs,
	}, "injected subtask task")
	if !strings.Contains(ctx.Evidence, "injected subtask task") {
		t.Fatalf("long subtask evidence = %q, want injected task description", ctx.Evidence)
	}
	// 长任务时 working 里的原始任务描述（第一条 user 消息）会被 64 条扫描窗口挤出，
	// 这正是注入任务描述要解决的场景：Evidence 必须仍包含用户意图。
	if strings.Contains(ctx.Evidence, "original task in working") {
		t.Fatalf("long subtask evidence = %q, original task should be evicted from window", ctx.Evidence)
	}
}

// 短任务（窗口内已有任务描述）时不重复注入：避免 Latest 和 Earlier 两个 section 相同文本。
func TestBuildSubtaskGuardReviewContextDedupesShortTask(t *testing.T) {
	a := &Agent{}
	msgs := []model.Message{model.NewTextMessage(model.RoleUser, "fix the login bug")}
	ctx := a.buildSubtaskGuardReviewContext(runner.ToolExecution{WorkingMessages: msgs}, "fix the login bug")
	if got := strings.Count(ctx.Evidence, "fix the login bug"); got != 1 {
		t.Fatalf("short subtask evidence contains task %d times, want 1: %q", got, ctx.Evidence)
	}
}

// 长任务描述（含授权范围文件列表）完整注入：不被 180 runes 截断，
// 文件列表必须完整可见，Guard review 才能确认授权范围（修复连环 modify 的核心场景）。
func TestBuildSubtaskGuardReviewContextKeepsTaskFileList(t *testing.T) {
	a := &Agent{}
	// 任务描述超过 180 runes，文件列表在中间（之前会被 trimForGuardMiddle 截断成 [omitted]）。
	task := "严格验证新语义并修到通过。只能修改以下测试文件：" +
		"search-run.test.ts, search-model.test.ts, autosearch-content.test.ts, " +
		"kernel-runtime.test.ts, mutation-observer.test.ts, visibility.test.ts, " +
		"background-sync.test.ts, storage-adapter.test.ts, event-bus.test.ts, " +
		"这些是授权范围，不要修改生产代码。"
	if len([]rune(task)) <= 180 {
		t.Fatalf("test task must exceed 180 runes, got %d", len([]rune(task)))
	}
	msgs := make([]model.Message, 0, 70)
	msgs = append(msgs, model.NewTextMessage(model.RoleUser, task))
	for i := 0; i < 69; i++ {
		msgs = append(msgs, model.NewTextMessage(model.RoleAssistant, "step"))
	}
	ctx := a.buildSubtaskGuardReviewContext(runner.ToolExecution{WorkingMessages: msgs}, task)
	// 文件列表必须完整可见（无截断标记）。
	if strings.Contains(ctx.Evidence, "[omitted]") {
		t.Fatalf("subtask evidence truncated file list: %q", ctx.Evidence)
	}
	for _, f := range []string{"search-run.test.ts", "event-bus.test.ts", "storage-adapter.test.ts"} {
		if !strings.Contains(ctx.Evidence, f) {
			t.Fatalf("subtask evidence missing %q: %q", f, ctx.Evidence)
		}
	}
}
