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
	"github.com/alanchenchen/suna/internal/logging"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/prompt"
	"github.com/alanchenchen/suna/internal/runner"
	"github.com/alanchenchen/suna/internal/subtask"
	"github.com/alanchenchen/suna/internal/tools"
	"github.com/alanchenchen/suna/internal/tools/agenttools"
	"github.com/alanchenchen/suna/internal/tools/skilltools"
)

func (a *Agent) ExecuteTool(ctx context.Context, id string, name string, params map[string]any) tools.Result {
	return a.executeTool(ctx, runner.ToolExecution{ID: id, Name: name, Params: params}, nil)
}

type mainExecutor struct {
	agent  *Agent
	events chan<- Event
}

func (e mainExecutor) ExecuteTool(ctx context.Context, call runner.ToolExecution) tools.Result {
	return e.agent.executeTool(ctx, call, e.events)
}

func (a *Agent) executeTool(ctx context.Context, call runner.ToolExecution, events chan<- Event) tools.Result {
	id, name, params := call.ID, call.Name, call.Params
	if _, ok := a.tools.Get(name); !ok {
		return tools.ErrorResult(fmt.Sprintf("tool %q not found", name))
	}
	if name == skilltools.ToolLoad && events != nil {
		skillName, _ := params["name"].(string)
		if strings.TrimSpace(skillName) != "" {
			events <- Event{Type: EventSkillLoad, SkillName: strings.TrimSpace(skillName), SkillLoadStatus: "loading"}
		}
	}
	if a.shouldGuardTool(name) {
		// Guard 决策必须按顺序完成，确保用户刚刚作出的确认或拒绝先写入任务事实。
		// gate 只覆盖 review/确认阶段；通过后立即释放，工具执行仍由 Runner 并发调度。
		a.guardGate.Lock()
		result := a.guard.Check(ctx, name, params, a.buildGuardReviewContext(call))
		a.emitToolGuard(events, id, name, result)
		allowed := true
		if result.Decision == guard.Reject {
			allowed = false
		} else if result.Decision == guard.Modify {
			a.guardGate.Unlock()
			return guardModifyResult(result)
		} else if result.Decision == guard.Confirm {
			allowed = a.confirmGuard(ctx, id, call, result, events, true)
		}
		a.guardGate.Unlock()
		if !allowed {
			if result.Decision == guard.Reject {
				return tools.ErrorResult("blocked: " + result.Reason)
			}
			return tools.ErrorResult("blocked: user rejected guard confirmation")
		}
	}
	if err := sensitiveReadError(name, params); err != "" {
		return tools.ErrorResult(err)
	}
	execCtx := ctx
	if name == skilltools.ToolLoad || name == skilltools.ToolStart {
		execCtx = contextWithSkillRuntime(ctx, a, events)
		if name == skilltools.ToolLoad {
			execCtx = skilltools.WithProjectCatalog(execCtx, a.ProjectSkills())
		}
	}
	if name == agenttools.ToolAskUser || name == agenttools.ToolSpawn {
		execCtx = agenttools.WithEvents(ctx, events)
		execCtx = agenttools.WithRuntime(execCtx, a)
	}
	result := a.tools.Execute(execCtx, tools.Call{ID: id, Name: name, Params: params, Intent: call.Intent, AssistantContext: call.AssistantContext})
	if events != nil {
		if skillName, ok := skilltools.LoadNotificationFromResult(name, params, result); ok {
			events <- Event{Type: EventSkillLoad, SkillName: skillName, SkillLoadStatus: "loaded"}
		}
	}
	if name == agenttools.ToolAskUser || name == agenttools.ToolSpawn || name == skilltools.ToolLoad || name == skilltools.ToolStart {
		return result
	}
	if !result.IsError {
		result.Content = guard.MaskSensitiveContent(result.Content)
	}
	return result
}

func guardModifyResult(result *guard.GuardResult) tools.Result {
	msg := "guard requested a safer modified tool call"
	if strings.TrimSpace(result.Reason) != "" {
		msg += ": " + result.Reason
	}
	if strings.TrimSpace(result.Suggestion) != "" {
		msg += "\nsuggestion: " + result.Suggestion
	}
	return tools.ErrorResult(msg)
}

