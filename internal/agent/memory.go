package agent

import (
	"context"
	"strings"
	"time"

	"github.com/alanchenchen/suna/internal/logging"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/tools"
)

func (a *Agent) enqueueMemoryEvent(ctx context.Context, role model.Role, content string, hadToolCall, toolFailed, guardBlocked, userCorrection bool) {
	if a.extractQueue == nil || content == "" || role != model.RoleUser || hadToolCall || toolFailed || guardBlocked {
		return
	}
	candidate, ok := memory.ExtractCandidate(content, userCorrection)
	if !ok {
		return
	}
	a.extractQueue.Push(ctx, memory.DefaultUserID, a.modelRef, candidate)
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
	if a.stateStore == nil || a.working == nil || a.sessionID == "" {
		return
	}
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	msgs := a.working.Messages()
	if err := a.stateStore.Save(saveCtx, a.sessionID, strings.TrimSpace(a.sessionState), msgs, a.toolSummary); err != nil {
		logging.Error("agent", "save_session_state_failed", err, nil)
	}
	if a.sessionStore != nil {
		if err := a.sessionStore.SetMessageCount(saveCtx, a.sessionID, len(visibleMessagesForCount(msgs))); err != nil {
			logging.Error("agent", "save_session_meta_failed", err, nil)
		}
	}
}

func (a *Agent) commitCompactState(ctx context.Context, sessionState string) error {
	sessionState = strings.TrimSpace(sessionState)
	if sessionState == "" {
		return nil
	}
	a.sessionState = sessionState
	if a.stateStore == nil || a.working == nil || a.sessionID == "" {
		return nil
	}
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err := a.stateStore.Save(saveCtx, a.sessionID, a.sessionState, a.working.Messages(), a.toolSummary); err != nil {
		logging.Error("agent", "save_compact_state_failed", err, nil)
		return err
	}
	if a.sessionStore != nil {
		if err := a.sessionStore.SetMessageCount(saveCtx, a.sessionID, len(visibleMessagesForCount(a.working.Messages()))); err != nil {
			logging.Error("agent", "save_session_meta_failed", err, nil)
			return err
		}
	}
	return nil
}

func visibleMessagesForCount(msgs []model.Message) []model.Message {
	visible := make([]model.Message, 0, len(msgs))
	for _, msg := range msgs {
		if (msg.Role == model.RoleUser || msg.Role == model.RoleAssistant) && strings.TrimSpace(msg.Text()) != "" {
			visible = append(visible, msg)
		}
	}
	return visible
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
	a.toolSummary = a.toolSummary.Add(memory.ToolSummaryItem{Name: name, Status: status, Summary: summary})
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
