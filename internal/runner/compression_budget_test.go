package runner

import (
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/model"
)

func TestUsableInputBudgetReservesOutputAndMargin(t *testing.T) {
	// 200k 窗口、32k 输出：margin = 200000/200 = 1000，低于下限 2048，取 2048。
	got := usableInputBudget(200000, 32000)
	want := 200000 - 32000 - 2048
	if got != want {
		t.Fatalf("usableInputBudget() = %d, want %d", got, want)
	}
}

func TestUsableInputBudgetHasMinimumOne(t *testing.T) {
	// 窗口小于输出+margin 时兜底为 1，避免除零/负预算。
	got := usableInputBudget(1000, 5000)
	if got != 1 {
		t.Fatalf("usableInputBudget() = %d, want 1", got)
	}
}

func TestContextMarginScalesWithWindowAndHasMinimum(t *testing.T) {
	tests := []struct {
		name          string
		contextWindow int
		want          int
	}{
		{"small window uses minimum", 100000, minContextMarginTokens},
		{"large window scales", 1000000, 5000},
		{"tiny window still minimum", 1000, minContextMarginTokens},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contextMargin(tt.contextWindow)
			if got != tt.want {
				t.Fatalf("contextMargin(%d) = %d, want %d", tt.contextWindow, got, tt.want)
			}
		})
	}
}

func TestEstimatorSafetyTokensCalibratedUncalibrated(t *testing.T) {
	tests := []struct {
		name       string
		estimated  int
		calibrated bool
		want       int
	}{
		{"uncalibrated small uses floor", 10000, false, 8192},
		{"uncalibrated large scales", 200000, false, 12500},
		{"calibrated small uses floor", 10000, true, 2048},
		{"calibrated large scales", 200000, true, 5000},
		{"zero input", 0, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimatorSafetyTokens(tt.estimated, tt.calibrated)
			if got != tt.want {
				t.Fatalf("estimatorSafetyTokens(%d, %v) = %d, want %d", tt.estimated, tt.calibrated, got, tt.want)
			}
		})
	}
}

func TestCompactContextTokensAddsSafety(t *testing.T) {
	got := compactContextTokens(100000, false)
	want := 100000 + 8192
	if got != want {
		t.Fatalf("compactContextTokens() = %d, want %d", got, want)
	}
}

func TestCompactContextTokensZeroInput(t *testing.T) {
	if got := compactContextTokens(0, true); got != 0 {
		t.Fatalf("compactContextTokens(0) = %d, want 0", got)
	}
}

func TestShouldCompactRequestTriggersWhenOverBudget(t *testing.T) {
	req := &model.CompletionRequest{
		System:    strings.Repeat("s", 100000),
		MaxTokens: 32000,
	}
	// 大 system 让估算明显超过可用预算：100k 字符 ≈ 25k tokens + 8k safety > 60k 窗口预算。
	if !shouldCompactRequest(req, 60000, 1.0, false) {
		t.Fatal("shouldCompactRequest() = false, want true (over budget)")
	}
}

func TestShouldCompactRequestSkipsNilOrInvalid(t *testing.T) {
	if shouldCompactRequest(nil, 100000, 1.0, false) {
		t.Fatal("shouldCompactRequest(nil) = true, want false")
	}
	req := &model.CompletionRequest{MaxTokens: 32000}
	if shouldCompactRequest(req, 0, 1.0, false) {
		t.Fatal("shouldCompactRequest(ctx=0) = true, want false")
	}
}

func TestEstimateRequestTokensIncludesMaxOutput(t *testing.T) {
	req := &model.CompletionRequest{
		System:    "hello",
		MaxTokens: 64000,
	}
	got := estimateRequestTokens(req, 1.0)
	input := estimateInputTokens(req, 1.0)
	if got != input+64000 {
		t.Fatalf("estimateRequestTokens() = %d, want input %d + max 64000", got, input)
	}
}

func TestEstimateInputTokensNilRequest(t *testing.T) {
	if got := estimateInputTokens(nil, 1.0); got != 0 {
		t.Fatalf("estimateInputTokens(nil) = %d, want 0", got)
	}
}

func TestEstimateInputTokensCountsSystemSessionMessagesAndTools(t *testing.T) {
	req := &model.CompletionRequest{
		System:       "sys",
		SessionState: "state",
		Messages:     []model.Message{{Role: model.RoleUser, TextContent: "hi"}},
		Tools:        []model.ToolDef{{Name: "tool-a"}},
	}
	got := estimateInputTokens(req, 1.0)
	if got <= 0 {
		t.Fatalf("estimateInputTokens() = %d, want > 0", got)
	}
	// coef=2.0 应放大估算（ApplyCoefficient 按系数缩放）。
	scaled := estimateInputTokens(req, 2.0)
	if scaled <= got {
		t.Fatalf("estimateInputTokens(coef=2) = %d, want > %d", scaled, got)
	}
}

func TestCompactRecentBudgetNilRequest(t *testing.T) {
	if got := compactRecentBudget(nil, 100000, 1.0, false); got != 0 {
		t.Fatalf("compactRecentBudget(nil) = %d, want 0", got)
	}
}

func TestCompactRecentBudgetFitsInUsableSpace(t *testing.T) {
	req := &model.CompletionRequest{MaxTokens: 32000}
	got := compactRecentBudget(req, 200000, 1.0, false)
	usable := usableInputBudget(200000, 32000)
	if got <= 0 || got > usable {
		t.Fatalf("compactRecentBudget() = %d, want in (0, %d]", got, usable)
	}
}

func TestCountLargeToolOutputsCountsOnlyOversizedToolMessages(t *testing.T) {
	msgs := []model.Message{
		{Role: model.RoleTool, ToolCallID: "a", Content: []model.ContentBlock{{Type: model.ContentText, Text: strings.Repeat("x", 60*1024)}}},
		{Role: model.RoleTool, ToolCallID: "b", Content: []model.ContentBlock{{Type: model.ContentText, Text: "small"}}},
		{Role: model.RoleUser, Content: []model.ContentBlock{{Type: model.ContentText, Text: strings.Repeat("y", 60*1024)}}},
	}
	got := countLargeToolOutputs(msgs)
	if got != 1 {
		t.Fatalf("countLargeToolOutputs() = %d, want 1", got)
	}
}

func TestTrimToolResultsForContextTruncatesOversizedOnly(t *testing.T) {
	big := strings.Repeat("x", 200*1024)
	msgs := []model.Message{
		{Role: model.RoleTool, ToolCallID: "a", Content: []model.ContentBlock{{Type: model.ContentText, Text: big}}},
		{Role: model.RoleTool, ToolCallID: "b", Content: []model.ContentBlock{{Type: model.ContentText, Text: "small"}}},
	}
	out := trimToolResultsForContext(msgs)
	if len(out) != 2 {
		t.Fatalf("trimToolResultsForContext() returned %d messages, want 2", len(out))
	}
	if len(out[0].Text()) >= len(big) {
		t.Fatal("oversized tool message was not trimmed")
	}
	if out[1].Text() != "small" {
		t.Fatalf("small tool message changed: %q", out[1].Text())
	}
}
