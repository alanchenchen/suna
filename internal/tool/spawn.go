package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type Spawn struct {
	handler SpawnHandler
}

type SpawnHandler func(ctx context.Context, params map[string]any) (string, bool, error)

func NewSpawn(handler SpawnHandler) *Spawn {
	return &Spawn{handler: handler}
}

func (s *Spawn) Name() string { return "spawn" }
func (s *Spawn) Description() string {
	return "Create an isolated subtask runtime to execute a self-contained task. Only available to the main agent. Model and tools must be explicitly selected; tools are permissions."
}
func (s *Spawn) Category() Category { return Communicate }
func (s *Spawn) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task":    map[string]any{"type": "string", "description": "Sub-task description"},
			"model":   map[string]any{"type": "string", "description": "Exact model ref to use"},
			"system":  map[string]any{"type": "string", "description": "Fallback system prompt for the subtask"},
			"tools":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Explicit tool permissions for the subtask"},
			"timeout": map[string]any{"type": "integer", "description": "Timeout in seconds (default 300)"},
			"context": map[string]any{"type": "string", "description": "Extra context for the subtask"},
		},
		"required": []string{"task", "model", "tools"},
	}
}

func (s *Spawn) Execute(ctx context.Context, params map[string]any) Result {
	task, _ := params["task"].(string)
	if task == "" {
		return ErrorResult("task is required")
	}
	model, _ := params["model"].(string)
	if model == "" {
		return ErrorResult("spawn requires explicit model")
	}

	timeout := 300
	if t, ok := params["timeout"].(float64); ok && int(t) > 0 {
		timeout = int(t)
	}

	var tools []string
	if t, ok := params["tools"].([]any); ok {
		for _, v := range t {
			if s, ok := v.(string); ok {
				tools = append(tools, s)
			}
		}
	}

	if len(tools) == 0 {
		return ErrorResult("spawn requires explicit tools")
	}

	for _, t := range tools {
		if t == "spawn" {
			return ErrorResult("subtask cannot use spawn tool (nesting not allowed)")
		}
	}

	spawnCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	if s.handler == nil {
		return ErrorResult("no spawn handler configured")
	}

	result, success, err := s.handler(spawnCtx, params)
	if err != nil {
		return ErrorResult(fmt.Sprintf("spawn failed: %s", err))
	}

	output := map[string]any{
		"result":  result,
		"success": success,
	}
	bytes, _ := json.Marshal(output)
	return TextResult(string(bytes))
}
