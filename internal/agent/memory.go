package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/alanchenchen/suna/internal/logging"
	"github.com/alanchenchen/suna/internal/media"
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

func (a *Agent) replaceRunInputMessage(original, replacement model.Message) {
	if !messageHasImage(original) {
		return
	}
	msgs := a.working.Messages()
	for i := range msgs {
		if msgs[i].Role == model.RoleUser && msgs[i].Text() == original.Text() && messageHasImage(msgs[i]) {
			msgs[i] = replacement
			a.working.SetMessages(msgs)
			return
		}
	}
}

// replaceToolImagesWithSummaries 把 working 中所有带图片块的 user 消息替换为纯文本摘要，
// 覆盖用户传图（含多图）与 read_image 注入的图片消息。图片块只参与当前 run，
// 摘要文本（带 source）作为历史引用保留，供后续轮次通过 read_image 读回。
func (a *Agent) replaceToolImagesWithSummaries() {
	msgs := a.working.Messages()
	changed := false
	for i := range msgs {
		if msgs[i].Role != model.RoleUser || !messageHasImage(msgs[i]) {
			continue
		}
		// 保留原有文本（如用户输入），图片块替换为摘要文本；无文本时摘要直接作为消息内容。
		text := strings.TrimSpace(msgs[i].Text())
		summaries := imageSummaries(msgs[i].Content)
		if len(summaries) > 0 {
			if text != "" {
				text += "\n"
			}
			text += strings.Join(summaries, "\n")
		}
		msgs[i] = model.NewTextMessage(model.RoleUser, text)
		changed = true
	}
	if changed {
		a.working.SetMessages(msgs)
	}
}

// imageSummaries 把图片块转换为带 source 的摘要文本，格式与 daemon 端 attachmentSummary 一致。
func imageSummaries(blocks []model.ContentBlock) []string {
	var out []string
	for _, b := range blocks {
		if b.Type != model.ContentImage || b.Media == nil {
			continue
		}
		ref := b.Media
		parts := []string{fmt.Sprintf("[image: %s", ref.Name)}
		if ref.MimeType != "" {
			parts = append(parts, ref.MimeType)
		}
		if ref.Size > 0 {
			parts = append(parts, media.FormatSize(ref.Size))
		}
		switch ref.Kind {
		case model.MediaAttachment:
			if ref.Name != "" {
				parts = append(parts, "source=attachment:"+ref.Name)
			}
		case model.MediaPath:
			if ref.Path != "" {
				parts = append(parts, "source="+ref.Path)
			}
		case model.MediaURL:
			if ref.URL != "" {
				parts = append(parts, "source="+ref.URL)
			}
		}
		out = append(out, strings.Join(parts, ", ")+"]")
	}
	return out
}

func messageHasImage(msg model.Message) bool {
	for _, block := range msg.Content {
		if block.Type == model.ContentImage && block.Media != nil {
			return true
		}
	}
	return false
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
