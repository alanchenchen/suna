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
		ch, err := r.Router.Complete(ctx, req.ModelRef, completionReq)
		if err != nil {
			return result, err
		}
		streamTimeout := req.StreamTimeout
		dynamicReasoningTimeout := false
		if streamTimeout <= 0 {
			streamTimeout = defaultChatIdleTimeout
			dynamicReasoningTimeout = true
		}

		fullContent, toolCalls, usage, err := r.readStream(ctx, ch, streamTimeout, dynamicReasoningTimeout, req)
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

func (r *Runner) readStream(ctx context.Context, ch <-chan model.Chunk, streamTimeout time.Duration, dynamicReasoningTimeout bool, req Request) (string, []model.ToolCall, *model.Usage, error) {
	var fullContent string
	var toolCalls []model.ToolCall
	var lastUsage *model.Usage
	timer := time.NewTimer(streamTimeout)
	defer timer.Stop()

	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				return fullContent, toolCalls, lastUsage, nil
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
				return fullContent, toolCalls, lastUsage, ctx.Err()
			}
			if chunk.Error != "" {
				return fullContent, toolCalls, lastUsage, fmt.Errorf("%s", readableLLMStreamError(chunk.Error))
			}
			if chunk.ReasoningContent != "" && req.EmitReasoning && r.Sink != nil {
				r.Sink.Reasoning(chunk.ReasoningContent)
			}
			if chunk.Content != "" {
				fullContent += chunk.Content
				if req.EmitStream && r.Sink != nil {
					r.Sink.Stream(chunk.Content)
				}
			}
			if len(chunk.ToolCalls) > 0 {
				toolCalls = append(toolCalls, chunk.ToolCalls...)
			}
			if chunk.Usage != nil {
				lastUsage = chunk.Usage
			}
			if chunk.Done {
				return fullContent, toolCalls, lastUsage, nil
			}
		case <-timer.C:
			return fullContent, toolCalls, lastUsage, fmt.Errorf("LLM stream idle timeout (%s). The model may still be thinking; continue or retry if needed", streamTimeout)
		case <-ctx.Done():
			return fullContent, toolCalls, lastUsage, ctx.Err()
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

func readableLLMStreamError(errText string) string {
	text := strings.TrimSpace(errText)
	lower := strings.ToLower(text)
	if strings.Contains(lower, "unexpected end of json input") {
		return "LLM stream interrupted: the upstream model service returned incomplete JSON. Please retry, or switch model/provider and try again."
	}
	return text
}
