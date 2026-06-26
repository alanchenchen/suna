package model

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/alanchenchen/suna/internal/logging"
)

type llmRoute struct {
	Provider string
	Protocol string
	ModelRef string
	Model    string
}

type llmStreamStats struct {
	chunkCount     int
	assistantBytes int
	reasoningBytes int
	toolCalls      int
	usageReceived  bool
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

func requestID(req *CompletionRequest) string {
	if req != nil && req.RequestID != "" {
		return req.RequestID
	}
	return uuid.New().String()
}

func loggingFields(started time.Time, usage *Usage) logging.Event {
	fields := logging.Event{"duration_ms": time.Since(started).Milliseconds()}
	if usage != nil {
		fields["input_tokens"] = usage.InputTokens
		fields["output_tokens"] = usage.OutputTokens
		fields["cached_tokens"] = usage.CachedTokens
		fields["context_tokens"] = usage.TotalTokens
	}
	return fields
}

func purpose(req *CompletionRequest) string {
	if req != nil && req.Purpose != "" {
		return req.Purpose
	}
	return "unknown"
}

func logRoutedLLMSuccess(req *CompletionRequest, route llmRoute, fields logging.Event) {
	logRoutedLLM("INFO", req, route, "success", nil, fields)
}

func logRoutedLLMFailure(req *CompletionRequest, route llmRoute, err error, fields logging.Event) {
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
	logRoutedLLM("ERROR", req, route, "failed", err, fields)
}

func logRoutedLLM(level string, req *CompletionRequest, route llmRoute, status string, err error, fields logging.Event) {
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
