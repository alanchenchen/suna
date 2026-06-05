package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/alanchenchen/suna/internal/protocol"
	tuievents "github.com/alanchenchen/suna/internal/tui/events"
	tuiconfig "github.com/alanchenchen/suna/internal/tui/pages/config"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
)

// Root reducer 使用 events 子包的强类型消息；handler 留在 root，负责分发到 page models。
type notificationMsg = tuievents.NotificationMsg

type streamMsg = tuievents.StreamMsg
type reasoningMsg = tuievents.ReasoningMsg
type usageMsg = tuievents.UsageMsg
type askUserMsg = tuievents.AskUserMsg
type guardConfirmMsg = tuievents.GuardConfirmMsg
type toolStartMsg = tuievents.ToolStartMsg
type toolGuardMsg = tuievents.ToolGuardMsg
type toolEndMsg = tuievents.ToolEndMsg
type daemonStateMsg = tuievents.DaemonStateMsg
type compactResultMsg = tuievents.CompactResultMsg
type memoryListMsg = tuievents.MemoryListMsg
type sessionRestoreMessageMsg = tuievents.SessionRestoreMessageMsg
type sessionRestoreInputMsg = tuievents.SessionRestoreInputMsg
type daemonFullStatusMsg = tuievents.DaemonFullStatusMsg
type configStateMsg = tuievents.ConfigStateMsg
type skillListMsg = tuievents.SkillListMsg
type skillLoadMsg = tuievents.SkillLoadMsg
type skillReviewMsg = tuievents.SkillReviewMsg
type attachmentStatusMsg = tuievents.AttachmentStatusMsg
type requestErrorMsg = tuievents.RequestErrorMsg

const (
	notifyCompactError = tuievents.NotifyCompactError
	notifyConfigError  = tuievents.NotifyConfigError
)

func decodeLocalNotification(notif localNotification) any {
	return tuievents.Decode(notif.toEvent())
}

func (t *TUI) handleLocalNotification(notif localNotification) {
	if msg, ok := decodeLocalNotification(notif).(notificationMsg); ok {
		t.handleNotificationMsg(msg)
	}
}

func (t *TUI) handleNotificationMsg(msg notificationMsg) {
	switch m := msg.(type) {
	case streamMsg:
		t.handleStreamNotification(m.Params)
	case reasoningMsg:
		t.handleReasoningNotification(m.Params)
	case usageMsg:
		t.handleUsageNotification(m.Params)
	case toolStartMsg:
		t.handleToolStartNotification(m.Params)
	case toolGuardMsg:
		t.handleToolGuardNotification(m.Params)
	case toolEndMsg:
		t.handleToolEndNotification(m.Params)
	case askUserMsg:
		t.handleAskUserNotification(m.Params)
	case guardConfirmMsg:
		t.handleGuardConfirmNotification(m.Params)
	case daemonStateMsg:
		t.handleDaemonStateNotification(m.Params)
	case compactResultMsg:
		t.handleCompactResultNotification(m.Params)
	case memoryListMsg:
		t.handleMemoryListNotification(m.Params)
	case sessionRestoreMessageMsg:
		t.handleSessionRestoreMessageNotification(m)
	case sessionRestoreInputMsg:
		t.handleSessionRestoreInputNotification(m)
	case daemonFullStatusMsg:
		t.handleDaemonFullStatusNotification(m.Params)
	case configStateMsg:
		t.handleConfigStateNotification(m.Params)
	case skillListMsg:
		t.handleSkillListNotification(m.Params)
	case skillLoadMsg:
		t.handleSkillLoadNotification(m.Params)
	case skillReviewMsg:
		t.handleSkillReviewNotification(m.Params)
	case attachmentStatusMsg:
		t.handleAttachmentStatusNotification(m.Params)
	case requestErrorMsg:
		t.handleRequestErrorNotification(m)
	}
}

func (t *TUI) handleStreamNotification(p protocol.StreamParams) {
	if p.Done {
		t.finishStreamingMessages()
		if strings.HasPrefix(p.Chunk, "error:") || p.Chunk == "cancelled" {
			t.appendNonToolMessage(chatMsg{Role: "error", Content: p.Chunk})
		}
		t.resetPhase()
		t.applyContextStats(p.ContextTokens, p.ContextWindow)
		return
	}
	t.chat.HandleStreamStart(time.Now())
	if p.Chunk != "" {
		t.chat.LastAssistantText += p.Chunk
	}
	t.appendStreamMessage("assistant", p.Chunk)
}

