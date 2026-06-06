package model

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/alanchenchen/suna/internal/logging"
)

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

func logLLMSuccess(req *CompletionRequest, fields logging.Event) {
	logLLM("INFO", req, "success", nil, fields)
}

func logLLMFailure(req *CompletionRequest, err error, fields logging.Event) {
	logLLM("ERROR", req, "failed", err, fields)
}

func logLLM(level string, req *CompletionRequest, status string, err error, fields logging.Event) {
	if fields == nil {
		fields = logging.Event{}
	}
	fields["request_id"] = requestID(req)
	fields["purpose"] = purpose(req)
	fields["status"] = status
	if req != nil {
		fields["model"] = req.Model
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
