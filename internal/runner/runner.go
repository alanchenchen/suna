package runner

import (
	"context"
	"encoding/json"
	"errors"
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
	if r.Router == nil {
		return result, fmt.Errorf("no model configured, please add a model in config")
	}
	if req.Working == nil {
		return result, fmt.Errorf("working memory is required")
	}
	if req.ModelRef == "" {
		return result, fmt.Errorf("model ref is required")
	}
	if req.ModelID == "" {
		req.ModelID = req.ModelRef
	}
	if req.Purpose == "" {
		req.Purpose = "chat"
	}
	req.MaxTokens = r.resolveMaxTokens(req.ModelRef, req.MaxTokens)
	turns := 0
	toolCallsExecuted := 0

	for {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if req.MaxTurns > 0 && turns >= req.MaxTurns {
			return result, fmt.Errorf("max turns exceeded (%d)", req.MaxTurns)
		}
		turns++

		messages := req.Working.Messages()
		if req.Messages != nil {
			messages = req.Messages(ctx)
		}
		tools := buildToolDefs(req)
		contextWindow := r.contextWindow(req.ModelRef)
		completionReq := &model.CompletionRequest{
			Model:        req.ModelID,
			Purpose:      req.Purpose,
			RequestID:    uuid.New().String(),
			System:       req.System,
			SessionState: req.SessionState,
			Messages:     messages,
			Tools:        tools,
			MaxTokens:    req.MaxTokens,
		}
		if req.AutoCompress {
			completionReq.Messages = trimToolResultsForContext(completionReq.Messages)
			needCompact := shouldCompactRequest(completionReq, contextWindow)
			if needCompact && r.Sink != nil {
				r.Sink.Status(StatusEvent{Kind: StatusCompactRunning})
			}
			var compactErr error
			if needCompact {
				req.SessionState, compactErr = r.compactForRequest(ctx, req.Working, completionReq, contextWindow, req.SessionState)
				completionReq.SessionState = req.SessionState
				if compactErr == nil && r.Hooks.OnCompactCommit != nil {
					compactErr = r.Hooks.OnCompactCommit(ctx, req.SessionState)
				}
				if compactErr != nil {
					if r.Sink != nil {
						r.Sink.Status(StatusEvent{Kind: StatusCompactError, Message: "automatic context compression failed: " + compactErr.Error()})
					}
					return result, fmt.Errorf("automatic context compression failed: %w", compactErr)
				}
				if r.Sink != nil {
					r.Sink.Status(StatusEvent{Kind: StatusCompactDone})
				}
			}
			messages = req.Working.Messages()
			if req.Messages != nil {
				messages = req.Messages(ctx)
			}
			completionReq.Messages = trimToolResultsForContext(messages)
			if shouldCompactRequest(completionReq, contextWindow) {
				estimated := estimateRequestTokens(completionReq)
				inputLimit := usableInputBudget(contextWindow, completionReq.MaxTokens)
				logging.Error("memory", "session_compact_still_oversized", nil, logging.Event{"mode": "auto", "purpose": req.Purpose, "model": req.ModelID, "context_window": contextWindow, "request_tokens": estimated, "input_limit": inputLimit, "compacted": needCompact})
				if needCompact && r.Sink != nil {
					r.Sink.Status(StatusEvent{Kind: StatusCompactError, Message: "automatic context compression could not reduce the request enough; try /compact manually, reduce the current input, or start a new session"})
				}
				return result, fmt.Errorf("context remains too large after compaction (%d tokens estimated, %d token input limit); start a new session or reduce the current input", estimated, inputLimit)
			}
		}

		if req.AutoCompress {
			result.SessionState = req.SessionState
		}

		if r.Sink != nil {
			r.Sink.Status(StatusEvent{Kind: StatusWaitingLLM})
		}
		requestStarted := time.Now()
		estimatedContextTokens := logRequestPrepare(req, completionReq, contextWindow, turns)
		fullContent, toolCalls, usage, err := r.completeWithRecovery(ctx, req.ModelRef, completionReq, req)
		if err != nil {
			return result, err
		}
		if usage != nil {
			result.Usage = usage
			if r.Sink != nil {
				contextTokens := usage.TotalTokens
				if contextTokens <= 0 {
					contextTokens = usage.InputTokens + usage.OutputTokens
				}
				r.Sink.Usage(UsageEvent{
					InputTokens:            usage.InputTokens,
					OutputTokens:           usage.OutputTokens,
					CachedTokens:           usage.CachedTokens,
					ContextTokens:          contextTokens,
					EstimatedContextTokens: estimatedContextTokens,
					ContextWindow:          contextWindow,
					Duration:               time.Since(requestStarted),
				})
			}
			if r.UsageSink != nil {
				r.UsageSink.RecordUsage(ctx, req.ModelID, usage)
			}
		}

		if fullContent != "" || len(toolCalls) > 0 {
			if fullContent != "" && r.Hooks.OnAssistantText != nil {
				r.Hooks.OnAssistantText(ctx, fullContent)
			}
			if len(toolCalls) == 0 {
				req.Working.AddMessage(model.NewTextMessage(model.RoleAssistant, fullContent))
			}
		}

		if len(toolCalls) == 0 {
			result.FinalText = fullContent
			result.ContextWindow = r.contextWindow(req.ModelRef)
			return result, nil
		}

		preparedCalls, cleanToolCalls := r.prepareToolCalls(toolCalls, fullContent)
		assistantMsg := model.Message{
			Role:        model.RoleAssistant,
			TextContent: fullContent,
			Content:     []model.ContentBlock{{Type: model.ContentText, Text: fullContent}},
			ToolCalls:   cleanToolCalls,
		}
		req.Working.AddMessage(assistantMsg)

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
		results := r.executeToolCalls(ctx, preparedCalls, workingSnapshot, func(execResult toolExecResult) {
			if r.Sink != nil {
				r.Sink.ToolResult(ToolResultEvent{ID: execResult.tc.ID, Name: execResult.tc.Name, Result: execResult.result.Content, Error: execResult.result.IsError, Metadata: execResult.result.Metadata})
			}
			if execResult.result.IsError {
				result.HadToolError = true
			}
		})

		for _, execResult := range results {
			// TUI 事件仍拿原始工具结果展示；WorkingMemory 只保存面向模型的截断版本，避免单个工具输出直接撑爆上下文。
			toolText := memory.TruncateToolOutputForContext(execResult.result.Content)
			req.Working.AddMessage(model.Message{
				Role:        model.RoleTool,
				ToolCallID:  execResult.tc.ID,
				TextContent: toolText,
				Content:     []model.ContentBlock{{Type: model.ContentText, Text: toolText}},
			})
			if r.Hooks.OnToolResult != nil {
				r.Hooks.OnToolResult(execResult.tc.Name, execResult.result)
			}
		}

	}
}

