package subtask

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/runner"
	"github.com/alanchenchen/suna/internal/tools"
)

func TestToolDefsReturnsAllowedToolDefinitions(t *testing.T) {
	st := New(Request{ToolDefs: []model.ToolDef{{
		Name:        "readfile",
		Description: "read a file",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}},
	}}})

	defs := st.toolDefs()
	if len(defs) != 1 || defs[0].Name != "readfile" {
		t.Fatalf("toolDefs = %#v, want readfile", defs)
	}
	props := defs[0].Parameters["properties"].(map[string]any)
	props["path"] = map[string]any{"type": "number"}

	again := st.toolDefs()
	againProps := again[0].Parameters["properties"].(map[string]any)
	path := againProps["path"].(map[string]any)
	if path["type"] != "string" {
		t.Fatalf("toolDefs aliases request schema, path type = %v", path["type"])
	}
}

func TestRunRequiresBinding(t *testing.T) {
	result, err := New(Request{Task: "test"}).Run(context.Background(), nil)
	if err == nil {
		t.Fatal("Run() error = nil, want missing binding error")
	}
	if result.Status != StatusFailed || result.Error != err.Error() {
		t.Fatalf("Run() result = %#v, want failed result matching error", result)
	}
}

func TestRunInjectsBindingIntoContext(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if requests.Add(1) == 1 {
			_, _ = fmt.Fprint(w, "data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"type\":\"function\",\"function\":{\"name\":\"probe\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		_, _ = fmt.Fprint(w, "data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"{\\\"result\\\":\\\"done\\\",\\\"side_effects\\\":{\\\"status\\\":\\\"none\\\"}}\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()

	cfg := &config.Config{Models: []config.ModelConfig{{Provider: "test", Model: "test", BaseURL: server.URL, ContextWindow: 128000, MaxOutputTokens: 1024, APIKey: "test-key"}}}
	router, err := model.NewRouter(cfg, nil)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	binding, err := router.Bind("test/test")
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	executor := bindingContextExecutor{}
	result, err := New(Request{Task: "test", Binding: binding, ToolDefs: []model.ToolDef{{Name: "probe", Parameters: map[string]any{"type": "object"}}}}).Run(context.Background(), &runner.Runner{Executor: &executor})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != StatusCompleted || !executor.called {
		t.Fatalf("Run() result = %#v, executor called = %v, want completed execution", result, executor.called)
	}
	if executor.got != binding {
		t.Fatalf("binding in tool context = %p, want request binding %p", executor.got, binding)
	}
}

type bindingContextExecutor struct {
	got    *model.ModelBinding
	called bool
}

func (e *bindingContextExecutor) ExecuteTool(ctx context.Context, _ runner.ToolExecution) tools.Result {
	e.called = true
	e.got = model.BindingFromContext(ctx)
	return tools.TextResult("ok")
}

func TestParseFinalResultStructuredSideEffects(t *testing.T) {
	got := parseFinalResult(`{"result":"done","side_effects":{"status":"remaining","summary":"modified requested files","paths":["a.txt"]}}`)
	if got.Status != StatusCompleted {
		t.Fatalf("Status = %q, want %q", got.Status, StatusCompleted)
	}
	if got.Text != "done" {
		t.Fatalf("Text = %q, want done", got.Text)
	}
	if got.SideEffects.Status != SideEffectsRemaining {
		t.Fatalf("SideEffects.Status = %q, want %q", got.SideEffects.Status, SideEffectsRemaining)
	}
	if len(got.SideEffects.Paths) != 1 || got.SideEffects.Paths[0] != "a.txt" {
		t.Fatalf("SideEffects.Paths = %#v, want [a.txt]", got.SideEffects.Paths)
	}
}

func TestParseFinalResultUnstructuredMarksUnknown(t *testing.T) {
	got := parseFinalResult("plain answer")
	if got.Status != StatusCompletedUnstructured {
		t.Fatalf("Status = %q, want %q", got.Status, StatusCompletedUnstructured)
	}
	if got.Text != "plain answer" {
		t.Fatalf("Text = %q, want plain answer", got.Text)
	}
	if got.SideEffects.Status != SideEffectsUnknown {
		t.Fatalf("SideEffects.Status = %q, want %q", got.SideEffects.Status, SideEffectsUnknown)
	}
}

func TestParseFinalResultUnsupportedSideEffectsStatusMarksUnknown(t *testing.T) {
	got := parseFinalResult(`{"result":"done","side_effects":{"status":"maybe","summary":"custom"}}`)
	if got.Status != StatusCompleted {
		t.Fatalf("Status = %q, want %q", got.Status, StatusCompleted)
	}
	if got.SideEffects.Status != SideEffectsUnknown {
		t.Fatalf("SideEffects.Status = %q, want %q", got.SideEffects.Status, SideEffectsUnknown)
	}
	if got.SideEffects.Summary == "custom" || got.SideEffects.Summary == "" {
		t.Fatalf("SideEffects.Summary = %q, want unsupported status note plus original summary", got.SideEffects.Summary)
	}
}

func TestFailedResultUsesToolCallToChooseSideEffects(t *testing.T) {
	withoutTool := failedResult("boom", false)
	if withoutTool.Status != StatusFailed || withoutTool.SideEffects.Status != SideEffectsNone {
		t.Fatalf("without tool = %#v, want failed with none side effects", withoutTool)
	}
	withTool := failedResult("boom", true)
	if withTool.Status != StatusFailed || withTool.SideEffects.Status != SideEffectsUnknown {
		t.Fatalf("with tool = %#v, want failed with unknown side effects", withTool)
	}
}

func TestParseFinalResultAcceptsFencedJSON(t *testing.T) {
	got := parseFinalResult("```json\n{\"result\":\"done\",\"side_effects\":{\"status\":\"none\"}}\n```")
	if got.Status != StatusCompleted {
		t.Fatalf("Status = %q, want %q", got.Status, StatusCompleted)
	}
	if got.Text != "done" || got.SideEffects.Status != SideEffectsNone {
		t.Fatalf("result = %#v, want done with no side effects", got)
	}
}
