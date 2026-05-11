package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/alanchenchen/suna/internal/guard"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/prompt"
	"github.com/alanchenchen/suna/internal/tool"
)

// guardLLMReview 是注入到 Guard 的 LLM 审查函数。
// 使用 active model 做简短的意图判断，控制成本（<50 tokens）。
func (a *Agent) guardLLMReview(ctx context.Context, toolName string, paramsJSON string, target string, recentCtx string) (string, error) {
	reviewPrompt, err := a.prompts.RenderGuardReview(prompt.GuardReviewData{
		ToolName:      toolName,
		ToolParams:    paramsJSON,
		Target:        target,
		RecentContext: recentCtx,
	})
	if err != nil {
		return "", err
	}
	_, modelRef, err := a.router.Route(ctx, "")
	if err != nil {
		return "", err
	}
	p, err := a.router.Provider(modelRef)
	if err != nil {
		return "", err
	}
	req := &model.CompletionRequest{
		Model:     modelRef,
		System:    "Reply with JSON only.",
		Messages:  []model.Message{model.NewTextMessage(model.RoleUser, reviewPrompt)},
		MaxTokens: 100,
	}
	ch, err := p.Complete(ctx, req)
	if err != nil {
		return "", err
	}
	var result string
	for chunk := range ch {
		if chunk.Content != "" {
			result += chunk.Content
		}
	}
	return result, nil
}

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

	// 智能路由：一次 LLM 调用同时决定模型和工具
	var modelRef string
	if explicitRef, ok := params["model"].(string); ok && explicitRef != "" {
		modelRef = explicitRef
	} else {
		routeResult, err := a.router.RouteWithLLM(ctx, task, "")
		if err == nil && routeResult != nil {
			modelRef = routeResult.ModelRef
			// 路由推荐了工具且 main 没指定时，采用推荐
			if len(toolNames) == 0 && len(routeResult.Tools) > 0 {
				for _, t := range routeResult.Tools {
					if t != "spawn" {
						toolNames = append(toolNames, t)
					}
				}
			}
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
		modelRef:  modelRef,
	}

	// 使用模板渲染 sub-agent system prompt
	toolDescs := toolNames
	toolsSummary := fmt.Sprintf("You have access to: %s", strings.Join(toolDescs, ", "))
	extraCtx, _ := params["context"].(string)
	parentTask := ""
	if len(a.working.Messages()) > 0 {
		parentTask = task
	}

	var modelInfo string
	if mc, err := a.router.ModelConfig(modelRef); err == nil {
		modelInfo = fmt.Sprintf("%s (%s)", modelRef, strings.Join(mc.Strengths, ", "))
	}

	spawnPrompt, err := a.prompts.RenderSpawnSystem(prompt.SpawnPromptData{
		Task:       task,
		Tools:      toolsSummary,
		Context:    extraCtx,
		ModelInfo:  modelInfo,
		ParentTask: parentTask,
	})
	if err == nil && spawnPrompt != "" {
		sub.working.AddMessage(model.NewTextMessage(model.RoleSystem, spawnPrompt))
	} else {
		// 降级: 使用 LLM 传入的 system 参数
		if sys, ok := params["system"].(string); ok && sys != "" {
			sub.working.AddMessage(model.NewTextMessage(model.RoleSystem, sys))
		}
	}
	if extraCtx != "" {
		sub.working.AddMessage(model.NewTextMessage(model.RoleSystem, "Additional context:\n"+extraCtx))
	}

	subCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	subEvents := sub.Run(subCtx, task)
	success := true
	var statusText string
	for evt := range subEvents {
		if evt.Type == EventStatus && strings.HasPrefix(evt.Content, "error:") {
			success = false
			statusText = evt.Content
		}
	}

	msgs := sub.working.Messages()
	var result string
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == model.RoleAssistant && msgs[i].Text() != "" {
			result = msgs[i].Text()
			break
		}
	}

	if subCtx.Err() != nil {
		success = false
		statusText = subCtx.Err().Error()
	}
	if result == "" && statusText != "" {
		result = statusText
	}
	if result == "" {
		success = false
		statusText = "sub agent returned no answer"
		result = statusText
	}
	out, _ := json.Marshal(map[string]any{"result": result, "success": success, "status": statusText})
	return tool.TextResult(string(out))
}