func (a *Agent) emitToolGuard(events chan<- Event, id string, name string, result *guard.GuardResult) {
	if events == nil || result == nil {
		return
	}
	// Guard 决策是工具执行前的安全来源，单独发事件，避免混入工具执行结果 metadata。
	events <- Event{Type: EventToolGuard, GuardToolCallID: id, GuardTool: name, GuardRisk: guard.RiskString(result.Risk), GuardDecision: string(result.Decision), GuardSource: result.Source, GuardReason: result.Reason, GuardSuggestion: result.Suggestion, GuardReviewCode: result.ReviewCode, GuardReviewMsg: result.ReviewMessage}
}

func (a *Agent) confirmGuard(ctx context.Context, id string, call runner.ToolExecution, result *guard.GuardResult, events chan<- Event, recordTaskDecision bool) bool {
	name, params := call.Name, call.Params
	if events == nil {
		return false
	}
	replyCh := make(chan string, 1)
	runID := runIDFromContext(ctx)
	a.SetSteeringInteractionPending(runID, true)
	defer a.SetSteeringInteractionPending(runID, false)
	events <- Event{Type: EventGuardConfirm, GuardToolCallID: id, GuardTool: name, GuardParams: params, GuardRisk: guard.RiskString(result.Risk), GuardReason: result.Reason, GuardSuggestion: result.Suggestion, GuardReviewCode: result.ReviewCode, GuardReviewMsg: result.ReviewMessage, Reply: replyCh}
	select {
	case <-ctx.Done():
		return false
	case decision := <-replyCh:
		approved := strings.EqualFold(strings.TrimSpace(decision), "approve")
		finalDecision := guard.Reject
		finalReason := "user rejected guard confirmation"
		if approved {
			finalDecision = guard.Approve
			finalReason = result.Reason
		}
		if recordTaskDecision {
			a.recordGuardTaskReceipt(call, result, approved)
		}
		// 用户确认会覆盖前置 LLM/兜底 Guard 行，让历史工具块记录最终批准来源。
		events <- Event{Type: EventToolGuard, GuardToolCallID: id, GuardTool: name, GuardRisk: guard.RiskString(result.Risk), GuardDecision: string(finalDecision), GuardSource: "user", GuardReason: finalReason, GuardSuggestion: result.Suggestion}
		return approved
	}
}

func runIDFromContext(ctx context.Context) string {
	execCtx, _ := tools.ExecutionContextFrom(ctx)
	return execCtx.RunID
}

func (a *Agent) ExecuteAskUserTool(ctx context.Context, params map[string]any) tools.Result {
	events, _ := agenttools.Events(ctx).(chan<- Event)
	if events == nil {
		return tools.ErrorResult("askuser requires main agent event stream")
	}
	question, _ := params["question"].(string)
	if question == "" {
		return tools.ErrorResult("question is required")
	}
	var options []string
	if o, ok := params["options"].([]any); ok {
		for _, v := range o {
			if s, ok := v.(string); ok {
				options = append(options, s)
			}
		}
	}
	allowCustom := true
	if v, ok := params["allow_custom"].(bool); ok {
		allowCustom = v
	}
	replyCh := make(chan string, 1)
	runID := runIDFromContext(ctx)
	a.SetSteeringInteractionPending(runID, true)
	defer a.SetSteeringInteractionPending(runID, false)
	events <- Event{Type: EventAskUser, Question: question, Options: options, AllowCustom: allowCustom, Reply: replyCh}
	select {
	case <-ctx.Done():
		return tools.ErrorResult("cancelled")
	case answer := <-replyCh:
		b, _ := json.Marshal(map[string]string{"answer": answer})
		return tools.TextResult(string(b))
	}
}

