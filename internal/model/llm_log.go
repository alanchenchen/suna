package model

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/logging"
)

type llmRoute struct {
	Adapter  string
	Protocol string
	ModelRef string
	Model    string
}

type llmRequestStreamStats struct {
	chunkCount     int
	assistantBytes int
	reasoningBytes int
	toolCalls      int
	usageReceived  bool
	finish         *FinishInfo
	lastChunkAt    time.Time
}

const llmDebugTextLimit = 4000

type llmDebugMessage struct {
	Index      int                `json:"index"`
	Role       string             `json:"role"`
	Text       string             `json:"text,omitempty"`
	Content    []llmDebugContent  `json:"content,omitempty"`
	ToolCalls  []llmDebugToolCall `json:"tool_calls,omitempty"`
	ToolCallID string             `json:"tool_call_id,omitempty"`
	TextBytes  int                `json:"text_bytes,omitempty"`
	Truncated  bool               `json:"truncated,omitempty"`
}

type llmDebugContent struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	TextBytes int    `json:"text_bytes,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	MediaKind string `json:"media_kind,omitempty"`
	MediaName string `json:"media_name,omitempty"`
	MediaMime string `json:"media_mime,omitempty"`
	MediaSize int64  `json:"media_size,omitempty"`
}

type llmDebugToolCall struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Arguments     string `json:"arguments,omitempty"`
	ArgumentBytes int    `json:"argument_bytes,omitempty"`
	ArgsTruncated bool   `json:"args_truncated,omitempty"`
}

func ensureRequestID(req *CompletionRequest) string {
	if req == nil {
		return uuid.New().String()
	}
	if req.RequestID == "" {
		req.RequestID = uuid.New().String()
	}
	return req.RequestID
}

func newLLMRoute(ref string, mc config.ModelConfig, req *CompletionRequest) llmRoute {
	return llmRoute{
		Adapter:  mc.Provider,
		Protocol: string(mc.ProtocolOrDefault()),
		ModelRef: ref,
		Model:    resolvedRequestModel(mc, req),
	}
}

func resolvedRequestModel(mc config.ModelConfig, req *CompletionRequest) string {
	if req != nil && req.Model != "" {
		return req.Model
	}
	return mc.Model
}

func logLLMRequestStartFailure(req *CompletionRequest, route llmRoute, started time.Time, err error) {
	fields := llmRequestFields(started, nil)
	fields["usage_received"] = false
	logLLMRequestFailure(req, route, err, fields)
}

// logAndForwardLLMRequestStream 消费 provider 原始 stream，统计日志后原样转发给调用方。
// Go channel 不能被旁路监听；如果日志 goroutine 直接读取 raw，会和正常流程抢 chunk。
func logAndForwardLLMRequestStream(raw <-chan Chunk, req *CompletionRequest, route llmRoute, started time.Time) <-chan Chunk {
	out := make(chan Chunk, adapterChunkBuffer)
	go func() {
		defer close(out)
		usage, stats, failed, modelErr := collectLLMRequestStream(raw, out, started)
		fields := llmRequestFields(started, usage)
		fields["tool_calls"] = stats.toolCalls
		fields["chunk_count"] = stats.chunkCount
		fields["assistant_bytes"] = stats.assistantBytes
		fields["reasoning_bytes"] = stats.reasoningBytes
		fields["usage_received"] = stats.usageReceived
		fields["finish_shape"] = inferLLMFinishShape(stats, failed)
		addLLMRequestFinishFields(fields, stats.finish)
		if failed {
			fields["last_chunk_age_ms"] = time.Since(stats.lastChunkAt).Milliseconds()
			logLLMRequestFailure(req, route, modelErr, fields)
			return
		}
		logLLMRequestSuccess(req, route, fields)
	}()
	return out
}

// collectLLMRequestStream 只旁路统计并转发 chunk，不改变 provider 返回内容。
func collectLLMRequestStream(raw <-chan Chunk, out chan<- Chunk, started time.Time) (*Usage, llmRequestStreamStats, bool, *ModelError) {
	stats := llmRequestStreamStats{lastChunkAt: started}
	var usage *Usage
	for chunk := range raw {
		if chunk.Error != nil {
			out <- chunk
			return usage, stats, true, chunk.Error
		}
		stats.chunkCount++
		stats.lastChunkAt = time.Now()
		stats.assistantBytes += len(chunk.Content)
		stats.reasoningBytes += len(chunk.ReasoningContent)
		stats.toolCalls += len(chunk.ToolCalls)
		if chunk.Usage != nil {
			usage = chunk.Usage
			stats.usageReceived = true
		}
		if chunk.Finish != nil {
			stats.finish = chunk.Finish
		}
		out <- chunk
	}
	return usage, stats, false, nil
}

func llmRequestFields(started time.Time, usage *Usage) logging.Event {
	fields := logging.Event{"duration_ms": time.Since(started).Milliseconds()}
	if usage != nil {
		fields["input_tokens"] = usage.InputTokens
		fields["output_tokens"] = usage.OutputTokens
		fields["cache_read_tokens"] = usage.CacheReadTokens
		fields["cache_creation_tokens"] = usage.CacheCreationTokens
		fields["context_tokens"] = usage.TotalTokens
	}
	return fields
}

func addLLMRequestFinishFields(fields logging.Event, finish *FinishInfo) {
	if fields == nil || finish == nil {
		return
	}
	if finish.Reason != "" {
		fields["finish_reason"] = finish.Reason
	}
	if finish.Status != "" {
		fields["finish_status"] = finish.Status
	}
	if finish.NativeReason != "" && finish.NativeReason != finish.Reason {
		fields["native_finish_reason"] = finish.NativeReason
	}
	if finish.IncompleteReason != "" {
		fields["incomplete_reason"] = finish.IncompleteReason
	}
	if finish.StopSequence != "" {
		fields["stop_sequence"] = finish.StopSequence
	}
}

func inferLLMFinishShape(stats llmRequestStreamStats, failed bool) string {
	if failed {
		return "failed"
	}
	if stats.assistantBytes > 0 && stats.toolCalls > 0 {
		return "text_tool_call"
	}
	if stats.assistantBytes > 0 {
		return "text"
	}
	if stats.toolCalls > 0 {
		return "tool_call"
	}
	if stats.reasoningBytes > 0 {
		return "reasoning_only"
	}
	return "empty"
}

func requestID(req *CompletionRequest) string {
	if req != nil && req.RequestID != "" {
		return req.RequestID
	}
	return uuid.New().String()
}

func purpose(req *CompletionRequest) string {
	if req != nil && req.Purpose != "" {
		return req.Purpose
	}
	return "unknown"
}

func logLLMRequestSuccess(req *CompletionRequest, route llmRoute, fields logging.Event) {
	logLLMRequest("INFO", req, route, "success", nil, fields)
}

func logLLMRequestFailure(req *CompletionRequest, route llmRoute, err error, fields logging.Event) {
	if fields == nil {
		fields = logging.Event{}
	}
	if modelErr, ok := err.(*ModelError); ok && modelErr != nil {
		if modelErr.StatusCode > 0 {
			fields["status_code"] = modelErr.StatusCode
		}
		if modelErr.Code != "" {
			fields["provider_code"] = modelErr.Code
		}
		if modelErr.Type != "" {
			fields["provider_type"] = modelErr.Type
		}
	}
	logLLMRequest("ERROR", req, route, "failed", err, fields)
}

func logLLMRequest(level string, req *CompletionRequest, route llmRoute, status string, err error, fields logging.Event) {
	if fields == nil {
		fields = logging.Event{}
	}
	fields["request_id"] = requestID(req)
	fields["purpose"] = purpose(req)
	fields["status"] = status
	if route.Adapter != "" {
		fields["provider"] = route.Adapter
	}
	if route.Protocol != "" {
		fields["protocol"] = route.Protocol
	}
	if route.ModelRef != "" {
		fields["model_ref"] = route.ModelRef
	}
	if route.Model != "" {
		fields["model"] = route.Model
	} else if req != nil {
		fields["model"] = req.Model
	}
	if req != nil {
		fields["request_messages"] = len(req.Messages)
		fields["tool_defs"] = len(req.Tools)
		fields["max_tokens"] = req.MaxTokens
		if req.Temperature != nil {
			fields["temperature"] = *req.Temperature
		}
	}
	logLLMDebugRequest(req, route, fields["request_id"])
	if level == "ERROR" {
		logging.Error("llm", "request", err, fields)
		return
	}
	if err != nil {
		fields["err"] = fmt.Sprintf("%v", err)
	}
	logging.Info("llm", "request", fields)
}

func logLLMDebugRequest(req *CompletionRequest, route llmRoute, requestID any) {
	if !llmDebugEnabled() || req == nil {
		return
	}
	messages, err := json.Marshal(llmDebugMessages(req.Messages))
	if err != nil {
		messages = []byte(`[]`)
	}
	fields := logging.Event{
		"request_id":          fmt.Sprintf("%v", requestID),
		"purpose":             purpose(req),
		"model_ref":           route.ModelRef,
		"model":               route.Model,
		"system_chars":        len(req.System),
		"session_state_chars": len(req.SessionState),
		"messages_json":       string(messages),
		"tool_names":          strings.Join(llmToolNames(req.Tools), ","),
	}
	if req.Temperature != nil {
		fields["temperature"] = *req.Temperature
	}
	logging.Info("llm", "debug_request", fields)
}

func llmDebugEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SUNA_LLM_DEBUG"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func llmDebugMessages(messages []Message) []llmDebugMessage {
	out := make([]llmDebugMessage, 0, len(messages))
	for i, msg := range messages {
		text, textTruncated := truncateLLMDebugText(msg.Text(), llmDebugTextLimit)
		item := llmDebugMessage{
			Index:      i,
			Role:       string(msg.Role),
			Text:       text,
			ToolCallID: msg.ToolCallID,
			TextBytes:  len(msg.Text()),
			Truncated:  textTruncated,
		}
		if len(msg.Content) > 0 {
			item.Content = llmDebugContentBlocks(msg.Content)
		}
		if len(msg.ToolCalls) > 0 {
			item.ToolCalls = llmDebugToolCalls(msg.ToolCalls)
		}
		out = append(out, item)
	}
	return out
}

func llmDebugContentBlocks(blocks []ContentBlock) []llmDebugContent {
	out := make([]llmDebugContent, 0, len(blocks))
	for _, block := range blocks {
		item := llmDebugContent{Type: string(block.Type)}
		if block.Type == ContentText {
			item.Text, item.Truncated = truncateLLMDebugText(block.Text, llmDebugTextLimit)
			item.TextBytes = len(block.Text)
		}
		if block.Media != nil {
			item.MediaKind = string(block.Media.Kind)
			item.MediaName = block.Media.Name
			item.MediaMime = block.Media.MimeType
			item.MediaSize = block.Media.Size
		}
		out = append(out, item)
	}
	return out
}

func llmDebugToolCalls(calls []ToolCall) []llmDebugToolCall {
	out := make([]llmDebugToolCall, 0, len(calls))
	for _, call := range calls {
		args, truncated := truncateLLMDebugText(call.Arguments, llmDebugTextLimit)
		out = append(out, llmDebugToolCall{
			ID:            call.ID,
			Name:          call.Name,
			Arguments:     args,
			ArgumentBytes: len(call.Arguments),
			ArgsTruncated: truncated,
		})
	}
	return out
}

func llmToolNames(tools []ToolDef) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		if strings.TrimSpace(tool.Name) != "" {
			names = append(names, tool.Name)
		}
	}
	return names
}

func truncateLLMDebugText(text string, limit int) (string, bool) {
	if limit <= 0 || len(text) <= limit {
		return text, false
	}
	end := 0
	for end < len(text) {
		_, size := utf8.DecodeRuneInString(text[end:])
		if end+size > limit {
			break
		}
		end += size
	}
	return text[:end] + "...[truncated]", true
}
