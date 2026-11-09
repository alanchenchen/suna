package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/alanchenchen/suna/internal/logging"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/tools"
)

func (r *Runner) Run(ctx context.Context, req Request) (Result, error) {
	var result Result
	if req.Binding == nil {
		return result, fmt.Errorf("model binding is required")
	}
	modelRef := req.Binding.Ref()
	if modelRef == "" {
		return result, fmt.Errorf("model binding ref is required")
	}
	modelID := req.Binding.ModelID()
	if modelID == "" {
		modelID = modelRef
	}
	if req.Working == nil {
		return result, fmt.Errorf("working memory is required")
	}
	if req.Purpose == "" {
		req.Purpose = "chat"
	}
	req.MaxTokens = r.resolveMaxTokens(req.Binding, req.MaxTokens)
	if req.Memory == nil {
		req.Memory = NewContextMemory("")
		defer req.Memory.Cancel()
	}
	turns := 0
	toolCallsExecuted := 0

	for {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if req.MaxTurns > 0 && turns >= req.MaxTurns {
			if req.SealSteering != nil {
				req.SealSteering()
			}
			return result, fmt.Errorf("max turns exceeded (%d)", req.MaxTurns)
		}
		// 每次物理模型请求前吸收已排队消息，避免消息恰好在上一个安全边界后到达而多等一轮。
		if _, err := r.applySteering(ctx, req, false); err != nil {
			return result, err
		}

		messages := req.Working.Messages()
		var project func([]memory.Message) []model.Message
		if req.Project != nil {
			project = req.Project(ctx)
		}
		tools := buildToolDefs(req)
		contextWindow := req.Binding.ContextWindow()
		coef := r.calibrationCoefficient(modelRef)
		calibrated := r.calibrationReady(modelRef)
		completionReq := &model.CompletionRequest{Model: modelID, Purpose: req.Purpose, RequestID: uuid.NewString(), Invocation: req.Invocation, System: req.System, Tools: tools, MaxTokens: req.MaxTokens, Temperature: model.Float64Ptr(0.7)}
		if req.AutoCompress {
			q := CompactRequest{Binding: req.Binding, Invocation: req.Invocation, Working: req.Working, Request: *completionReq, Coefficient: coef, Calibrated: calibrated, Project: project, Commit: r.Hooks.CommitMemory, Usage: func(u *model.Usage) {
				if r.UsageSink != nil {
					r.UsageSink.RecordUsage(context.WithoutCancel(ctx), modelID, u)
				}
			}}
			for {
				if err := req.Memory.Maintain(ctx, r.Compressor, q, false); err != nil {
					return result, err
				}
				// hard 等待期间到达的纠正必须先落库再进入主请求；有新增则重算完整预算。
				// 沿用本次背景与工具定义；仅真实越过硬边界才再维护，不因重算多发普通候选。
				applied, err := r.applySteering(ctx, req, false)
				if err != nil {
					return result, err
				}
				if !applied || q.input(req.Working.Messages(), req.Memory.Summary()) < q.budget() {
					break
				}
			}
			messages = req.Working.Messages()
		}
		completionReq.SessionState = req.Memory.Summary()
		if project != nil {
			completionReq.Messages = project(messages)
		} else {
			completionReq.Messages = memory.ModelMessages(messages)
		}

		turns++
		if r.Sink != nil {
			r.Sink.Status(StatusEvent{Kind: StatusWaitingLLM})
		}
		requestStarted := time.Now()
		rawEstimatedTokens, estimatedContextTokens := logRequestPrepare(req, completionReq, contextWindow, turns, coef, calibrated)
		fullContent, toolCalls, usage, err := r.completeWithRecovery(ctx, req.Binding, completionReq, req)
		if err != nil {
			return result, err
		}
		if usage != nil {
			result.Usage = usage
			// 用真实 input token 回喂校准器；Observe 内部做异常过滤与 EMA 平滑，中转站偏差不会带偏系数。
			if r.Calibrator != nil && usage.InputTokens > 0 {
				r.Calibrator.Observe(modelRef, rawEstimatedTokens, usage.InputTokens)
			}
			if r.Sink != nil {
				contextTokens := usage.TotalTokens
				if contextTokens <= 0 {
					contextTokens = usage.InputTokens + usage.OutputTokens
				}
				r.Sink.Usage(UsageEvent{
					InputTokens:            usage.InputTokens,
					OutputTokens:           usage.OutputTokens,
					CacheReadTokens:        usage.CacheReadTokens,
					CacheCreationTokens:    usage.CacheCreationTokens,
					ContextTokens:          contextTokens,
					EstimatedContextTokens: estimatedContextTokens,
					ContextWindow:          contextWindow,
					Duration:               time.Since(requestStarted),
				})
			}
			if r.UsageSink != nil {
				r.UsageSink.RecordUsage(ctx, modelID, usage)
			}
		}

		consumed := req.Working.MarkConsumed()

		if len(toolCalls) == 0 {
			if fullContent != "" {
				req.Working.AddMessage(memory.NewTextMessage(model.RoleAssistant, fullContent))
				req.Memory.AddGrowth([]memory.Message{memory.NewTextMessage(model.RoleAssistant, fullContent)})
				if err := r.checkpoint(ctx); err != nil {
					return result, err
				}
				if r.Hooks.OnAssistantSaved != nil {
					r.Hooks.OnAssistantSaved(ctx)
				}
			} else if consumed {
				// 完整空响应不造空 assistant，但真实消费标记仍须在终态前保存。
				if err := r.checkpoint(ctx); err != nil {
					return result, err
				}
			}
			if req.MaxTurns > 0 && turns >= req.MaxTurns {
				if req.SealSteering != nil && req.SealSteering() {
					return result, fmt.Errorf("max turns exceeded (%d) before queued user messages could be applied", req.MaxTurns)
				}
				result.FinalText = fullContent
				result.ContextWindow = req.Binding.ContextWindow()
				return result, nil
			}
			if applied, err := r.applySteering(ctx, req, true); err != nil {
				return result, err
			} else if applied {
				continue
			}
			result.FinalText = fullContent
			result.ContextWindow = req.Binding.ContextWindow()
			return result, nil
		}

		// 检查点只能登记完整、可执行的声明；参数按统一工具接口要求为 JSON 对象，
		// 空对象合法，不能把截断 JSON 经 ParseToolCallArguments 包成 raw 后执行。
		for _, call := range toolCalls {
			var args map[string]any
			if call.ID == "" || call.Name == "" || json.Unmarshal([]byte(call.Arguments), &args) != nil || args == nil {
				return result, fmt.Errorf("model returned an incomplete tool declaration")
			}
		}
		preparedCalls, cleanToolCalls := r.prepareToolCalls(toolCalls, fullContent)
		assistantMsg := memory.Message{
			Role:        model.RoleAssistant,
			TextContent: fullContent,
			Content:     []model.ContentBlock{{Type: model.ContentText, Text: fullContent}},
			ToolCalls:   cleanToolCalls,
		}
		req.Working.AddMessage(assistantMsg)
		req.Memory.AddGrowth([]memory.Message{assistantMsg})
		if err := r.checkpoint(ctx); err != nil {
			return result, err
		}
		if r.Hooks.OnAssistantSaved != nil {
			r.Hooks.OnAssistantSaved(ctx)
		}

		for _, pc := range preparedCalls {
			result.HadToolCall = true
			if r.Sink != nil {
				r.Sink.ToolCall(ToolCallEvent{ID: pc.tc.ID, Name: pc.tc.Name, Params: pc.params, Intent: pc.intent})
			}
		}
		if req.MaxToolCalls > 0 && toolCallsExecuted+len(preparedCalls) > req.MaxToolCalls {
			return result, fmt.Errorf("max tool calls exceeded (%d)", req.MaxToolCalls)
		}
		toolCallsExecuted += len(preparedCalls)

		workingSnapshot := req.Working.Messages()
		var saveErr error
		var pendingResults []ToolResultEvent
		base := workingSnapshot
		received := make([]*memory.Message, len(preparedCalls))
		r.executeToolCalls(ctx, preparedCalls, memory.ModelMessages(workingSnapshot), func(execResult toolExecResult) {
			toolText := memory.TruncateToolOutputForContext(execResult.result.Content)
			message := memory.NewTextMessage(model.RoleTool, toolText)
			message.ToolCallID = execResult.tc.ID
			message.ToolStatus = "completed"
			if execResult.result.IsError {
				message.ToolStatus = "failed"
				result.HadToolError = true
			}
			message.Truncated = execResult.result.Truncated || toolText != execResult.result.Content
			for _, image := range execResult.result.Images {
				if image.Media != nil {
					message.Content = append(message.Content, model.ContentBlock{Type: model.ContentText, Text: memory.ImageReference(*image.Media)})
				}
			}
			if len(execResult.result.Images) > 0 {
				message.TextContent = ""
			}
			received[execResult.index] = &message
			ordered := make([]memory.Message, 0, len(base)+len(received))
			ordered = append(ordered, base...)
			for _, m := range received {
				if m != nil {
					ordered = append(ordered, *m)
				}
			}
			req.Working.SetMessages(ordered)
			req.Memory.AddGrowth([]memory.Message{message})
			r.toolImages = append(r.toolImages, execResult.result.Images...)
			pendingResults = append(pendingResults, ToolResultEvent{ID: execResult.tc.ID, Name: execResult.tc.Name, Result: execResult.result.Content, Error: execResult.result.IsError, Metadata: execResult.result.Metadata})
			if err := r.checkpoint(ctx); err != nil {
				if saveErr == nil {
					saveErr = err
				}
				return
			}
			if r.Sink != nil {
				for _, event := range pendingResults {
					r.Sink.ToolResult(event)
				}
			}
			pendingResults = nil
		})
		if saveErr != nil {
			return result, saveErr
		}

		// 工具产出的图片（如 read_image）统一注入为一条 user 消息，图片块只参与当前 run；
		// 注入前按 MediaRef 去重，避免同一图片在上下文里重复累积。
		if r.injectToolImages(req.Working) {
			if err := r.checkpoint(ctx); err != nil {
				return result, err
			}
		}
		if req.MaxTurns <= 0 || turns < req.MaxTurns {
			if _, err := r.applySteering(ctx, req, false); err != nil {
				return result, err
			}
		}

	}
}

