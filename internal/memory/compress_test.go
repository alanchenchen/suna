package memory

import (
	"context"
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/prompt"
)

func TestCompressHistoryBuildsSessionState(t *testing.T) {
	summaryText := "# Session State\n\n## Active context\n- continue task"
	var requestBody []byte
	binding := newTestModelBinding(t, summaryText, func(body []byte) { requestBody = body })
	compressor := NewCompressor()
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
	compressed, summary, folded, err := compressor.compressHistoryKeepingState(context.Background(), binding, messages, "", 1, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if summary != summaryText {
		t.Fatalf("summary = %q, want %q", summary, summaryText)
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
	input := string(requestBody)
	for _, want := range []string{"# Session State", "## Completed work / topic ledger", "## User requirements and decisions", "<user_message", "不要新增复杂语义"} {
		if !strings.Contains(input, want) {
			t.Fatalf("compress input missing %q:\n%s", want, input)
		}
	}
}

func TestCompressHistoryPreservesToolCallParentForRecentToolResult(t *testing.T) {
	binding := newTestModelBinding(t, "# Session State\n\n## Active context\n- compacted", nil)
	compressor := NewCompressor()
	loader, err := prompt.New()
	if err != nil {
		t.Fatal(err)
	}
	compressor.SetPrompts(loader)

	messages := []model.Message{
		model.NewTextMessage(model.RoleUser, "older request"),
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "call-1", Name: "readfile", Arguments: `{"path":"a"}`}}},
		{Role: model.RoleTool, ToolCallID: "call-1", TextContent: "result"},
	}
	compressed, _, _, err := compressor.compressHistoryKeepingState(context.Background(), binding, messages, "", 1, 400000, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(compressed) != 2 {
		t.Fatalf("len(compressed) = %d, want parent assistant and tool result", len(compressed))
	}
	if compressed[0].Role != model.RoleAssistant || len(compressed[0].ToolCalls) != 1 || compressed[0].ToolCalls[0].ID != "call-1" {
		t.Fatalf("compressed[0] = %+v, want assistant tool_call call-1", compressed[0])
	}
	if compressed[1].Role != model.RoleTool || compressed[1].ToolCallID != "call-1" {
		t.Fatalf("compressed[1] = %+v, want tool result call-1", compressed[1])
	}
}

func TestCompressHistoryPreservesParallelToolCallParentForRecentToolResult(t *testing.T) {
	binding := newTestModelBinding(t, "# Session State\n\n## Active context\n- compacted", nil)
	compressor := NewCompressor()
	loader, err := prompt.New()
	if err != nil {
		t.Fatal(err)
	}
	compressor.SetPrompts(loader)

	messages := []model.Message{
		model.NewTextMessage(model.RoleUser, "older request"),
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{
			{ID: "call-a", Name: "readfile", Arguments: `{"path":"a"}`},
			{ID: "call-b", Name: "readfile", Arguments: `{"path":"b"}`},
		}},
		{Role: model.RoleTool, ToolCallID: "call-a", TextContent: "result a"},
		{Role: model.RoleTool, ToolCallID: "call-b", TextContent: "result b"},
	}
	compressed, _, _, err := compressor.compressHistoryKeepingState(context.Background(), binding, messages, "", 1, 400000, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(compressed) != 3 {
		t.Fatalf("len(compressed) = %d, want assistant and both retained tool results", len(compressed))
	}
	if compressed[0].Role != model.RoleAssistant || len(compressed[0].ToolCalls) != 2 {
		t.Fatalf("compressed[0] = %+v, want assistant with parallel tool calls", compressed[0])
	}
	if compressed[2].Role != model.RoleTool || compressed[2].ToolCallID != "call-b" {
		t.Fatalf("compressed[2] = %+v, want recent tool result call-b", compressed[2])
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
