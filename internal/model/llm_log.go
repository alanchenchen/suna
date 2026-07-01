package model

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/logging"
)

type llmRoute struct {
	Provider string
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
		Provider: mc.Provider,
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
	out := make(chan Chunk, providerChunkBuffer)
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
		fields["cached_tokens"] = usage.CachedTokens
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
	if route.Provider != "" {
		fields["provider"] = route.Provider
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
		fields["temperature"] = req.Temperature
	}
	if level == "ERROR" {
		logging.Error("llm", "request", err, fields)
		return
	}
	if err != nil {
		fields["err"] = fmt.Sprintf("%v", err)
	}
	logging.Info("llm", "request", fields)
}