func (r *Runner) checkpoint(ctx context.Context) error {
	if r.Hooks.Checkpoint != nil {
		return r.Hooks.Checkpoint(ctx)
	}
	return nil
}

func (r *Runner) applySteering(ctx context.Context, req Request, seal bool) (bool, error) {
	if req.TakeSteering == nil {
		return false, nil
	}
	inputs := req.TakeSteering(seal)
	applied := false
	for _, input := range inputs {
		if input.Message.Role != model.RoleUser || len(input.Message.Content) == 0 {
			continue
		}
		input.Message.Origin = memory.OriginUser
		before := req.Working.Messages()
		req.Working.AddMessage(input.Message)
		if err := r.checkpoint(ctx); err != nil {
			// 输入尚未用于模型或工具，保存失败仅回退本条；mailbox 保留认领，
			// 由本轮失败收尾明确 reject，不能重新允许撤回或发 applied。
			req.Working.SetMessages(before)
			return false, err
		}
		req.Memory.AddGrowth([]memory.Message{input.Message})
		applied = true
		if r.Hooks.OnSteeringApplied != nil {
			r.Hooks.OnSteeringApplied(ctx, input)
		}
	}
	return applied, nil
}

func (r *Runner) completeWithRecovery(ctx context.Context, binding *model.ModelBinding, completionReq *model.CompletionRequest, req Request) (string, []model.ToolCall, *model.Usage, error) {
	modelRef := binding.Ref()
	streamTimeout := req.StreamTimeout
	dynamicReasoningTimeout := false
	if streamTimeout <= 0 {
		streamTimeout = defaultChatIdleTimeout
		dynamicReasoningTimeout = true
	}
	policy := r.RetryPolicy.normalized()
	var recoveryStarted time.Time
	var deadline time.Time
	var lastErr error
	for attempt := 1; ; attempt++ {
		if attempt > 1 && !recoveryStarted.IsZero() && !time.Now().Before(deadline) {
			logModelRecovery("exhausted", completionReq, modelRef, attempt-1, 0, lastErr)
			return "", nil, nil, lastErr
		}
		requestCtx := ctx
		cancelRequest := func() {}
		if !recoveryStarted.IsZero() {
			requestCtx, cancelRequest = context.WithDeadline(ctx, deadline)
		}
		ch, err := binding.Complete(requestCtx, *completionReq)
		if err != nil {
			lastErr = err
		} else {
			content, toolCalls, usage, streamErr, visibleOutput := r.readStream(requestCtx, ch, streamTimeout, dynamicReasoningTimeout, req)
			if streamErr == nil {
				cancelRequest()
				if attempt > 1 {
					logModelRecovery("recovered", completionReq, modelRef, attempt, 0, nil)
				}
				return content, toolCalls, usage, nil
			}
			if visibleOutput {
				cancelRequest()
				return content, toolCalls, usage, streamErr
			}
			lastErr = streamErr
		}
		cancelRequest()
		if !retryableModelRequestError(lastErr) {
			return "", nil, nil, lastErr
		}
		if recoveryStarted.IsZero() {
			recoveryStarted = time.Now()
			deadline = policy.deadline(recoveryStarted)
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			logModelRecovery("exhausted", completionReq, modelRef, attempt, 0, lastErr)
			return "", nil, nil, lastErr
		}
		delay := policy.delay(attempt)
		if delay > remaining {
			delay = remaining
		}
		logModelRecovery("retrying", completionReq, modelRef, attempt, delay, lastErr)
		if r.Sink != nil {
			r.Sink.Status(StatusEvent{Kind: StatusLLMRetrying, Attempt: attempt, Delay: delay, Elapsed: time.Since(recoveryStarted), Budget: policy.Budget, Error: model.NewModelError(lastErr)})
		}
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return "", nil, nil, ctx.Err()
		}
		if r.Sink != nil {
			r.Sink.Status(StatusEvent{Kind: StatusWaitingLLM})
		}
	}
}

