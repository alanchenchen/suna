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

	got := logRequestPrepare(Request{Purpose: "chat", ModelRef: "P/model", ModelID: "model", AutoCompress: true}, completionReq, 400000, 1)
	want := estimateInputTokens(completionReq)
	if got != want {
		t.Fatalf("got %d, want %d", got, want)
	}
	if got == estimateRequestTokens(completionReq) {
		t.Fatalf("got %d, want input-only estimate without max output budget", got)
	}
}

func TestEstimatorSafetyTokens(t *testing.T) {
	if got, want := estimatorSafetyTokens(1000), 8192; got != want {
		t.Fatalf("small estimate got %d, want %d", got, want)
	}
	if got, want := estimatorSafetyTokens(178335), 11145; got != want {
		t.Fatalf("large estimate got %d, want %d", got, want)
	}
	if got, want := compactContextTokens(178335), 189480; got != want {
		t.Fatalf("compact estimate got %d, want %d", got, want)
	}
}

func TestShouldCompactRequestUsesEstimatorSafety(t *testing.T) {
	req := &model.CompletionRequest{
		Messages:  []model.Message{model.NewTextMessage(model.RoleUser, strings.Repeat("a", 1000))},
		MaxTokens: 128000,
	}
	estimated := estimateInputTokens(req)
	limit := compactContextTokens(estimated) - 1
	contextWindow := req.MaxTokens + minContextMarginTokens + limit
	if !shouldCompactRequest(req, contextWindow) {
		t.Fatal("expected request to compact when estimate plus safety exceeds usable budget")
	}
}