func (r *Runner) completeWithRecovery(ctx context.Context, modelRef string, completionReq *model.CompletionRequest, req Request) (string, []model.ToolCall, *model.Usage, error) {
	const maxAttempts = 3
	const retryDelay = 8 * time.Second
	streamTimeout := req.StreamTimeout
	dynamicReasoningTimeout := false
	if streamTimeout <= 0 {
		streamTimeout = defaultChatIdleTimeout
		dynamicReasoningTimeout = true
	}
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		ch, err := r.Router.Complete(ctx, modelRef, completionReq)
		if err != nil {
			lastErr = err
		} else {
			content, toolCalls, usage, streamErr, visibleOutput := r.readStream(ctx, ch, streamTimeout, dynamicReasoningTimeout, req)
			if streamErr == nil {
				return content, toolCalls, usage, nil
			}
			if visibleOutput {
				return content, toolCalls, usage, streamErr
			}
			lastErr = streamErr
		}
		if attempt >= maxAttempts || !retryableModelRequestError(lastErr) {
			if attempt > 1 || retryableModelRequestError(lastErr) {
				logModelRecovery("exhausted", completionReq, modelRef, attempt, maxAttempts, 0, lastErr)
			}
			return "", nil, nil, lastErr
		}
		logModelRecovery("retrying", completionReq, modelRef, attempt, maxAttempts, retryDelay, lastErr)
		if r.Sink != nil {
			r.Sink.Status(StatusEvent{Kind: StatusLLMRetrying, Attempt: attempt + 1, MaxAttempts: maxAttempts, Delay: retryDelay, Error: model.NewModelError(lastErr)})
		}
		select {
		case <-time.After(retryDelay):
		case <-ctx.Done():
			return "", nil, nil, ctx.Err()
		}
		if r.Sink != nil {
			r.Sink.Status(StatusEvent{Kind: StatusWaitingLLM})
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("model request failed without an error")
	}
	return "", nil, nil, lastErr
}