func (t *TUI) handleReasoningNotification(p protocol.StreamParams) {
	t.chat.HandleReasoningStart(time.Now())
	t.appendStreamMessage("reasoning", p.Chunk)
}

func (t *TUI) handleUsageNotification(p protocol.UsageParams) {
	t.hasUsage = true
	t.lastInputTok = p.InputTokens
	t.lastOutputTok = p.OutputTokens
	t.lastCachedTok = p.CachedTokens
	t.lastTokensPerSec = p.TokensPerSec
	if p.DurationMs > 0 {
		t.lastDuration = time.Duration(p.DurationMs) * time.Millisecond
	} else {
		t.lastDuration = 0
	}
	t.sessionInputTok += p.InputTokens
	t.sessionOutputTok += p.OutputTokens
	t.sessionCachedTok += p.CachedTokens
	t.applyContextStats(p.ContextTokens, p.ContextWindow)
}

func (t *TUI) handleAskUserNotification(p protocol.AskUserParams) {
	t.chat.SetAskPending(p)
	t.appendNonToolMessage(chatMsg{Role: "system", Content: "❓ " + p.Question})
	t.resetPhase()
}

func (t *TUI) handleGuardConfirmNotification(p protocol.GuardConfirmParams) {
	t.enqueueGuardConfirm(&guardConfirmView{ID: p.ID, ToolCallID: p.ToolCallID, Tool: p.Tool, Params: p.Params, Risk: p.Risk, Reason: p.Reason, Suggestion: p.Suggestion})
}

func (t *TUI) handleToolStartNotification(p protocol.ToolStartParams) {
	t.finishStreamingMessages()
	t.chat.Textarea.Blur()
	id := p.ID
	if id == "" {
		id = fmt.Sprintf("%s_%d", p.Tool, time.Now().UnixNano())
	}
	t.chat.StartTool(p, id, time.Now())
}

func (t *TUI) handleToolGuardNotification(p protocol.ToolGuardParams) {
	t.chat.ApplyToolGuard(p)
}

func (t *TUI) handleToolEndNotification(p protocol.ToolEndParams) {
	id := p.ID
	if id == "" {
		id = fmt.Sprintf("%s_%d", p.Tool, time.Now().UnixNano())
	}
	t.chat.EndTool(p, id, time.Now())
}

func (t *TUI) handleDaemonStateNotification(p protocol.DaemonStateParams) {
	if p.ProviderName != "" {
		t.providerName = p.ProviderName
	}
	if p.ModelName != "" {
		t.modelName = p.ModelName
	}
	if t.mode == uipage.Chat && len(t.chat.Messages) == 0 {
		t.appendNonToolMessage(chatMsg{Role: "system", Content: t.i18n.Tf("status.daemon_connected", p.PID)})
	}
}

func (t *TUI) handleCompactResultNotification(p protocol.CompactResult) {
	t.chat.Compacting = false
	t.appendNonToolMessage(chatMsg{Role: "system", Content: t.renderCompactPanel(p)})
	_ = t.syncInputFocus()
}

func (t *TUI) handleMemoryListNotification(p protocol.MemoryListResult) {
	if len(p.Memories) == 0 {
		t.appendNonToolMessage(chatMsg{Role: "system", Content: t.i18n.T("memory.not_found")})
	} else {
		t.appendNonToolMessage(chatMsg{Role: "panel", Content: t.renderMemoryList(p.Memories)})
	}
}

func (t *TUI) handleSessionRestoreMessageNotification(p sessionRestoreMessageMsg) {
	if p.Content != "" {
		t.appendNonToolMessage(chatMsg{Role: p.Role, Content: p.Content})
	}
}

func (t *TUI) handleSessionRestoreInputNotification(p sessionRestoreInputMsg) {
	if p.Content != "" {
		t.setInputValue(p.Content)
	}
}

func (t *TUI) handleDaemonFullStatusNotification(p protocol.DaemonStatusParams) {
	t.daemonStatus = p
	if t.daemonStatus.Provider != "" {
		t.providerName = t.daemonStatus.Provider
	}
	if t.daemonStatus.Model != "" {
		t.modelName = t.daemonStatus.Model
	}
	if t.providerName != "" && t.modelName != "" {
		t.configState.ActiveModel = t.providerName + "/" + t.modelName
	}
	t.applyContextStats(t.daemonStatus.ContextTokens, t.daemonStatus.ContextWindow)
}