func (a *Agent) ExecuteSpawnTool(ctx context.Context, id string, params map[string]any) tools.Result {
	events, _ := agenttools.Events(ctx).(chan<- Event)
	if events == nil {
		return tools.ErrorResult("spawn requires main agent event stream")
	}
	task, _ := params["task"].(string)
	task = strings.TrimSpace(task)
	if task == "" {
		return tools.ErrorResult("task is required")
	}
	modelRef, _ := params["model"].(string)
	modelRef = strings.TrimSpace(modelRef)
	if modelRef == "" {
		return tools.ErrorResult("spawn requires explicit model. Choose one of: " + strings.Join(a.availableModelRefs(), ", "))
	}
	if a.router == nil {
		return tools.ErrorResult("spawn requires configured models, but no model router is available")
	}
	if _, err := a.router.Bind(modelRef); err != nil {
		return tools.ErrorResult(fmt.Sprintf("invalid spawn model %q. Choose one of: %s", modelRef, strings.Join(a.availableModelRefs(), ", ")))
	}
	if !a.router.IsSpawnableModel(a.modelRef, modelRef) {
		return tools.ErrorResult(fmt.Sprintf("spawn model %q is not available for session model %q. Choose one of: %s", modelRef, a.modelRef, strings.Join(a.availableModelRefs(), ", ")))
	}
	allowedTools, toolNames, errResult := a.buildSubtaskAllowedTools(params["tools"])
	if errResult.IsError {
		return errResult
	}
	toolDefs := a.buildSubtaskToolDefs(allowedTools)
	inputBlocks, errResult := a.buildSubtaskInput(task, params["input_images"])
	if errResult.IsError {
		return errResult
	}

	extraCtx, _ := params["context"].(string)
	env := getEnvInfoForWorkDir(a.cwd)
	toolsSummary := strings.Join(toolNames, ", ")
	if toolsSummary == "" {
		toolsSummary = "none"
	}
	subtaskPrompt, err := a.prompts.RenderSubtaskSystem(prompt.SubtaskPromptData{Task: task, Tools: toolsSummary, Context: extraCtx, OS: env["OS"], Arch: env["Arch"], WorkDir: env["WorkDir"], Workspace: a.cfg.Guard.Workspace})
	if err != nil || subtaskPrompt == "" {
		subtaskPrompt = fmt.Sprintf("You are a Suna subtask. Complete the assigned task and return a concise result.\n\nTask:\n%s\n\nAvailable tools: %s", task, toolsSummary)
	}

	spawnID := id
	if spawnID == "" {
		spawnID = uuid.New().String()
	}
	binding, err := a.router.Bind(modelRef)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("invalid spawn model %q: %v", modelRef, err))
	}
	ctx = model.WithBinding(ctx, binding)
	ctx = tools.MergeExecutionContext(ctx, tools.ExecutionContext{BoundaryID: "spawn:" + spawnID})
	r := a.newSubtaskRunner(events, spawnID, allowedTools)
	st := subtask.New(subtask.Request{ID: spawnID, Task: task, Input: inputBlocks, Binding: binding, Invocation: model.Invocation{SessionScope: model.SessionScope(a.sessionID)}, System: subtaskPrompt, ToolDefs: toolDefs})
	res, err := st.Run(ctx, r)
	if execCtx, ok := tools.ExecutionContextFrom(ctx); ok && a.tools != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if cleanupErr := a.tools.CleanupRun(cleanupCtx, execCtx); cleanupErr != nil {
			// subtask 结果保持原语义，清理 partial 通过安全结构化日志暴露。
			logging.Error("agent", "exec_subtask_cleanup_partial", nil, logging.Event{"session_id": execCtx.SessionID, "run_id": execCtx.RunID, "boundary_id": execCtx.BoundaryID})
		}
		cleanupCancel()
	}
	if err != nil && res.Status == "" {
		res = subtask.Result{
			Status: subtask.StatusFailed,
			Error:  err.Error(),
			SideEffects: subtask.SideEffects{
				Status:  subtask.SideEffectsUnknown,
				Summary: "Subtask failed before reporting side effects.",
			},
		}
	}
	out, _ := json.Marshal(spawnResultPayload(res))
	return spawnToolResult(string(out), res)
}

func spawnResultPayload(res subtask.Result) map[string]any {
	payload := map[string]any{
		"status":       res.Status,
		"result":       res.Text,
		"side_effects": res.SideEffects,
	}
	if strings.TrimSpace(res.Error) != "" {
		payload["error"] = res.Error
	}
	return payload
}

func spawnToolResult(content string, res subtask.Result) tools.Result {
	if res.Status != subtask.StatusFailed {
		return tools.TextResult(content)
	}
	errText := strings.TrimSpace(res.Error)
	if errText == "" {
		errText = strings.TrimSpace(res.Text)
	}
	if errText == "" {
		errText = "subtask failed"
	}
	return tools.Result{Content: content, Error: errText, IsError: true}
}

