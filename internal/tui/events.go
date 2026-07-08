package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/alanchenchen/suna/internal/protocol"
	tuievents "github.com/alanchenchen/suna/internal/tui/events"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
)

// Root reducer 使用 events 子包的强类型消息；handler 留在 root，负责分发到 page models。
type notificationMsg = tuievents.NotificationMsg

type agentDeltaMsg = tuievents.AgentDeltaMsg
type agentRunMsg = tuievents.AgentRunMsg
type userMessageMsg = tuievents.UserMessageMsg
type sessionStateMsg = tuievents.SessionStateMsg
type usageMsg = tuievents.UsageMsg
type askUserMsg = tuievents.AskUserMsg
type guardConfirmMsg = tuievents.GuardConfirmMsg
type interactionResolvedMsg = tuievents.InteractionResolvedMsg
type toolStartMsg = tuievents.ToolStartMsg
type toolGuardMsg = tuievents.ToolGuardMsg
type toolEndMsg = tuievents.ToolEndMsg
type daemonStateMsg = tuievents.DaemonStateMsg
type compactResultMsg = tuievents.CompactResultMsg
type memoryListMsg = tuievents.MemoryListMsg
type daemonFullStatusMsg = tuievents.DaemonFullStatusMsg
type configStateMsg = tuievents.ConfigStateMsg
type skillListMsg = tuievents.SkillListMsg
type mcpListMsg = tuievents.MCPListMsg
type skillLoadMsg = tuievents.SkillLoadMsg
type skillReviewMsg = tuievents.SkillReviewMsg
type attachmentStatusMsg = tuievents.AttachmentStatusMsg
type requestErrorMsg = tuievents.RequestErrorMsg

type daemonStatusResultMsg struct{ Params protocol.DaemonStatusParams }
type configResultMsg struct{ Params protocol.ConfigParams }

// attachmentStatusResultMsg 等 resultMsg 只表示 method response，不进入 daemon notification 解码路径。
type attachmentStatusResultMsg struct {
	Params protocol.AttachmentStatusResult
}
type sessionListResultMsg struct{ Params protocol.SessionListResult }
type sessionErrorMsg struct{ Message string }
type sessionSnapshotResultMsg struct{ Params protocol.SessionSnapshot }
type memoryListResultMsg struct{ Params protocol.MemoryListResult }
type skillListResultMsg struct{ Params protocol.SkillListResult }
type mcpListResultMsg struct{ Params protocol.MCPListResult }

