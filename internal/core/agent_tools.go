package core

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/alanchenchen/suna/internal/guard"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/tool"
)

// executeTool 执行单个工具调用
// 包含敏感文件拦截和内容脱敏逻辑
func (a *Agent) executeTool(ctx context.Context, name string, params map[string]any, events chan<- Event) tool.Result {
	if name == "askuser" {
		return a.executeAskUser(ctx, params, events)
	}

	if name == "spawn" {
		return a.executeSpawn(ctx, params)
	}

	t, ok := a.registry.Get(name)
	if !ok {
		return tool.ErrorResult(fmt.Sprintf("tool %q not found", name))
	}

	if name == "readfile" {
		if path, ok := params["path"].(string); ok {
			if sensitive, reason := guard.IsSensitivePath(path); sensitive {
				return tool.ErrorResult(fmt.Sprintf("blocked: sensitive file (%s). Reading credential/secret files is not allowed.", reason))
			}
		}
	}

	if t.Category() == tool.Act {
		result := a.guard.Check(ctx, name, params)
		if result.Decision == guard.Reject {
			return tool.ErrorResult("blocked: " + result.Reason)
		}
	}

	result := t.Execute(ctx, params)

	if !result.IsError {
		result.Content = guard.MaskSensitiveContent(result.Content)
	}

	return result
}

func (a *Agent) executeAskUser(ctx context.Context, params map[string]any, events chan<- Event) tool.Result {
	question, _ := params["question"].(string)
	if question == "" {
		return tool.ErrorResult("question is required")
	}
	var options []string
	if o, ok := params["options"].([]any); ok {
		for _, v := range o {
			if s, ok := v.(string); ok {
				options = append(options, s)
			}
		}
	}
	replyCh := make(chan string, 1)
	events <- Event{Type: EventAskUser, Question: question, Options: options, Reply: replyCh}
	select {
	case <-ctx.Done():
		return tool.ErrorResult("cancelled")
	case answer := <-replyCh:
		b, _ := json.Marshal(map[string]string{"answer": answer})
		return tool.TextResult(string(b))
	}
}

// executeSpawn 创建子 agent 执行子任务
func (a *Agent) executeSpawn(ctx context.Context, params map[string]any) tool.Result {
	task, _ := params["task"].(string)
	if task == "" {
		return tool.ErrorResult("task is required")
	}

	timeout := 300
	if t, ok := params["timeout"].(float64); ok && int(t) > 0 {
		timeout = int(t)
	}

	var toolNames []string
	if t, ok := params["tools"].([]any); ok {
		for _, v := range t {
			if s, ok := v.(string); ok {
				toolNames = append(toolNames, s)
			}
		}
	}
	if len(toolNames) == 0 {
		toolNames = []string{"readfile", "listdir", "readhttp", "exec"}
	}

	for _, t := range toolNames {
		if t == "spawn" {
			return tool.ErrorResult("sub agent cannot spawn (nesting not allowed)")
		}
	}

	subRegistry := tool.NewRegistry()
	for _, name := range toolNames {
		if t, ok := a.registry.Get(name); ok {
			subRegistry.Register(t)
		}
	}

	sessionID := uuid.New().String()
	sub := &Agent{
		cfg:       a.cfg,
		router:    a.router,
		registry:  subRegistry,
		guard:     guard.NewGuard(nil, sessionID),
		working:   memory.NewWorkingMemory(),
		episodic:  a.episodic,
		semantic:  a.semantic,
		prompts:   a.prompts,
		sessionID: sessionID,
	}

	if sys, ok := params["system"].(string); ok && sys != "" {
		sub.working.AddMessage(model.NewTextMessage(model.RoleSystem, sys))
	}
	if extra, ok := params["context"].(string); ok && extra != "" {
		sub.working.AddMessage(model.NewTextMessage(model.RoleSystem, "Additional context:\n"+extra))
	}

	subCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	subEvents := sub.Run(subCtx, task)
	for range subEvents {
	}

	msgs := sub.working.Messages()
	var result string
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == model.RoleAssistant && msgs[i].Text() != "" {
			result = msgs[i].Text()
			break
		}
	}

	out, _ := json.Marshal(map[string]any{"result": result, "success": true})
	return tool.TextResult(string(out))
}
