package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/alanchenchen/suna/internal/guard"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/prompt"
	"github.com/alanchenchen/suna/internal/runner"
	"github.com/alanchenchen/suna/internal/subtask"
	"github.com/alanchenchen/suna/internal/tool"
)

func (a *Agent) ExecuteTool(ctx context.Context, id string, name string, params map[string]any) tool.Result {
	return a.executeTool(ctx, id, name, params, nil)
}

type mainExecutor struct {
	agent  *Agent
	events chan<- Event
}

func (e mainExecutor) ExecuteTool(ctx context.Context, id string, name string, params map[string]any) tool.Result {
	return e.agent.executeTool(ctx, id, name, params, e.events)
}

func (a *Agent) executeTool(ctx context.Context, id string, name string, params map[string]any, events chan<- Event) tool.Result {
	if name == "askuser" {
		return a.executeAskUser(ctx, params, events)
	}
	if name == "spawn" {
		return a.executeSpawn(ctx, id, params, events)
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
			if !a.confirmGuard(ctx, id, name, params, result, events) {
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

func (a *Agent) confirmGuard(ctx context.Context, id string, name string, params map[string]any, result *guard.GuardResult, events chan<- Event) bool {
	if events == nil {
		return false
	}
	replyCh := make(chan string, 1)
	events <- Event{Type: EventGuardConfirm, GuardToolCallID: id, GuardTool: name, GuardParams: params, GuardRisk: guard.RiskString(result.Risk), GuardReason: result.Reason, GuardSuggestion: result.Suggestion, Reply: replyCh}
	select {
	case <-ctx.Done():
		return false
	case decision := <-replyCh:
		return strings.EqualFold(strings.TrimSpace(decision), "approve")
	}
}

func (a *Agent) executeAskUser(ctx context.Context, params map[string]any, events chan<- Event) tool.Result {
	if events == nil {
		return tool.ErrorResult("askuser requires main agent event stream")
	}
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

func (a *Agent) executeSpawn(ctx context.Context, id string, params map[string]any, events chan<- Event) tool.Result {
	if events == nil {
		return tool.ErrorResult("spawn requires main agent event stream")
	}
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
	subRegistry, errResult := a.buildSubtaskRegistry(params["tools"])
	if errResult.IsError {
		return errResult
	}

	extraCtx, _ := params["context"].(string)
	env := getEnvInfo()
	toolsSummary := strings.Join(subRegistry.Names(), ", ")
	subtaskPrompt, err := a.prompts.RenderSubtaskSystem(prompt.SubtaskPromptData{Task: task, Tools: toolsSummary, Context: extraCtx, OS: env["OS"], Arch: env["Arch"], WorkDir: env["WorkDir"]})
	if err != nil || subtaskPrompt == "" {
		if sys, ok := params["system"].(string); ok && sys != "" {
			subtaskPrompt = sys
		}
	}
	if subtaskPrompt == "" {
		subtaskPrompt = fmt.Sprintf("You are an isolated Suna subtask runner. Complete this task and return a concise result.\n\nTask:\n%s\n\nTools:\n%s", task, toolsSummary)
	}

	spawnID := id
	if spawnID == "" {
		spawnID = uuid.New().String()
	}
	r := a.newSubtaskRunner(events, spawnID, subRegistry)
	st := subtask.New(subtask.Request{ID: spawnID, Task: task, ModelRef: modelRef, ModelID: resolveModelID(a.cfg, modelRef), System: subtaskPrompt, Tools: subRegistry, Timeout: time.Duration(timeout) * time.Second})
	res, err := st.Run(ctx, r)
	if err != nil && res.Text == "" {
		res.Text = err.Error()
		res.Status = err.Error()
	}
	out, _ := json.Marshal(map[string]any{"result": res.Text, "success": res.Success, "status": res.Status})
	return tool.TextResult(string(out))
}

func (a *Agent) newSubtaskRunner(events chan<- Event, spawnID string, subRegistry *tool.Registry) *runner.Runner {
	return &runner.Runner{Router: a.router, Compressor: a.compressor, Executor: subtaskExecutor{agent: a, events: events, registry: subRegistry}, Sink: subtaskSink{events: events, spawnID: spawnID}, UsageSink: a, Hooks: runner.Hooks{CleanToolParams: cleanParamsForRegistry(subRegistry)}}
}

type subtaskExecutor struct {
	agent    *Agent
	events   chan<- Event
	registry *tool.Registry
}

func (e subtaskExecutor) ExecuteTool(ctx context.Context, id string, name string, params map[string]any) tool.Result {
	if name == "askuser" || name == "spawn" {
		return tool.ErrorResult("subtask cannot use " + name)
	}
	t, ok := e.registry.Get(name)
	if !ok {
		return tool.ErrorResult(fmt.Sprintf("tool %q not allowed for subtask", name))
	}
	if t.Category() == tool.Act {
		result := e.agent.guard.Check(ctx, name, params)
		if result.Decision == guard.Reject {
			return tool.ErrorResult("blocked: " + result.Reason)
		}
		if result.Decision == guard.Confirm || result.Decision == guard.Modify {
			if !e.agent.confirmGuard(ctx, id, name, params, result, e.events) {
				return tool.ErrorResult("blocked: user rejected guard confirmation")
			}
		}
	}
	res := t.Execute(ctx, params)
	if !res.IsError {
		res.Content = guard.MaskSensitiveContent(res.Content)
	}
	return res
}

type subtaskSink struct {
	events  chan<- Event
	spawnID string
}

func (s subtaskSink) Status(content string)    {}
func (s subtaskSink) Stream(content string)    {}
func (s subtaskSink) Reasoning(content string) {}
func (s subtaskSink) ToolCall(call runner.ToolCallEvent) {
	s.events <- Event{Type: EventToolCall, ToolCallID: s.namespaced(call.ID), ToolName: call.Name, ToolParams: call.Params, ToolIntent: call.Intent}
}
func (s subtaskSink) ToolResult(result runner.ToolResultEvent) {
	s.events <- Event{Type: EventToolResult, ToolCallID: s.namespaced(result.ID), ToolName: result.Name, ToolResult: result.Result, ToolError: result.Error}
}
func (s subtaskSink) namespaced(id string) string {
	return "spawn:" + s.spawnID + ":" + id
}

func (a *Agent) buildSubtaskRegistry(value any) (*tool.Registry, tool.Result) {
	toolNames := parseStringList(value)
	if len(toolNames) == 0 {
		return nil, tool.ErrorResult("spawn requires explicit tools. Tools are permissions; choose least privilege from: " + strings.Join(a.availableSpawnTools(), ", "))
	}
	subRegistry := tool.NewRegistry()
	seen := make(map[string]bool, len(toolNames))
	for _, name := range toolNames {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if name == "spawn" {
			return nil, tool.ErrorResult("subtask cannot spawn (nesting not allowed)")
		}
		if name == "askuser" {
			return nil, tool.ErrorResult("askuser is not available to subtasks; the main agent should ask the user directly")
		}
		if t, ok := a.registry.Get(name); ok {
			subRegistry.Register(t)
			continue
		}
		return nil, tool.ErrorResult(fmt.Sprintf("invalid spawn tool %q. Choose from: %s", name, strings.Join(a.availableSpawnTools(), ", ")))
	}
	if len(subRegistry.Names()) == 0 {
		return nil, tool.ErrorResult("spawn requires at least one valid tool permission")
	}
	return subRegistry, tool.Result{}
}

func cleanParamsForRegistry(registry *tool.Registry) func(string, map[string]any) (map[string]any, string) {
	return func(name string, params map[string]any) (map[string]any, string) {
		intent := consumeToolIntent(params)
		if registry == nil {
			return params, intent
		}
		t, ok := registry.Get(name)
		if !ok {
			return params, intent
		}
		return filterParams(params, schemaPropertyKeys(t.Parameters())), intent
	}
}

func (a *Agent) cleanToolParams(name string, params map[string]any) (map[string]any, string) {
	intent := consumeToolIntent(params)
	allowed := a.toolParamKeys(name)
	if len(allowed) == 0 {
		return params, intent
	}
	return filterParams(params, allowed), intent
}

func filterParams(params map[string]any, allowed map[string]bool) map[string]any {
	clean := make(map[string]any, len(params))
	for k, v := range params {
		if allowed[k] {
			clean[k] = v
		}
	}
	return clean
}

func (a *Agent) toolParamKeys(name string) map[string]bool {
	if t, ok := a.registry.Get(name); ok {
		return schemaPropertyKeys(t.Parameters())
	}
	switch name {
	case "askuser":
		return map[string]bool{"question": true, "options": true}
	case "spawn":
		return map[string]bool{"task": true, "model": true, "system": true, "tools": true, "timeout": true, "context": true}
	default:
		return nil
	}
}

func schemaPropertyKeys(schema map[string]any) map[string]bool {
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		return nil
	}
	keys := make(map[string]bool, len(props))
	for k := range props {
		keys[k] = true
	}
	return keys
}

func consumeToolIntent(params map[string]any) string {
	if params == nil {
		return ""
	}
	intent, _ := params["intent"].(string)
	delete(params, "intent")
	return strings.TrimSpace(intent)
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

func (a *Agent) guardLLMReview(ctx context.Context, toolName string, paramsJSON string, target string, recentCtx string) (string, error) {
	reviewPrompt, err := a.prompts.RenderGuardReview(prompt.GuardReviewData{ToolName: toolName, ToolParams: paramsJSON, Target: target, RecentContext: recentCtx})
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
	req := &model.CompletionRequest{Model: modelRef, Purpose: "guard_review", RequestID: uuid.New().String(), System: "Reply with JSON only.", Messages: []model.Message{model.NewTextMessage(model.RoleUser, reviewPrompt)}, MaxTokens: 100}
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