func (a *Agent) newSubtaskRunner(events chan<- Event, spawnID string, allowedTools map[string]bool) *runner.Runner {
	return &runner.Runner{Compressor: a.compressor, Calibrator: a.calibrator, Executor: subtaskExecutor{agent: a, events: events, allowedTools: allowedTools, spawnID: spawnID}, Sink: subtaskSink{events: events, spawnID: spawnID}, UsageSink: a, Hooks: runner.Hooks{CleanToolParams: a.cleanToolParams}}
}

type subtaskExecutor struct {
	agent        *Agent
	events       chan<- Event
	allowedTools map[string]bool
	spawnID      string
}

func (e subtaskExecutor) ExecuteTool(ctx context.Context, call runner.ToolExecution) tools.Result {
	name, params := call.Name, call.Params
	spec, ok := e.agent.tools.Get(name)
	if !ok {
		return tools.ErrorResult(fmt.Sprintf("tool %q not found", name))
	}
	if !tools.CanGrantToSubtask(spec) {
		return tools.ErrorResult(fmt.Sprintf("tool %q is not available to subtasks", name))
	}
	if !e.allowedTools[name] {
		return tools.ErrorResult(fmt.Sprintf("tool %q not allowed for subtask", name))
	}
	if e.agent.shouldGuardTool(name) {
		eventID := e.namespaced(call.ID)
		// subtask 也通过同一会话的 Guard gate，避免与主任务在同一 TUI 上交错确认。
		// 它不记录到主任务回执，保持 subtask 的意图上下文隔离。
		e.agent.guardGate.Lock()
		result := e.agent.guard.Check(ctx, name, params, e.agent.buildSubtaskGuardReviewContext(call))
		e.agent.emitToolGuard(e.events, eventID, name, result)
		allowed := true
		if result.Decision == guard.Reject {
			allowed = false
		} else if result.Decision == guard.Modify {
			e.agent.guardGate.Unlock()
			return guardModifyResult(result)
		} else if result.Decision == guard.Confirm {
			allowed = e.agent.confirmGuard(ctx, eventID, call, result, e.events, false)
		}
		e.agent.guardGate.Unlock()
		if !allowed {
			if result.Decision == guard.Reject {
				return tools.ErrorResult("blocked: " + result.Reason)
			}
			return tools.ErrorResult("blocked: user rejected guard confirmation")
		}
	}
	if err := sensitiveReadError(name, params); err != "" {
		return tools.ErrorResult(err)
	}
	res := e.agent.tools.Execute(ctx, tools.Call{ID: call.ID, Name: name, Params: params, Intent: call.Intent, AssistantContext: call.AssistantContext})
	if !res.IsError {
		res.Content = guard.MaskSensitiveContent(res.Content)
	}
	return res
}

func (e subtaskExecutor) namespaced(id string) string {
	if e.spawnID == "" {
		return id
	}
	return "spawn:" + e.spawnID + ":" + id
}

func (a *Agent) buildGuardReviewContext(call runner.ToolExecution) guard.ReviewContext {
	// smart guard 只传当前任务、用户最终决定和短工具意图，避免混杂历史工具输出。
	// main 与 subtask 的 review 上下文必须隔离，避免子任务串用 main 对话上下文。
	ctx := guard.ReviewContext{
		ToolIntent: guardExecutionRationale(call),
	}
	messages := call.WorkingMessages
	if len(messages) == 0 && a.working != nil {
		messages = a.working.Messages()
	}
	ctx.Task, ctx.LatestUserInput, ctx.UserDecisions, ctx.PreviousTask = a.guardTaskReviewContext()
	if ctx.Task == "" {
		ctx.Task = trimForGuardMiddle(lastUserTextFromMessages(messages), guardReviewTaskLimit)
	}
	if ctx.LatestUserInput == "" {
		ctx.LatestUserInput = trimForGuardMiddle(lastUserTextFromMessages(messages), guardReviewLatestInputLimit)
	}
	return ctx
}

func (a *Agent) buildSubtaskGuardReviewContext(call runner.ToolExecution) guard.ReviewContext {
	ctx := guard.ReviewContext{
		ToolIntent: guardExecutionRationale(call),
	}
	ctx.LatestUserInput = trimForGuardMiddle(lastUserTextFromMessages(call.WorkingMessages), guardReviewLatestInputLimit)
	ctx.Task = trimForGuardMiddle(lastUserTextFromMessages(call.WorkingMessages), guardReviewTaskLimit)
	return ctx
}

