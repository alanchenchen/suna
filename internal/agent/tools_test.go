package agent

import (
	"context"
	"testing"

	"github.com/alanchenchen/suna/internal/guard"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/runner"
	"github.com/alanchenchen/suna/internal/tool"
)

func TestSubtaskReadFileBlocksSensitivePath(t *testing.T) {
	registry := tool.NewRegistry()
	registry.Register(tool.ReadFile{})
	a := &Agent{guard: guard.NewGuardWithMode(nil, "test", guard.ModeAuto)}
	executor := subtaskExecutor{agent: a, registry: registry}

	result := executor.ExecuteTool(context.Background(), runner.ToolExecution{ID: "call-1", Name: "readfile", Params: map[string]any{"path": ".env"}})
	if !result.IsError || result.Error == "" {
		t.Fatalf("subtask readfile .env result = %#v, want error", result)
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
	registry := tool.NewRegistry()
	registry.Register(tool.WriteFile{})
	a := &Agent{guard: guard.NewGuardWithMode(nil, "test", guard.ModeSmart)}
	a.guard.SetLLMReviewer(func(ctx context.Context, req guard.ReviewRequest) (string, error) {
		return `{"decision":"modify","reason":"too broad","suggestion":"narrow it"}`, nil
	})
	events := make(chan Event, 2)
	executor := subtaskExecutor{agent: a, events: events, registry: registry, spawnID: "spawn-1"}

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
	case <-context.Background().Done():
		t.Fatal("unreachable")
	default:
		t.Fatal("missing guard event")
	}
}

func testWorkingMemory(userText string) *memory.WorkingMemory {
	w := memory.NewWorkingMemory()
	w.AddMessage(model.NewTextMessage(model.RoleUser, userText))
	return w
}