func (t *TUI) handleConfigStateNotification(p protocol.ConfigParams) {
	t.configState = p
	t.config.Error = ""
	if t.configState.Locale != "" {
		t.i18n.SetLocale(LocaleID(t.configState.Locale))
	}
	if t.configState.Theme != "" {
		t.setTheme(t.configState.Theme)
	}
	if t.configState.GuardMode == "" {
		t.configState.GuardMode = "ask"
	}
	if t.config.DeleteConfirm != "" {
		t.config.DeleteConfirm = ""
	}
	if t.configState.ActiveModel != "" {
		if mc, ok := t.activeConfigModel(); ok {
			t.providerName = mc.Provider
			t.modelName = mc.Model
			t.contextWindow = tuiconfig.DefaultContextWindow(mc)
		}
	}
	if t.config.SetupMode && len(t.configState.Models) > 0 {
		t.config.SetupMode = false
		t.config.FormOpen = false
		t.config.Page = "home"
		t.mode = uipage.Welcome
		return
	}
	t.afterConfigFormSaved()
	if t.config.Page == "detail" && t.config.DetailRef != "" {
		// 删除模型后配置通知会先更新列表；若当前详情 ref 已失效，自动回模型列表。
		if _, ok := t.modelByRef(t.config.DetailRef); !ok {
			t.returnToConfigModels()
		}
	}
	if t.mode == uipage.Welcome && len(t.configState.Models) == 0 && !t.hasConfiguredModel() {
		t.mode = uipage.Config
		t.config.FromMode = uipage.Welcome
		t.config.SetupMode = true
		t.openProviderForm("", nil)
	}
}

func (t *TUI) afterConfigFormSaved() {
	if !t.config.FormOpen {
		return
	}
	wasWorkspace := t.config.WorkspaceOpen
	editingRef := t.config.EditingName
	targetRef := ""
	if !wasWorkspace {
		// 保存编辑后 provider/model 可能变化，先按表单里的新 ref 回到详情页。
		targetRef = t.configProviderFormRef()
	}
	t.config.FormOpen = false
	t.config.WorkspaceOpen = false
	t.config.EditingName = ""
	if wasWorkspace {
		t.config.Page = "home"
	} else if editingRef != "" {
		// 新旧 ref 都不存在时，说明目标模型已不可见，退回列表避免“模型未找到”空面板。
		if !t.openConfigDetailIfPresent(targetRef) && !t.openConfigDetailIfPresent(editingRef) {
			t.returnToConfigModels()
		}
	} else {
		t.config.Page = "models"
	}
}

func (t *TUI) handleSkillListNotification(p protocol.SkillListResult) {
	t.chat.SetSkills(p.Skills)
}

func (t *TUI) handleSkillLoadNotification(p protocol.SkillLoadParams) {
	status := strings.TrimSpace(p.Status)
	if status == "loading" {
		t.chat.SetStatusLabel(t.i18n.Tf("status.skill_loading", p.Name), time.Now())
	} else {
		t.chat.ClearStatusLabel()
	}
	t.appendNonToolMessage(chatMsg{Role: "skill", Content: p})
	t.scrollToBottomOnNextSync()
}

func (t *TUI) handleSkillReviewNotification(p protocol.SkillReviewParams) {
	switch strings.TrimSpace(p.Status) {
	case "running":
		t.chat.SetStatusLabel(t.i18n.Tf("status.skill_reviewing", p.Name), time.Now())
	case "done", "error":
		t.chat.ClearStatusLabel()
		t.appendNonToolMessage(chatMsg{Role: "skill_review", Content: p})
		t.scrollToBottomOnNextSync()
	}
}

func (t *TUI) handleAttachmentStatusNotification(p protocol.AttachmentStatusResult) {
	t.attachmentStatus = p
	t.config.Error = ""
}

func (t *TUI) handleRequestErrorNotification(p requestErrorMsg) {
	if p.Scope == notifyCompactError {
		t.chat.Compacting = false
		t.appendNonToolMessage(chatMsg{Role: "error", Content: p.Message})
		_ = t.syncInputFocus()
		return
	}
	t.config.Error = p.Message
}

func (t *TUI) applyContextStats(tokens, window int) {
	if tokens > 0 {
		t.contextTokens = tokens
	}
	if window > 0 {
		t.contextWindow = window
	}
}
