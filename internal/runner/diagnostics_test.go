package runner

import (
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/model"
)

func TestLogRequestPrepareReturnsEstimatedInputContext(t *testing.T) {
	completionReq := &model.CompletionRequest{
		RequestID: "test-request",
		System:    "system prompt",
		Messages: []model.Message{
			{Role: model.RoleUser, TextContent: "hello"},
		},
		MaxTokens: 128000,
	}

	raw, calibrated := logRequestPrepare(Request{Purpose: "chat", ModelRef: "P/model", ModelID: "model", AutoCompress: true}, completionReq, 400000, 1, 1.0, false)
	want := estimateInputTokens(completionReq, 1.0)
	if raw != want {
		t.Fatalf("raw got %d, want %d", raw, want)
	}
	if calibrated != want {
		t.Fatalf("calibrated got %d, want %d at coef 1.0", calibrated, want)
	}
	if raw == estimateRequestTokens(completionReq, 1.0) {
		t.Fatalf("got %d, want input-only estimate without max output budget", raw)
	}
}

func TestEstimatorSafetyTokens(t *testing.T) {
	// 未校准：维持 1/16，最低 8192。
	if got, want := estimatorSafetyTokens(1000, false), 8192; got != want {
		t.Fatalf("uncalibrated small estimate got %d, want %d", got, want)
	}
	if got, want := estimatorSafetyTokens(178335, false), 11145; got != want {
		t.Fatalf("uncalibrated large estimate got %d, want %d", got, want)
	}
	if got, want := compactContextTokens(178335, false), 189480; got != want {
		t.Fatalf("uncalibrated compact estimate got %d, want %d", got, want)
	}
	// 已校准：收到 1/40，最低 2048。
	if got, want := estimatorSafetyTokens(1000, true), 2048; got != want {
		t.Fatalf("calibrated small estimate got %d, want %d", got, want)
	}
	if got, want := estimatorSafetyTokens(178335, true), 4458; got != want {
		t.Fatalf("calibrated large estimate got %d, want %d", got, want)
	}
}

func TestShouldCompactRequestUsesEstimatorSafety(t *testing.T) {
	req := &model.CompletionRequest{
		Messages:  []model.Message{model.NewTextMessage(model.RoleUser, strings.Repeat("a", 1000))},
		MaxTokens: 128000,
	}
	estimated := estimateInputTokens(req, 1.0)
	limit := compactContextTokens(estimated, false) - 1
	contextWindow := req.MaxTokens + minContextMarginTokens + limit
	if !shouldCompactRequest(req, contextWindow, 1.0, false) {
		t.Fatal("expected request to compact when estimate plus safety exceeds usable budget")
	}
}