func logModelRecovery(status string, req *model.CompletionRequest, modelRef string, attempt int, delay time.Duration, err error) {
	fields := logging.Event{
		"request_id": requestIDForRecovery(req),
		"status":     status,
		"attempt":    attempt,
		"model_ref":  modelRef,
	}
	if req != nil {
		fields["model"] = req.Model
		fields["purpose"] = req.Purpose
	}
	if delay > 0 {
		fields["retry_delay_ms"] = delay.Milliseconds()
	}
	me := model.NewModelError(err)
	if me != nil {
		fields["error_kind"] = me.Kind
		if me.StatusCode > 0 {
			fields["status_code"] = me.StatusCode
		}
		if me.Code != "" {
			fields["provider_code"] = me.Code
		}
		if me.Type != "" {
			fields["provider_type"] = me.Type
		}
	}
	if status == "exhausted" {
		logging.Error("llm", "recovery", err, fields)
		return
	}
	logging.Info("llm", "recovery", fields)
}

func requestIDForRecovery(req *model.CompletionRequest) string {
	if req != nil && req.RequestID != "" {
		return req.RequestID
	}
	return ""
}

func (r *Runner) readStream(ctx context.Context, ch <-chan model.Chunk, streamTimeout time.Duration, dynamicReasoningTimeout bool, req Request) (string, []model.ToolCall, *model.Usage, error, bool) {
	var contentBuilder strings.Builder
	var toolCalls []model.ToolCall
	var lastUsage *model.Usage
	var visibleOutput bool
	var finish *model.FinishInfo
	timer := time.NewTimer(streamTimeout)
	defer timer.Stop()

	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				return contentBuilder.String(), toolCalls, lastUsage, fmt.Errorf("model stream closed before completion"), visibleOutput
			}
			if dynamicReasoningTimeout && chunk.ReasoningContent != "" {
				streamTimeout = defaultReasoningIdleTimeout
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(streamTimeout)
			if ctx.Err() != nil {
				return contentBuilder.String(), toolCalls, lastUsage, ctx.Err(), visibleOutput
			}
			if chunk.Error != nil {
				return contentBuilder.String(), toolCalls, lastUsage, chunk.Error, visibleOutput
			}
			if chunk.ReasoningContent != "" && req.EmitReasoning && r.Sink != nil {
				visibleOutput = true
				r.Sink.Reasoning(chunk.ReasoningContent)
			}
			if chunk.Content != "" {
				visibleOutput = true
				contentBuilder.WriteString(chunk.Content)
				if req.EmitStream && r.Sink != nil {
					r.Sink.Stream(chunk.Content)
				}
			}
			if len(chunk.ToolCalls) > 0 {
				visibleOutput = true
				toolCalls = append(toolCalls, chunk.ToolCalls...)
			}
			if chunk.Usage != nil {
				lastUsage = chunk.Usage
			}
			if chunk.Finish != nil {
				finish = chunk.Finish
			}
			if chunk.Done {
				// Done 只表示本地流结束；完整检查点还需供应商明确的完成标记。
				if finish == nil || (finish.Status == "" && finish.Reason == "") {
					return contentBuilder.String(), toolCalls, lastUsage, fmt.Errorf("model response has no completion marker"), visibleOutput
				}
				if finish.IncompleteReason != "" || (finish.Status != "" && finish.Status != "completed") {
					return contentBuilder.String(), toolCalls, lastUsage, fmt.Errorf("model response was incomplete (%s %s)", finish.Status, finish.Reason), visibleOutput
				}
				switch finish.Reason {
				case "", "stop", "end_turn", "stop_sequence", "tool_calls", "tool_use", "refusal":
					// Anthropic 的 refusal 是完整拒绝答复，不是截断或请求失败。
				default:
					return contentBuilder.String(), toolCalls, lastUsage, fmt.Errorf("model response was incomplete (%s %s)", finish.Status, finish.Reason), visibleOutput
				}
				return contentBuilder.String(), toolCalls, lastUsage, nil, visibleOutput
			}
		case <-timer.C:
			return contentBuilder.String(), toolCalls, lastUsage, fmt.Errorf("LLM stream idle timeout (%s). The model may still be thinking; continue or retry if needed", streamTimeout), visibleOutput
		case <-ctx.Done():
			return contentBuilder.String(), toolCalls, lastUsage, ctx.Err(), visibleOutput
		}
	}
}

