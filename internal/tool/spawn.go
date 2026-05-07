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
	return "创建子 agent 执行子任务。仅主 agent 可用。子 agent 拥有独立上下文和受限工具集。"
}
func (s *Spawn) Category() Category { return Communicate }
func (s *Spawn) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task":    map[string]any{"type": "string", "description": "子任务描述"},
			"model":   map[string]any{"type": "string", "description": "使用的模型名称"},
			"system":  map[string]any{"type": "string", "description": "子 agent 的系统提示词"},
			"tools":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "子 agent 可用的工具列表"},
			"timeout": map[string]any{"type": "integer", "description": "超时秒数（默认300）"},
			"context": map[string]any{"type": "string", "description": "传给子 agent 的额外上下文"},
		},
		"required": []string{"task"},
	}
}

func (s *Spawn) Execute(ctx context.Context, params map[string]any) Result {
	task, _ := params["task"].(string)
	if task == "" {
		return ErrorResult("task is required")
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
		tools = []string{"readfile", "listdir", "readhttp", "exec"}
	}

	for _, t := range tools {
		if t == "spawn" {
			return ErrorResult("sub agent cannot use spawn tool (nesting not allowed)")
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
