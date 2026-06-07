package agent

import (
	"context"
	"strings"

	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/tools"
)

func (a *Agent) enqueueMemoryEvent(ctx context.Context, role model.Role, content string, hadToolCall, toolFailed, guardBlocked, userCorrection bool) {
	if a.extractQueue == nil || content == "" {
		return
	}
	sig := memory.JudgeSignificance(content, "", hadToolCall, toolFailed, guardBlocked, userCorrection)
	if sig == memory.SignificanceLow {
		return
	}
	a.extractQueue.Push(ctx, memory.DefaultUserID, string(role), content, sig)
}

func (a *Agent) replaceLastUserMessage(text string, replacement model.Message) {
	msgs := a.working.Messages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == model.RoleUser && msgs[i].Text() == text {
			msgs[i] = replacement
			a.working.SetMessages(msgs)
			return
		}
	}
}

func (a *Agent) saveConversationState(ctx context.Context) {
	if a.conversation == nil || a.working == nil {
		return
	}
	msgs := a.working.Messages()
	_ = a.conversation.Save(ctx, memory.DefaultUserID, memory.BuildResumeSummary(msgs), msgs, a.toolSummary)
}

func (a *Agent) addToolSummary(name string, result tools.Result) {
	if name == "" {
		return
	}
	status := "success"
	if result.IsError {
		status = "error"
	}
	summary := summarizeToolResult(result.Content)
	if summary == "" {
		summary = "completed"
	}
	a.toolSummary = append(a.toolSummary, memory.ToolSummaryItem{Name: name, Status: status, Summary: summary})
}

func summarizeToolResult(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	compact := make([]string, 0, 2)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		compact = append(compact, line)
		if len(compact) >= 2 {
			break
		}
	}
	return strings.Join(compact, " | ")
}