func (r *Runner) prepareToolCalls(toolCalls []model.ToolCall, fullContent string) ([]preparedToolCall, []memory.ToolCall) {
	toolIntent := extractToolIntent(fullContent)
	preparedCalls := make([]preparedToolCall, 0, len(toolCalls))
	cleanToolCalls := make([]memory.ToolCall, 0, len(toolCalls))
	for _, tc := range toolCalls {
		params := model.ParseToolCallArguments(tc.Arguments)
		intent := ""
		if r.Hooks.CleanToolParams != nil {
			params, intent = r.Hooks.CleanToolParams(tc.Name, params)
		}
		if intent == "" {
			intent = toolIntent
		}
		cleanTC := memory.ToolCall{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments, Intent: intent}
		if b, err := json.Marshal(params); err == nil {
			cleanTC.Arguments = string(b)
		}
		preparedCalls = append(preparedCalls, preparedToolCall{tc: model.ToolCall{ID: cleanTC.ID, Name: cleanTC.Name, Arguments: cleanTC.Arguments}, params: params, intent: intent, assistantContext: strings.TrimSpace(fullContent)})
		cleanToolCalls = append(cleanToolCalls, cleanTC)
	}
	return preparedCalls, cleanToolCalls
}

func (r *Runner) executeToolCalls(ctx context.Context, calls []preparedToolCall, workingSnapshot []model.Message, onResult func(toolExecResult)) []toolExecResult {
	resultCh := make(chan toolExecResult, len(calls))
	for i, pc := range calls {
		go func(index int, pc preparedToolCall) {
			res := tools.ErrorResult("tool executor not configured")
			if r.Executor != nil {
				res = r.Executor.ExecuteTool(ctx, ToolExecution{ID: pc.tc.ID, Name: pc.tc.Name, Params: pc.params, Intent: pc.intent, AssistantContext: pc.assistantContext, WorkingMessages: cloneMessages(workingSnapshot)})
			}
			resultCh <- toolExecResult{index: index, tc: pc.tc, result: res}
		}(i, pc)
	}
	results := make([]toolExecResult, len(calls))
	for i := 0; i < len(calls); i++ {
		execResult := <-resultCh
		results[execResult.index] = execResult
		if onResult != nil {
			onResult(execResult)
		}
	}
	return results
}

