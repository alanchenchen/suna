package tui

import (
	"fmt"
	"sort"
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
type steeringMsg = tuievents.SteeringMsg
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
type mcpUpdatedMsg = tuievents.MCPUpdatedMsg
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

// newSessionResultMsg 保留新会话快照；旧会话删除失败时仍必须切换到已创建的新会话。
type newSessionResultMsg struct {
	Params    protocol.SessionSnapshot
	DeleteErr error
}
type sessionMetadataResultMsg struct{ Session protocol.SessionInfo }
type sessionTitleUpdateResultMsg struct {
	SessionID       string
	OldTitle        string
	OptimisticTitle string
	Session         protocol.SessionInfo
	Err             error
}
type memoryListResultMsg struct{ Params protocol.MemoryListResult }
type skillListResultMsg struct{ Params protocol.SkillListResult }
type mcpListResultMsg struct{ Params protocol.MCPListResult }
type steerResultMsg struct {
	Message     protocol.SteeringMessage
	ClientMsgID string
	Err         error
}
type steerRemoveResultMsg struct {
	Message protocol.SteeringMessage
	Err     error
}
type cancelResultMsg struct {
	Err      error
	Rejected bool
}

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
	// Welcome 不消费与 Chat runtime 绑定的事件。detach 前已经进入本地队列的
	// stream/tool/interaction 通知不能重新持有刚释放的展示内容。
	if isChatRuntimeNotification(msg) && t.mode == uipage.Welcome && t.currentSession.ID == "" {
		return
	}
	switch m := msg.(type) {
	case agentDeltaMsg:
		t.handleAgentDeltaNotification(m.Params)
	case agentRunMsg:
		t.handleAgentRunNotification(m.Params)
	case steeringMsg:
		t.handleSteeringNotification(m.Params)
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
	case mcpUpdatedMsg:
		t.handleMCPUpdatedNotification(m.Params)
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

func isChatRuntimeNotification(msg notificationMsg) bool {
	switch m := msg.(type) {
	case agentDeltaMsg, agentRunMsg, steeringMsg, userMessageMsg, usageMsg, toolStartMsg, toolGuardMsg, toolEndMsg, askUserMsg, guardConfirmMsg, interactionResolvedMsg, compactResultMsg, memoryListMsg, skillLoadMsg, skillReviewMsg:
		return true
	case attachmentStatusMsg:
		return m.Params.SessionID != ""
	case requestErrorMsg:
		return m.Scope == notifyCompactError
	default:
		return false
	}
}

func (t *TUI) handleSteeringNotification(p protocol.SteeringMessage) {
	if p.RunID == "" || p.RunID == t.completedRunID || (t.activeRunID != "" && p.RunID != t.activeRunID) {
		return
	}
	t.removeSteeringSubmission(p.ClientMsgID)
	idx := t.pendingSteeringIndex(p.ID)
	switch p.State {
	case protocol.SteeringQueued:
		if _, terminal := t.chat.SteeringTerminal[p.ID]; terminal {
			break
		}
		if idx >= 0 {
			t.chat.PendingSteering[idx] = p
		} else {
			t.chat.PendingSteering = append(t.chat.PendingSteering, p)
		}
		sort.SliceStable(t.chat.PendingSteering, func(i, j int) bool {
			return t.chat.PendingSteering[i].Sequence < t.chat.PendingSteering[j].Sequence
		})
	case protocol.SteeringApplied, protocol.SteeringRemoved, protocol.SteeringRejected:
		if idx >= 0 {
			t.chat.PendingSteering = append(t.chat.PendingSteering[:idx], t.chat.PendingSteering[idx+1:]...)
		}
		if t.chat.SteeringTerminal == nil {
			t.chat.SteeringTerminal = make(map[string]protocol.SteeringState)
		}
		_, seen := t.chat.SteeringTerminal[p.ID]
		t.chat.SteeringTerminal[p.ID] = p.State
		if !seen && p.CanControl && (p.State == protocol.SteeringRemoved || p.State == protocol.SteeringRejected) {
			if text := steeringMessageText(p); text != "" {
				t.chat.Textarea.SetValue(restoreSteeringDraft(t.chat.Textarea.Value(), text))
				t.chat.Textarea.CursorEnd()
			}
		}
	}
	t.layoutChat()
	t.syncContent()
}

func (t *TUI) resolveSteeringSubmission(clientMsgID string, failed bool) (resolved bool, restore []string) {
	if clientMsgID == "" {
		return false, nil
	}
	for i := range t.chat.SteeringSubmissions {
		if t.chat.SteeringSubmissions[i].ClientMsgID != clientMsgID {
			continue
		}
		t.chat.SteeringSubmissions[i].Resolved = true
		t.chat.SteeringSubmissions[i].Failed = failed
		resolved = true
		break
	}
	if !resolved {
		return false, nil
	}
	for _, item := range t.chat.SteeringSubmissions {
		if !item.Resolved {
			return true, nil
		}
	}
	for _, item := range t.chat.SteeringSubmissions {
		if item.Failed {
			restore = append(restore, item.Text)
		}
	}
	t.chat.SteeringSubmissions = nil
	return true, restore
}

func (t *TUI) restoreUnresolvedSteeringSubmissions() {
	if len(t.chat.SteeringSubmissions) == 0 {
		return
	}
	texts := make([]string, 0, len(t.chat.SteeringSubmissions))
	for _, item := range t.chat.SteeringSubmissions {
		if !item.Resolved {
			texts = append(texts, item.Text)
		}
	}
	// 终态后 RPC 结果可能迟到；先清空本地提交，再按原发送顺序恢复尚未确认的草稿，避免重复恢复。
	t.chat.SteeringSubmissions = nil
	t.restoreSteeringDrafts(texts)
}

func (t *TUI) restoreSteeringDrafts(texts []string) {
	if len(texts) == 0 {
		return
	}
	t.chat.Textarea.SetValue(restoreSteeringDraft(t.chat.Textarea.Value(), strings.Join(texts, "\n")))
	t.chat.Textarea.CursorEnd()
}

func (t *TUI) pendingSteeringIndex(id string) int {
	for i := range t.chat.PendingSteering {
		if t.chat.PendingSteering[i].ID == id {
			return i
		}
	}
	return -1
}

func (t *TUI) removeSteeringSubmission(clientMsgID string) bool {
	resolved, restore := t.resolveSteeringSubmission(clientMsgID, false)
	t.restoreSteeringDrafts(restore)
	return resolved
}

func steeringMessageText(message protocol.SteeringMessage) string {
	var texts []string
	for _, part := range message.Parts {
		if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
			texts = append(texts, strings.TrimSpace(part.Text))
		}
	}
	return strings.Join(texts, "\n")
}

func (t *TUI) handleAgentDeltaNotification(p protocol.AgentDeltaParams) {
	t.chat.Compacting = false
	t.compactAuto = false
	t.compactStartedAt = time.Time{}
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

func (t *TUI) handleUsageNotification(p protocol.UsageParams) {
	t.hasUsage = true
	t.lastInputTok = p.InputTokens
	t.lastOutputTok = p.OutputTokens
	t.lastCachedTok = p.CacheReadTokens
	t.lastTokensPerSec = p.TokensPerSec
	if p.DurationMs > 0 {
		t.lastDuration = time.Duration(p.DurationMs) * time.Millisecond
	} else {
		t.lastDuration = 0
	}
	t.sessionInputTok += p.InputTokens
	t.sessionOutputTok += p.OutputTokens
	t.sessionCachedTok += p.CacheReadTokens
	contextTokens := p.EstimatedContextTokens
	if contextTokens <= 0 {
		contextTokens = p.ContextTokens
	}
	t.applyContextStats(contextTokens, p.ContextWindow)
}

func (t *TUI) activeSessionRun() bool {
	switch t.currentSession.Status {
	case protocol.SessionStatusRunning, protocol.SessionStatusWaiting, protocol.SessionStatusCompacting:
		return true
	default:
		return false
	}
}

// canAcceptRunInteraction 只接收仍有活动 run 的交互。completedRunID 是终态栅栏，
// active session 则允许 Handoff observer 接管 daemon 明确授权的 CanReply 交互。
func (t *TUI) canAcceptRunInteraction() bool {
	return !t.cancelling && t.completedRunID == "" && (t.currentRunCanControl || t.activeSessionRun())
}

func (t *TUI) canResumeRunAfterInteraction() bool {
	return !t.cancelling && t.completedRunID == "" && (t.currentRunCanControl || t.activeSessionRun())
}

func (t *TUI) handleAskUserNotification(p protocol.AskUserParams) {
	if p.SessionID != "" && t.currentSession.ID != "" && p.SessionID != t.currentSession.ID {
		return
	}
	if !t.canAcceptRunInteraction() {
		return
	}
	if p.CanReply {
		// 阻塞交互接管输入时必须先失焦，确保可见状态与按键归属一致。
		t.chat.Textarea.Blur()
	}
	if !p.CanReply {
		t.appendNonToolMessage(chatMsg{Role: "system", Content: "❓ " + p.Question + "\n" + t.tr("handoff.waiting_owner")})
		t.resetPhase()
		return
	}
	t.chat.EnqueueAskUser(p)
	if activeAskAllowCustom(t.chat.ActiveAsk()) {
		_ = t.syncInputFocus()
	}
	t.appendNonToolMessage(chatMsg{Role: "system", Content: "❓ " + p.Question})
	t.resetPhase()
}

func (t *TUI) handleGuardConfirmNotification(p protocol.GuardConfirmParams) {
	if p.SessionID != "" && t.currentSession.ID != "" && p.SessionID != t.currentSession.ID {
		return
	}
	if !t.canAcceptRunInteraction() {
		// cancel/终态之后到达的 Guard 已不属于可继续的 run，不能重新激活或污染本地状态。
		return
	}
	if p.CanReply {
		// Guard 接管输入时必须先失焦，确保可见状态与按键归属一致。
		t.chat.Textarea.Blur()
	}
	if !p.CanReply {
		t.appendNonToolMessage(chatMsg{Role: "system", Content: t.tr("handoff.waiting_owner")})
		t.resetPhase()
		return
	}
	t.enqueueGuardConfirm(&guardConfirmView{ID: p.ID, ToolCallID: p.ToolCallID, Tool: p.Tool, Params: p.Params, ReadOnly: p.ReadOnly, Reason: p.Reason, ReviewCode: p.ReviewCode, ReviewMessage: p.ReviewMessage})
}

func (t *TUI) handleInteractionResolvedNotification(p protocol.InteractionResolvedParams) {
	if p.SessionID != "" && t.currentSession.ID != "" && p.SessionID != t.currentSession.ID {
		return
	}
	if t.chat.RemoveInteraction(p.ID) {
		// 外部 resolve 可能使队列推进到允许自定义输入的 AskUser；焦点必须跟随新的交互呈现恢复。
		_ = t.syncInputFocus()
		t.syncContent()
	}
}

func (t *TUI) handleToolStartNotification(p protocol.ToolStartParams) {
	t.runHadToolCall = true
	t.finishStreamingMessages()
	t.chat.Compacting = false
	t.compactAuto = false
	t.compactStartedAt = time.Time{}
	_ = t.syncInputFocus()
	id := p.ID
	if id == "" {
		id = fmt.Sprintf("%s_%d", p.Tool, time.Now().UnixNano())
	}
	now := time.Now()
	if p.Tool == "skill_load" {
		t.chat.StartSkillLoad(p, id, now)
		return
	}
	t.chat.StartTool(p, id, now)
}

func (t *TUI) handleToolGuardNotification(p protocol.ToolGuardParams) {
	t.chat.ApplyToolGuard(p)
}

func (t *TUI) handleToolEndNotification(p protocol.ToolEndParams) {
	id := p.ID
	if id == "" {
		id = fmt.Sprintf("%s_%d", p.Tool, time.Now().UnixNano())
	}
	now := time.Now()
	if t.chat.EndSkillLoad(p, id, now) {
		return
	}
	t.chat.EndTool(p, id, now)
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
			t.compactStartedAt = time.Now()
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
		t.compactStartedAt = time.Time{}
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
		t.compactStartedAt = time.Time{}
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
	if t.mode == uipage.Chat {
		if t.chat.SessionsOverlayOpen {
			t.setSessionOverlaySessions()
		} else {
			t.chat.SetSessions(t.sessions)
		}
	}
	t.pickWelcomeSessions()
	if p.Session.ID == t.currentSession.ID {
		t.currentSession = p.Session
		t.applyCurrentSessionModelConfig()
		if p.Session.Status == protocol.SessionStatusIdle {
			t.currentRunCanControl = false
			t.clearRunRetryStatus()
			t.chat.ClearRunInteractions()
			// 取消中必须等待 agent_run cancelled 收尾工具和一次性提示，不能被较早的 session idle 快照清空。
			if !t.cancelling {
				t.resetPhase()
			}
		}
	}
	if t.mode == uipage.Welcome {
		t.menu.SetItems(t.welcomeMenuItems(), t.width)
	}
}

func (t *TUI) restoreOptimisticSessionTitle(sessionID, optimisticTitle, oldTitle string) {
	if sessionID == "" {
		return
	}
	for i := range t.sessions {
		if t.sessions[i].ID == sessionID && t.sessions[i].Title == optimisticTitle {
			t.sessions[i].Title = oldTitle
			break
		}
	}
	if t.currentSession.ID == sessionID && t.currentSession.Title == optimisticTitle {
		t.currentSession.Title = oldTitle
	}
	if t.mode == uipage.Chat {
		if t.chat.SessionsOverlayOpen {
			t.setSessionOverlaySessions()
		} else {
			t.chat.SetSessions(t.sessions)
		}
	}
	t.pickWelcomeSessions()
	if t.mode == uipage.Welcome {
		t.menu.SetItems(t.welcomeMenuItems(), t.width)
	}
}

func (t *TUI) applySessionSnapshot(p protocol.SessionSnapshot) bool {
	previousSessionID := t.currentSession.ID
	switched := previousSessionID != p.Session.ID
	if previousSessionID != "" && switched {
		// 在 Chat 内 join 另一会话不会经过 Welcome 的 ResetRuntime；列表数据、筛选词
		// 和选中项必须随 session 切换清空，避免显示或操作前一会话的项目。
		t.chat.ResetNativeLists()
	}
	if t.handoffRole == "" {
		t.handoffRole = handoffRoleHost
	}
	if previousSessionID != p.Session.ID {
		t.clearTranscriptManualScroll()
		t.completedRunID = ""
		t.cancelNoticeRunID = ""
		t.cancelling = false
		t.runStartedAt = time.Time{}
		t.activeRunID = ""
		t.runHadToolCall = false
	}
	t.currentSession = p.Session
	t.applyCurrentSessionModelConfig()
	currentRun := p.CurrentRun
	preserveSteeringState := currentRun != nil && currentRun.RunID != "" && currentRun.RunID == t.activeRunID
	if currentRun != nil && t.completedRunID != "" && currentRun.RunID == t.completedRunID {
		currentRun = nil
	} else if currentRun != nil && currentRun.RunID != "" {
		t.completedRunID = ""
	}
	t.currentRunCanControl = currentRun != nil && currentRun.CanControl
	t.chat.PendingSteering = nil
	if currentRun != nil {
		t.chat.PendingSteering = append([]protocol.SteeringMessage(nil), currentRun.PendingSteering...)
	}
	if !preserveSteeringState {
		t.chat.SteeringSubmissions = nil
		t.chat.SteeringTerminal = nil
	}
	t.cancelling = false
	t.chat.Compacting = false
	t.compactAuto = false
	t.compactStartedAt = time.Time{}
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
	if currentRun != nil && currentRun.Status != protocol.SessionStatusIdle {
		now := time.Now()
		t.startRunElapsed(currentRun.RunID, now)
		if currentRun.State == protocol.AgentRunCancelling {
			t.enterCancelling()
		} else {
			t.chat.StartLLMWait(now)
			if currentRun.Status == protocol.SessionStatusCompacting {
				// attach 只能从当前时刻估算 Compact 耗时；状态仍应按 daemon 快照还原，不能退化成普通模型等待。
				t.chat.Compacting = true
				t.compactAuto = true
				t.compactStartedAt = now
			}
		}
		if currentRun.ReasoningBuffer != "" {
			t.appendStreamMessage("reasoning", currentRun.ReasoningBuffer)
		}
		if currentRun.AssistantBuffer != "" {
			t.appendStreamMessage("assistant", currentRun.AssistantBuffer)
		}
	}
	t.trimDisplayHistoryIfNeeded()
	t.chat.ResumeAvailable = false
	t.forceScrollToBottomOnNextSync()
	return switched
}

func (t *TUI) handleDaemonFullStatusNotification(p protocol.DaemonStatusParams) {
	t.daemonStatus = p
	// Daemon status describes the default model for new sessions. An attached
	// session keeps its own context window even if its model was removed from
	// the latest configuration snapshot.
	if t.currentSession.ModelRef != "" {
		t.applyCurrentSessionModelConfig()
		return
	}
	t.applyContextStats(p.ContextTokens, p.ContextWindow)
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
		t.refreshNativeLists()
	}
	if t.configState.Theme != "" {
		t.setTheme(t.configState.Theme)
	}
	if t.configState.GuardMode == "" {
		t.configState.GuardMode = "smart"
	}
	if t.config.DeleteConfirm != "" {
		t.config.DeleteConfirm = ""
	}
	t.applyCurrentSessionModelConfig()
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

func (t *TUI) handleMCPUpdatedNotification(p protocol.MCPUpdatedParams) {
	t.chat.UpdateMCPServer(p.Server)
}

func (t *TUI) handleSkillLoadNotification(p protocol.SkillLoadParams) {
	status := strings.TrimSpace(p.Status)
	if status == "loading" {
		t.chat.SetStatusLabel(t.i18n.Tf("status.skill_loading", p.Name), time.Now())
	} else {
		t.chat.ClearStatusLabel()
	}
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
