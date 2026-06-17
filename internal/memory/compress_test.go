package memory

import (
	"context"
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/prompt"
)

type captureCompressProvider struct {
	request *model.CompletionRequest
	text    string
}

func (p *captureCompressProvider) Complete(ctx context.Context, req *model.CompletionRequest) (<-chan model.Chunk, error) {
	p.request = req
	ch := make(chan model.Chunk, 1)
	ch <- model.Chunk{Content: p.text, Done: true}
	close(ch)
	return ch, nil
}
func (p *captureCompressProvider) EstimateTokens(text string) int { return len(text) / 4 }
func (p *captureCompressProvider) ContextWindow() int             { return 100000 }
func (p *captureCompressProvider) MaxOutputTokens() int           { return 8192 }

func TestCompressHistoryBuildsSessionState(t *testing.T) {
	provider := &captureCompressProvider{text: "# Session State\n\n## Active context\n- continue task"}
	compressor := NewCompressor(provider)
	loader, err := prompt.New()
	if err != nil {
		t.Fatal(err)
	}
	compressor.SetPrompts(loader)

	messages := []model.Message{
		model.NewTextMessage(model.RoleUser, "我要一个最小改动方案，同时记住之前讨论的约束。"+strings.Repeat("补充背景。", 80)),
		model.NewTextMessage(model.RoleAssistant, "可以新增很多协议字段。"),
		model.NewTextMessage(model.RoleUser, "不要新增复杂语义，复用现有 compact。"),
		model.NewTextMessage(model.RoleAssistant, "好的，改为 continuation state。"),
	}
	compressed, summary, folded, err := compressor.compressHistoryKeepingState(context.Background(), messages, "", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if summary != provider.text {
		t.Fatalf("summary = %q, want provider text", summary)
	}
	if folded != 3 {
		t.Fatalf("folded = %d, want 3", folded)
	}
	if len(compressed) != 1 {
		t.Fatalf("len(compressed) = %d, want 1 recent message", len(compressed))
	}
	if compressed[0].Text() != "好的，改为 continuation state。" {
		t.Fatalf("recent message = %q, want latest assistant message", compressed[0].Text())
	}
	input := provider.request.Messages[0].Text()
	for _, want := range []string{"# Session State", "## Completed work / topic ledger", "## User requirements and decisions", "<user_message", "不要新增复杂语义"} {
		if !strings.Contains(input, want) {
			t.Fatalf("compress input missing %q:\n%s", want, input)
		}
	}
}

func TestFormatCompressInputDenoisesToolOutput(t *testing.T) {
	longToolOutput := strings.Repeat("tool-log-line\n", 800)
	messages := []model.Message{
		model.NewTextMessage(model.RoleUser, "请检查压缩策略，不要只按 coding 优化。"),
		{
			Role:      model.RoleAssistant,
			ToolCalls: []model.ToolCall{{ID: "call-1", Name: "readfile", Arguments: `{"path":"internal/memory/compress.go","content":"` + strings.Repeat("x", 6000) + `"}`}},
		},
		{Role: model.RoleTool, ToolCallID: "call-1", TextContent: longToolOutput, Content: []model.ContentBlock{{Type: model.ContentText, Text: longToolOutput}}},
	}

	input := formatCompressInput(messages)
	for _, want := range []string{"<user_message", "不要只按 coding 优化", "<tool_call", `name="readfile"`, "<tool_result", "truncated"} {
		if !strings.Contains(input, want) {
			t.Fatalf("formatted input missing %q:\n%s", want, input)
		}
	}
	if strings.Contains(input, strings.Repeat("tool-log-line\n", 500)) {
		t.Fatalf("formatted input kept too much raw tool output")
	}
	if strings.Contains(input, strings.Repeat("x", 5000)) {
		t.Fatalf("formatted input kept too much raw tool arguments")
	}
}

func TestChooseRecentKeepUsesTokenBudget(t *testing.T) {
	messages := []model.Message{
		model.NewTextMessage(model.RoleUser, strings.Repeat("很长的早期内容", 200)),
		model.NewTextMessage(model.RoleAssistant, strings.Repeat("很长的回复", 200)),
		model.NewTextMessage(model.RoleUser, strings.Repeat("最新请求", 200)),
	}
	keep := chooseRecentKeepWithBudget(messages, 12000, 500)
	if keep != 1 {
		t.Fatalf("keep = %d, want 1 for tight budget", keep)
	}
}

func TestChooseRecentKeepKeepsMoreChatTurnsWhenBudgetAllows(t *testing.T) {
	messages := []model.Message{
		model.NewTextMessage(model.RoleUser, "u1"),
		model.NewTextMessage(model.RoleAssistant, "a1"),
		model.NewTextMessage(model.RoleUser, "u2"),
		model.NewTextMessage(model.RoleAssistant, "a2"),
		model.NewTextMessage(model.RoleUser, "u3"),
	}
	keep := chooseRecentKeep(messages, 200000)
	if keep != len(messages)-1 {
		t.Fatalf("keep = %d, want %d", keep, len(messages)-1)
	}
}

func TestChooseRecentKeepUsesSmallerToolWindow(t *testing.T) {
	messages := []model.Message{
		model.NewTextMessage(model.RoleUser, "u1"),
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "1", Name: "readfile"}}},
		{Role: model.RoleTool, ToolCallID: "1", TextContent: "r1"},
		model.NewTextMessage(model.RoleUser, "u2"),
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "2", Name: "exec"}}},
		{Role: model.RoleTool, ToolCallID: "2", TextContent: "r2"},
		model.NewTextMessage(model.RoleUser, "u3"),
	}
	keep := chooseRecentKeep(messages, 200000)
	if keep != 4 {
		t.Fatalf("keep = %d, want 4 for recent 2 user turns in tool-heavy context", keep)
	}
}

func TestSessionStateMaxTokensBounds(t *testing.T) {
	tests := []struct {
		name string
		ctx  int
		want int
	}{
		{name: "default", ctx: 0, want: 2000},
		{name: "small", ctx: 32000, want: minSessionStateTokens},
		{name: "large", ctx: 1000000, want: maxSessionStateTokens},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if got := sessionStateMaxTokens(tt.ctx); got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}