func cloneMessages(msgs []model.Message) []model.Message {
	if len(msgs) == 0 {
		return nil
	}
	cp := make([]model.Message, len(msgs))
	copy(cp, msgs)
	return cp
}

// injectToolImages 把本轮工具产出的图片块合并注入为一条 user 消息。
// 注入层只做同批次去重（同一轮多次 read_image 同一张图只注入一次），
// 跨轮重读放行：模型可以重看图片（含图片内容更新后的新版），
// 当前run结束时只在副本投影媒体引用，不按source文本删改其他消息。
func (r *Runner) injectToolImages(working *memory.WorkingMemory) bool {
	if working == nil {
		return false
	}
	images := collectResultImages(r.toolImages)
	r.toolImages = r.toolImages[:0]
	if len(images) == 0 {
		return false
	}
	unique := images[:0]
	for _, img := range images {
		// 同批次内重复也要去重：unique 是尚未注入 working 的候选，检查范围需包含它。
		if !imageBlockInBlocks(unique, img) {
			unique = append(unique, img)
		}
	}
	if len(unique) == 0 {
		return false
	}
	working.AddMessage(memory.Message{
		Role:    model.RoleUser,
		Origin:  memory.OriginTool,
		Content: unique,
	})
	return true
}

// collectResultImages 收集本轮工具结果中的图片块；同一结果里的图片保持顺序。
func collectResultImages(images []model.ContentBlock) []model.ContentBlock {
	out := make([]model.ContentBlock, 0, len(images))
	for _, img := range images {
		if img.Type == model.ContentImage && img.Media != nil {
			out = append(out, img)
		}
	}
	return out
}