func guardExecutionRationale(call runner.ToolExecution) string {
	intent := strings.TrimSpace(call.Intent)
	context := strings.TrimSpace(call.AssistantContext)
	if intent == "" {
		return trimForGuard(context, 520)
	}
	if context == "" || context == intent {
		return trimForGuard(intent, 520)
	}
	return trimForGuard("Current step: "+intent+"\nAgent plan: "+context, 520)
}

func lastUserTextFromMessages(msgs []model.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == model.RoleUser {
			return msgs[i].Text()
		}
	}
	return ""
}

func trimForGuard(s string, max int) string {
	return trimForGuardMiddle(s, max)
}

func (a *Agent) shouldGuardTool(name string) bool {
	if a == nil || a.tools == nil {
		// 工具目录不可用时保持保守：未知工具默认需要 Guard。
		return true
	}
	spec, ok := a.tools.Get(name)
	if !ok {
		return true
	}
	return tools.ShouldGuard(spec)
}

func sensitiveReadError(name string, params map[string]any) string {
	if name != "readfile" || params == nil {
		return ""
	}
	path, _ := params["path"].(string)
	if sensitive, reason := guard.IsSensitivePath(path); sensitive {
		return fmt.Sprintf("blocked: sensitive file (%s). Reading credential/secret files is not allowed.", reason)
	}
	return ""
}

type subtaskSink struct {
	events  chan<- Event
	spawnID string
}

// subtaskSink 只把子任务工具事件转发到主会话；状态、文本流和 reasoning 已由 spawn 结果摘要承载。
func (s subtaskSink) Status(status runner.StatusEvent) {}
func (s subtaskSink) Stream(content string)            {}
func (s subtaskSink) Reasoning(content string)         {}

// 子任务 usage 只落库，不进入主 TUI token 展示。
func (s subtaskSink) Usage(usage runner.UsageEvent) {}
func (s subtaskSink) ToolCall(call runner.ToolCallEvent) {
	s.events <- Event{Type: EventToolCall, ToolCallID: s.namespaced(call.ID), ToolName: call.Name, ToolParams: call.Params, ToolIntent: call.Intent}
}
func (s subtaskSink) ToolResult(result runner.ToolResultEvent) {
	s.events <- Event{Type: EventToolResult, ToolCallID: s.namespaced(result.ID), ToolName: result.Name, ToolResult: result.Result, ToolError: result.Error, ToolMetadata: result.Metadata}
}
func (s subtaskSink) namespaced(id string) string {
	return "spawn:" + s.spawnID + ":" + id
}

func (a *Agent) buildSubtaskAllowedTools(value any) (map[string]bool, []string, tools.Result) {
	toolNames := parseStringList(value)
	allowed := make(map[string]bool, len(toolNames))
	ordered := make([]string, 0, len(toolNames))
	for _, name := range toolNames {
		name = strings.TrimSpace(name)
		if name == "" || allowed[name] {
			continue
		}
		spec, ok := a.tools.Get(name)
		if !ok || !tools.CanGrantToSubtask(spec) {
			return nil, nil, tools.ErrorResult(fmt.Sprintf("invalid spawn tool %q. Choose from: %s", name, strings.Join(a.availableSpawnTools(), ", ")))
		}
		allowed[name] = true
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	return allowed, ordered, tools.Result{}
}

func (a *Agent) buildSubtaskToolDefs(allowed map[string]bool) []model.ToolDef {
	if len(allowed) == 0 || a == nil || a.tools == nil {
		return nil
	}
	all := a.tools.ToolDefs(withIntentParameter)
	defs := make([]model.ToolDef, 0, len(allowed))
	for _, def := range all {
		if allowed[def.Name] {
			defs = append(defs, def)
		}
	}
	return defs
}

func (a *Agent) buildSubtaskInput(task string, value any) ([]model.ContentBlock, tools.Result) {
	blocks := []model.ContentBlock{{Type: model.ContentText, Text: task}}
	indexes, err := parseImageIndexes(value)
	if err != nil {
		return nil, tools.ErrorResult(err.Error())
	}
	if len(indexes) == 0 {
		return blocks, tools.Result{}
	}
	images := a.currentInputImages()
	for _, idx := range indexes {
		if idx < 0 || idx >= len(images) {
			return nil, tools.ErrorResult(fmt.Sprintf("invalid input image index %d; current user message has %d image(s)", idx, len(images)))
		}
		blocks = append(blocks, images[idx])
	}
	return blocks, tools.Result{}
}

func (a *Agent) currentInputImages() []model.ContentBlock {
	images := make([]model.ContentBlock, 0)
	for _, block := range a.currentInputBlocks {
		if block.Type == model.ContentImage && block.Media != nil {
			images = append(images, block)
		}
	}
	return images
}

func parseImageIndexes(value any) ([]int, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case []any:
		indexes := make([]int, 0, len(v))
		for _, item := range v {
			idx, ok := numericIndex(item)
			if !ok {
				return nil, fmt.Errorf("input_images must contain integer indexes")
			}
			indexes = append(indexes, idx)
		}
		return indexes, nil
	case []int:
		return append([]int(nil), v...), nil
	default:
		return nil, fmt.Errorf("input_images must be an array of image indexes")
	}
}

