package core

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
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
	if a.modelRef != "" && (name == "askuser" || name == "spawn") {
		return tool.ErrorResult("sub-agent cannot use " + name)
	}

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
		if result.Decision == guard.Confirm || result.Decision == guard.Modify {
			if !a.confirmGuard(ctx, name, params, result, events) {
				return tool.ErrorResult("blocked: user rejected guard confirmation")
			}
		}
	}

	result := t.Execute(ctx, params)

	if !result.IsError {
		result.Content = guard.MaskSensitiveContent(result.Content)
	}

	return result
}

func (a *Agent) confirmGuard(ctx context.Context, name string, params map[string]any, result *guard.GuardResult, events chan<- Event) bool {
	replyCh := make(chan string, 1)
	events <- Event{
		Type:            EventGuardConfirm,
		GuardTool:       name,
		GuardParams:     params,
		GuardRisk:       guard.RiskString(result.Risk),
		GuardReason:     result.Reason,
		GuardSuggestion: result.Suggestion,
		Reply:           replyCh,
	}
	select {
	case <-ctx.Done():
		return false
	case decision := <-replyCh:
		return strings.EqualFold(strings.TrimSpace(decision), "approve")
	}
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
	task = strings.TrimSpace(task)
	if task == "" {
		return tool.ErrorResult("task is required")
	}

	modelRef, _ := params["model"].(string)
	modelRef = strings.TrimSpace(modelRef)
	if modelRef == "" {
		return tool.ErrorResult("spawn requires explicit model. Choose one of: " + strings.Join(a.availableModelRefs(), ", "))
	}
	if a.router == nil {
		return tool.ErrorResult("spawn requires configured models, but no model router is available")
	}
	if _, err := a.router.Provider(modelRef); err != nil {
		return tool.ErrorResult(fmt.Sprintf("invalid spawn model %q. Choose one of: %s", modelRef, strings.Join(a.availableModelRefs(), ", ")))
	}

	timeout := 300
	if t, ok := params["timeout"].(float64); ok && int(t) > 0 {
		timeout = int(t)
	}

	toolNames := parseStringList(params["tools"])
	if len(toolNames) == 0 {
		return tool.ErrorResult("spawn requires explicit tools. Tools are permissions; choose least privilege from: " + strings.Join(a.availableSpawnTools(), ", "))
	}

	subRegistry := tool.NewRegistry()
	seen := make(map[string]bool, len(toolNames))
	for _, name := range toolNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		if name == "spawn" {
			return tool.ErrorResult("sub agent cannot spawn (nesting not allowed)")
		}
		if t, ok := a.registry.Get(name); ok {
			subRegistry.Register(t)
			continue
		}
		if name == "askuser" {
			return tool.ErrorResult("askuser is not available to sub-agents; the main agent should ask the user directly")
		}
		return tool.ErrorResult(fmt.Sprintf("invalid spawn tool %q. Choose from: %s", name, strings.Join(a.availableSpawnTools(), ", ")))
	}
	if len(subRegistry.Names()) == 0 {
		return tool.ErrorResult("spawn requires at least one valid tool permission")
	}

	sessionID := uuid.New().String()
	sub := &Agent{
		cfg:                  a.cfg,
		router:               a.router,
		registry:             subRegistry,
		guard:                a.newGuardForSession(sessionID),
		working:              memory.NewWorkingMemory(),
		episodic:             a.episodic,
		semantic:             a.semantic,
		prompts:              a.prompts,
		sessionID:            sessionID,
		modelRef:             modelRef,
		systemPromptOverride: "",
	}

	// 使用模板渲染 sub-agent system prompt
	toolDescs := toolNames
	toolsSummary := strings.Join(toolDescs, ", ")
	extraCtx, _ := params["context"].(string)

	env := getEnvInfo()

	spawnPrompt, err := a.prompts.RenderSpawnSystem(prompt.SpawnPromptData{
		Task:    task,
		Tools:   toolsSummary,
		Context: extraCtx,
		OS:      env["OS"],
		Arch:    env["Arch"],
		WorkDir: env["WorkDir"],
	})
	if err == nil && spawnPrompt != "" {
		sub.systemPromptOverride = spawnPrompt
	} else {
		// 降级: 使用 LLM 传入的 system 参数
		if sys, ok := params["system"].(string); ok && sys != "" {
			sub.systemPromptOverride = sys
		}
	}
	if sub.systemPromptOverride == "" {
		sub.systemPromptOverride = fmt.Sprintf("You are a Suna sub-agent. Complete this task and return a concise result.\n\nTask:\n%s\n\nTools:\n%s", task, toolsSummary)
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

func parseStringList(value any) []string {
	switch v := value.(type) {
	case []any:
		items := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				items = append(items, strings.TrimSpace(s))
			}
		}
		return items
	case []string:
		items := make([]string, 0, len(v))
		for _, s := range v {
			if strings.TrimSpace(s) != "" {
				items = append(items, strings.TrimSpace(s))
			}
		}
		return items
	default:
		return nil
	}
}

func (a *Agent) availableModelRefs() []string {
	if a.router == nil {
		return nil
	}
	refs := a.router.ListProviders()
	sort.Strings(refs)
	return refs
}

func (a *Agent) availableSpawnTools() []string {
	if a.registry == nil {
		return nil
	}
	names := a.registry.Names()
	filtered := names[:0]
	for _, name := range names {
		if name != "spawn" {
			filtered = append(filtered, name)
		}
	}
	sort.Strings(filtered)
	return filtered
}