// imageBlockInBlocks 判断图片块是否已存在于一组内容块中（按 MediaRef 比较）。
func imageBlockInBlocks(blocks []model.ContentBlock, img model.ContentBlock) bool {
	if img.Media == nil {
		return true
	}
	for _, b := range blocks {
		if b.Type == model.ContentImage && b.Media != nil && sameMediaRef(*b.Media, *img.Media) {
			return true
		}
	}
	return false
}

func sameMediaRef(a, b model.MediaRef) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case model.MediaURL:
		return a.URL == b.URL
	case model.MediaPath, model.MediaAttachment:
		return a.Path == b.Path
	default:
		return false
	}
}

// calibrationCoefficient 读取指定模型的 token 估算校准系数；calibrator 未注入时返回 1.0（等价未校准）。
func (r *Runner) calibrationCoefficient(modelRef string) float64 {
	if r.Calibrator == nil {
		return 1.0
	}
	return r.Calibrator.Coefficient(modelRef)
}

// calibrationReady 表示指定模型是否已有有效校准数据，用于决定是否收小安全垫。
func (r *Runner) calibrationReady(modelRef string) bool {
	if r.Calibrator == nil {
		return false
	}
	return r.Calibrator.Calibrated(modelRef)
}

func (r *Runner) resolveMaxTokens(binding *model.ModelBinding, requested int) int {
	if binding == nil {
		return requested
	}
	maxOutput := binding.MaxOutputTokens()
	if requested > 0 && requested < maxOutput {
		return requested
	}
	return maxOutput
}

func buildToolDefs(req Request) []model.ToolDef {
	if req.ToolDefs != nil {
		return req.ToolDefs()
	}
	return nil
}

func extractToolIntent(fullContent string) string {
	text := strings.TrimSpace(fullContent)
	if text == "" {
		return ""
	}
	sentences := strings.FieldsFunc(text, func(r rune) bool {
		return r == '.' || r == '。' || r == '\n'
	})
	for i := len(sentences) - 1; i >= 0; i-- {
		s := strings.TrimSpace(sentences[i])
		if s != "" {
			return s
		}
	}
	return ""
}