func numericIndex(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		idx := int(n)
		return idx, n == float64(idx)
	default:
		return 0, false
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
	if spec, ok := a.tools.Get(name); ok {
		return schemaPropertyKeys(spec.Parameters)
	}
	return nil
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
	refs := a.router.ListSpawnableModels(a.modelRef)
	sort.Strings(refs)
	return refs
}

func (a *Agent) availableSpawnTools() []string {
	if a.tools == nil {
		return nil
	}
	specs := a.tools.Specs()
	filtered := make([]string, 0, len(specs))
	for _, spec := range specs {
		if tools.CanGrantToSubtask(spec) {
			filtered = append(filtered, spec.Name)
		}
	}
	sort.Strings(filtered)
	return filtered
}

func (a *Agent) guardLLMReview(ctx context.Context, req guard.ReviewRequest) (string, error) {
	params := req.ParamsJSON
	paramsTruncated := req.ParamsTruncated
	reviewPrompt, err := a.prompts.RenderGuardReview(prompt.GuardReviewData{
		ToolName:         req.ToolName,
		ToolParams:       params,
		ParamsTruncated:  paramsTruncated,
		Target:           req.Target,
		Risk:             req.Risk,
		Task:             req.Context.Task,
		LatestUserInput:  req.Context.LatestUserInput,
		UserDecisions:    req.Context.UserDecisions,
		PreviousTask:     req.Context.PreviousTask,
		ToolIntent:       req.Context.ToolIntent,
		AssistantContext: req.Context.AssistantContext,
	})
	if err != nil {
		return "", err
	}
	binding := model.BindingFromContext(ctx)
	if binding == nil {
		return "", fmt.Errorf("guard review requires model binding")
	}
	request := &model.CompletionRequest{Model: binding.ModelID(), Invocation: model.Invocation{SessionScope: model.SessionScope(a.sessionID)}, Purpose: "guard_review", RequestID: uuid.New().String(), System: "Reply with JSON only.", Messages: []model.Message{model.NewTextMessage(model.RoleUser, reviewPrompt)}, Temperature: model.Float64Ptr(0)}
	ch, err := binding.Complete(ctx, *request)
	if err != nil {
		return "", err
	}
	return readGuardReviewStream(ctx, ch, model.LLMGuardReviewTimeout)
}

func truncateGuardReviewParams(params string) (string, bool) {
	return params, false
}

func readGuardReviewStream(ctx context.Context, ch <-chan model.Chunk, timeout time.Duration) (string, error) {
	// Guard review 只需要短 JSON；长时间无响应时回退到人工确认，不能阻塞工具执行链路。
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	resetTimer := func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(timeout)
	}

	var result strings.Builder
	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				return result.String(), nil
			}
			resetTimer()
			if chunk.Error != nil {
				return "", chunk.Error
			}
			if chunk.Content != "" {
				result.WriteString(chunk.Content)
			}
			if chunk.Done {
				return result.String(), nil
			}
		case <-timer.C:
			return "", fmt.Errorf("guard review LLM stream timeout")
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}