func logModelRecovery(status string, req *model.CompletionRequest, modelRef string, attempt, maxAttempts int, delay time.Duration, err error) {
	fields := logging.Event{
		"request_id":   requestIDForRecovery(req),
		"status":       status,
		"attempt":      attempt,
		"max_attempts": maxAttempts,
		"model_ref":    modelRef,
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

func retryableModelRequestError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	me := model.NewModelError(err)
	if me == nil {
		return false
	}
	if me.Kind == model.ModelErrorNetwork {
		return true
	}
	if me.Kind != model.ModelErrorHTTP {
		return false
	}
	switch me.StatusCode {
	case 408, 429, 500, 502, 503, 504:
		return true
	default:
		return false
	}
}

func (r *Runner) readStream(ctx context.Context, ch <-chan model.Chunk, streamTimeout time.Duration, dynamicReasoningTimeout bool, req Request) (string, []model.ToolCall, *model.Usage, error, bool) {
	var contentBuilder strings.Builder
	var toolCalls []model.ToolCall
	var lastUsage *model.Usage
	var visibleOutput bool
	timer := time.NewTimer(streamTimeout)
	defer timer.Stop()

	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				return contentBuilder.String(), toolCalls, lastUsage, nil, visibleOutput
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
			if chunk.Done {
				return contentBuilder.String(), toolCalls, lastUsage, nil, visibleOutput
			}
		case <-timer.C:
			return contentBuilder.String(), toolCalls, lastUsage, fmt.Errorf("LLM stream idle timeout (%s). The model may still be thinking; continue or retry if needed", streamTimeout), visibleOutput
		case <-ctx.Done():
			return contentBuilder.String(), toolCalls, lastUsage, ctx.Err(), visibleOutput
		}
	}
}

func (r *Runner) prepareToolCalls(toolCalls []model.ToolCall, fullContent string) ([]preparedToolCall, []model.ToolCall) {
	toolIntent := extractToolIntent(fullContent)
	preparedCalls := make([]preparedToolCall, 0, len(toolCalls))
	cleanToolCalls := make([]model.ToolCall, 0, len(toolCalls))
	for _, tc := range toolCalls {
		params := model.ParseToolCallArguments(tc.Arguments)
		intent := ""
		if r.Hooks.CleanToolParams != nil {
			params, intent = r.Hooks.CleanToolParams(tc.Name, params)
		}
		if intent == "" {
			intent = toolIntent
		}
		cleanTC := tc
		if b, err := json.Marshal(params); err == nil {
			cleanTC.Arguments = string(b)
		}
		preparedCalls = append(preparedCalls, preparedToolCall{tc: cleanTC, params: params, intent: intent, assistantContext: strings.TrimSpace(fullContent)})
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

func (r *Runner) resolveMaxTokens(modelRef string, requested int) int {
	if r.Router == nil {
		return requested
	}
	maxOutput := r.Router.MaxOutputTokens(modelRef)
	if requested > 0 && requested < maxOutput {
		return requested
	}
	return maxOutput
}

func (r *Runner) contextWindow(modelRef string) int {
	if r.Router == nil {
		return 0
	}
	p, err := r.Router.Provider(modelRef)
	if err != nil || p == nil {
		return 0
	}
	return p.ContextWindow()
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
