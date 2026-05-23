package agent

import (
	"context"
	"testing"

	"github.com/alanchenchen/suna/internal/guard"
	"github.com/alanchenchen/suna/internal/tool"
)

func TestSubtaskReadFileBlocksSensitivePath(t *testing.T) {
	registry := tool.NewRegistry()
	registry.Register(tool.ReadFile{})
	a := &Agent{guard: guard.NewGuardWithMode(nil, "test", guard.ModeAuto)}
	executor := subtaskExecutor{agent: a, registry: registry}

	result := executor.ExecuteTool(context.Background(), "call-1", "readfile", map[string]any{"path": ".env"})
	if !result.IsError || result.Error == "" {
		t.Fatalf("subtask readfile .env result = %#v, want error", result)
	}
}