const (
	notifyCompactError = tuievents.NotifyCompactError
	notifyConfigError  = tuievents.NotifyConfigError
	notifyMCPError     = tuievents.NotifyMCPError
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
	case agentDeltaMsg:
		t.handleAgentDeltaNotification(m.Params)
	case agentRunMsg:
		t.handleAgentRunNotification(m.Params)
	case userMessageMsg:
		t.handleUserMessageNotification(m.Params)
	case sessionStateMsg:
		t.handleSessionStateNotification(m.Params)
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
	case interactionResolvedMsg:
		t.handleInteractionResolvedNotification(m.Params)
	case daemonStateMsg:
		t.handleDaemonStateNotification(m.Params)
	case compactResultMsg:
		t.handleCompactResultNotification(m.Params)
	case memoryListMsg:
		t.handleMemoryListNotification(m.Params)
	case daemonFullStatusMsg:
		t.handleDaemonFullStatusNotification(m.Params)
	case configStateMsg:
		t.handleConfigStateNotification(m.Params)
	case skillListMsg:
		t.handleSkillListNotification(m.Params)
	case mcpListMsg:
		t.handleMCPListNotification(m.Params)
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

func (t *TUI) handleAgentDeltaNotification(p protocol.AgentDeltaParams) {
	t.chat.Compacting = false
	t.compactAuto = false
	t.chat.ResumeAvailable = false
	t.lastTextStreamAt = time.Now()
	switch p.Kind {
	case protocol.AgentDeltaReasoning:
		t.chat.HandleReasoningStart(t.lastTextStreamAt)
		t.appendStreamMessage("reasoning", p.Content)
	default:
		t.chat.HandleStreamStart(t.lastTextStreamAt)
		t.appendStreamMessage("assistant", p.Content)
	}
}

func (t *TUI) handleUserMessageNotification(p protocol.UserMessageParams) {
	if p.SessionID != "" && t.currentSession.ID != "" && p.SessionID != t.currentSession.ID {
		return
	}
	text, attachments := userMessageContentFromParts(p.Parts)
	if strings.TrimSpace(text) == "" && len(attachments) == 0 {
		return
	}
	t.finishStreamingMessages()
	t.appendNonToolMessage(chatMsg{Role: "user", Content: userMessageContent{Text: text, Attachments: attachments}})
	t.scrollToBottomOnNextSync()
}

func userMessageContentFromParts(parts []protocol.MessagePart) (string, []attachmentItem) {
	var texts []string
	var attachments []attachmentItem
	for _, part := range parts {
		switch part.Type {
		case "text":
			if strings.TrimSpace(part.Text) != "" {
				texts = append(texts, part.Text)
			}
		case "image":
			attachments = append(attachments, attachmentItem{Type: part.Type, SourceKind: part.Source.Kind, Path: part.Source.Path, URL: part.Source.URL, Name: part.Source.Name, MimeType: part.Source.MimeType, Size: part.Source.Size})
		}
	}
	return strings.Join(texts, "\n"), attachments
}

func (t *TUI) handleAgentRunNotification(p protocol.AgentRunParams) {
	if p.State == protocol.AgentRunRunning {
		t.currentRunCanControl = p.CanControl
	}
	switch p.State {
	case protocol.AgentRunRetrying:
		t.chat.SetStatusLabel(t.formatRunRetryStatus(p), time.Now())
	case protocol.AgentRunFailed, protocol.AgentRunCancelled:
		t.currentRunCanControl = false
		t.finishStreamingMessages()
		if p.State == protocol.AgentRunCancelled {
			t.appendNonToolMessage(chatMsg{Role: "error", Content: t.tr("model_error.cancelled")})
		} else {
			t.appendNonToolMessage(chatMsg{Role: "error", Content: t.formatModelError(p)})
		}
		t.chat.ResumeAvailable = p.ResumeAvailable
		t.resetPhase()
	case protocol.AgentRunDone:
		t.currentRunCanControl = false
		t.finishStreamingMessages()
		t.chat.ResumeAvailable = false
		t.resetPhase()
	case protocol.AgentRunRunning:
		if p.Phase == protocol.AgentRunPhaseModel {
			t.startLLMWait()
			t.chat.SetStatusLabel(t.tr("status.waiting_model"), time.Now())
		}
	}
}

func (t *TUI) formatRunRetryStatus(p protocol.AgentRunParams) string {
	if p.Attempt > 0 && p.MaxAttempts > 0 && p.DelayMs > 0 {
		seconds := max(1, int((p.DelayMs+999)/1000))
		return t.i18n.Tf("status.model_retrying", seconds, p.Attempt, p.MaxAttempts)
	}
	return t.tr("status.model_retrying_simple")
}

func (t *TUI) formatModelError(p protocol.AgentRunParams) string {
	summary := t.tr("model_error.unknown")
	detail := strings.TrimSpace(p.Message)
	if p.Error != nil {
		summary = t.modelErrorSummary(*p.Error)
		detail = strings.TrimSpace(p.Error.Message)
	}
	lines := []string{t.i18n.Tf("model_error.failed", summary)}
	if detail != "" && detail != summary && !strings.Contains(summary, detail) {
		lines = append(lines, t.i18n.Tf("model_error.detail", detail))
	}
	if p.ResumeAvailable {
		lines = append(lines, t.tr("model_error.action_retry"))
	} else {
		lines = append(lines, t.tr("model_error.action_check"))
	}
	return strings.Join(lines, "\n")
}

func (t *TUI) modelErrorSummary(err protocol.ModelError) string {
	switch err.Kind {
	case protocol.ModelErrorHTTP:
		if err.StatusCode > 0 {
			label := strings.TrimSpace(firstNonEmpty(err.Code, err.Type, err.Message))
			if label == "" {
				return fmt.Sprintf("HTTP %d", err.StatusCode)
			}
			return fmt.Sprintf("HTTP %d · %s", err.StatusCode, label)
		}
	case protocol.ModelErrorNetwork:
		if strings.TrimSpace(err.Message) != "" {
			return t.i18n.Tf("model_error.network_with_message", err.Message)
		}
		return t.tr("model_error.network")
	case protocol.ModelErrorCancelled:
		return t.tr("model_error.cancelled")
	case protocol.ModelErrorInternal:
		if strings.TrimSpace(err.Message) != "" {
			return err.Message
		}
		return t.tr("model_error.internal")
	}
	return firstNonEmpty(err.Message, t.tr("model_error.unknown"))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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
	contextTokens := p.EstimatedContextTokens
	if contextTokens <= 0 {
		contextTokens = p.ContextTokens
	}
	t.applyContextStats(contextTokens, p.ContextWindow)
}

func (t *TUI) handleAskUserNotification(p protocol.AskUserParams) {
	if p.SessionID != "" && t.currentSession.ID != "" && p.SessionID != t.currentSession.ID {
		return
	}
	if !p.CanReply {
		t.appendNonToolMessage(chatMsg{Role: "system", Content: "❓ " + p.Question + "\n" + t.tr("handoff.waiting_owner")})
		t.resetPhase()
		return
	}
	t.chat.EnqueueAskUser(p)
	t.appendNonToolMessage(chatMsg{Role: "system", Content: "❓ " + p.Question})
	t.resetPhase()
}

func (t *TUI) handleGuardConfirmNotification(p protocol.GuardConfirmParams) {
	if p.SessionID != "" && t.currentSession.ID != "" && p.SessionID != t.currentSession.ID {
		return
	}
	if !p.CanReply {
		t.appendNonToolMessage(chatMsg{Role: "system", Content: t.tr("handoff.waiting_owner")})
		t.resetPhase()
		return
	}
	t.enqueueGuardConfirm(&guardConfirmView{ID: p.ID, ToolCallID: p.ToolCallID, Tool: p.Tool, Params: p.Params, Risk: p.Risk, Reason: p.Reason, Suggestion: p.Suggestion, ReviewCode: p.ReviewCode, ReviewMessage: p.ReviewMessage})
}

func (t *TUI) handleInteractionResolvedNotification(p protocol.InteractionResolvedParams) {
	if p.SessionID != "" && t.currentSession.ID != "" && p.SessionID != t.currentSession.ID {
		return
	}
	if t.chat.RemoveInteraction(p.ID) {
		t.syncContent()
	}
}

func (t *TUI) handleToolStartNotification(p protocol.ToolStartParams) {
	t.finishStreamingMessages()
	t.chat.Compacting = false
	t.compactAuto = false
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
	if p.Running != nil {
		if *p.Running {
			t.finishStreamingMessages()
			t.compactAuto = true
			t.chat.Compacting = true
			t.chat.Loading = true
			t.chat.Phase = phaseFirstLLM
			t.chat.PhaseStart = time.Now()
			t.chat.Textarea.Blur()
			t.scrollToBottomOnNextSync()
			_ = t.syncInputFocus()
			return
		}
		t.chat.Compacting = false
		t.compactAuto = false
		if strings.TrimSpace(p.Error) != "" {
			t.resetPhase()
			t.appendNonToolMessage(chatMsg{Role: "error", Content: p.Error})
		}
		_ = t.syncInputFocus()
		return
	}
	if strings.TrimSpace(p.Error) != "" {
		t.chat.Compacting = false
		t.compactAuto = false
		t.appendNonToolMessage(chatMsg{Role: "error", Content: p.Error})
		_ = t.syncInputFocus()
		return
	}
	t.resetPhase()
	t.applyContextStats(p.AfterTokens, p.ContextWindow)
	t.appendNonToolMessage(chatMsg{Role: "panel", Content: t.renderCompactPanel(p)})
	_ = t.syncInputFocus()
}

func (t *TUI) handleMemoryListNotification(p protocol.MemoryListResult) {
	t.chat.SetMemories(p.Memories)
	if !t.chat.MemoryOverlayOpen {
		if len(p.Memories) == 0 {
			t.appendNonToolMessage(chatMsg{Role: "system", Content: t.i18n.T("memory.not_found")})
		} else {
			t.appendNonToolMessage(chatMsg{Role: "panel", Content: t.renderMemoryList(p.Memories)})
		}
	}
}

func (t *TUI) handleSessionStateNotification(p protocol.SessionStateParams) {
	if p.Session.ID == "" {
		return
	}
	updated := false
	for i := range t.sessions {
		if t.sessions[i].ID == p.Session.ID {
			t.sessions[i] = p.Session
			updated = true
			break
		}
	}
	if !updated {
		t.sessions = append(t.sessions, p.Session)
	}
	t.chat.SetSessions(t.sessions)
	t.pickWelcomeSessions()
	if t.currentSession.ID != "" && p.Session.ID == t.currentSession.ID {
		t.currentSession = p.Session
	}
	if t.mode == uipage.Welcome {
		t.menu.SetItems(t.welcomeMenuItems(), t.width)
	}
}

func (t *TUI) applySessionSnapshot(p protocol.SessionSnapshot) {
	if t.handoffRole == "" {
		t.handoffRole = handoffRoleHost
	}
	t.currentSession = p.Session
	t.currentRunCanControl = p.CurrentRun != nil && p.CurrentRun.CanControl
	t.chat.Messages = nil
	t.chat.DisplayDiscard = chatpage.DisplayDiscardSummary{}
	for _, m := range p.Messages {
		if m.Content != "" {
			t.appendNonToolMessage(chatMsg{Role: m.Role, Content: m.Content})
		}
	}
	if p.ToolSummary != nil {
		if content := t.renderSessionRestoreToolSummary(*p.ToolSummary); content != "" {
			t.appendNonToolMessage(chatMsg{Role: "restore_summary", Content: content})
		}
	}
	if p.Compacted {
		t.appendNonToolMessage(chatMsg{Role: "system", Content: t.tr("session.restore_compacted")})
	}
	if p.CurrentRun != nil && p.CurrentRun.Status != protocol.SessionStatusIdle {
		t.chat.StartLLMWait(time.Now())
		if p.CurrentRun.ReasoningBuffer != "" {
			t.appendStreamMessage("reasoning", p.CurrentRun.ReasoningBuffer)
		}
		if p.CurrentRun.AssistantBuffer != "" {
			t.appendStreamMessage("assistant", p.CurrentRun.AssistantBuffer)
		}
	}
	t.trimDisplayHistoryIfNeeded()
	t.chat.ResumeAvailable = false
	t.scrollToBottomOnNextSync()
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
	if t.pendingConfigNotice != "" {
		t.config.Notice = t.pendingConfigNotice
		t.pendingConfigNotice = ""
	}
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
			t.contextWindow = mc.ContextWindow
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

func (t *TUI) handleMCPListNotification(p protocol.MCPListResult) {
	t.chat.SetMCPServers(p.Servers)
}

func (t *TUI) handleSkillLoadNotification(p protocol.SkillLoadParams) {
	status := strings.TrimSpace(p.Status)
	if status == "loading" {
		t.chat.SetStatusLabel(t.i18n.Tf("status.skill_loading", p.Name), time.Now())
	} else {
		t.chat.ClearStatusLabel()
	}
	if t.updateLastSkillLoadMessage(p) {
		t.scrollToBottomOnNextSync()
		return
	}
	t.appendNonToolMessage(chatMsg{Role: "skill", Content: p})
	t.scrollToBottomOnNextSync()
}

func (t *TUI) updateLastSkillLoadMessage(p protocol.SkillLoadParams) bool {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return false
	}
	for i := len(t.chat.Messages) - 1; i >= 0; i-- {
		msg := &t.chat.Messages[i]
		switch msg.Role {
		case "skill":
			prev, ok := msg.Content.(protocol.SkillLoadParams)
			if !ok || strings.TrimSpace(prev.Name) != name {
				return false
			}
			msg.Content = p
			msg.Render = chatMsg{}.Render
			return true
		case "assistant", "user", "error", "system", "restore_summary", "panel", "skill_review":
			return false
		}
	}
	return false
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
	if p.SessionID != "" && t.currentSession.ID != "" && p.SessionID != t.currentSession.ID {
		return
	}
	t.attachmentStatus = p
	t.config.Error = ""
}

func (t *TUI) handleRequestErrorNotification(p requestErrorMsg) {
	if p.Scope == notifyCompactError {
		t.chat.Compacting = false
		t.compactAuto = false
		t.resetPhase()
		t.appendNonToolMessage(chatMsg{Role: "error", Content: p.Message})
		_ = t.syncInputFocus()
		return
	}
	if p.Scope == notifyMCPError {
		t.chat.SetMCPError(p.Message)
		return
	}
	t.pendingConfigNotice = ""
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
