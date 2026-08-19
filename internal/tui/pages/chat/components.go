package chat

import (
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	"github.com/alanchenchen/suna/internal/tui/components/toolview"
)

type ComponentDeps struct {
	Placeholder    string
	TextareaStyles textarea.Styles
	SpinnerStyle   lipgloss.Style
}

// InitComponents 初始化 Chat 页面拥有的 Bubble components；root 只注入样式和文案。
func (m *Model) InitComponents(deps ComponentDeps) {
	m.Viewport = viewport.New()
	m.Viewport.SoftWrap = false
	m.Viewport.MouseWheelEnabled = true
	m.Viewport.MouseWheelDelta = MouseWheelDelta

	ta := textarea.New()
	ta.Prompt = ""
	ta.Placeholder = deps.Placeholder
	ta.DynamicHeight = true
	ta.MaxHeight = InputMaxHeight
	ta.MinHeight = 1
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetStyles(deps.TextareaStyles)
	m.Textarea = ta

	m.Spinner = spinner.New(spinner.WithSpinner(spinner.Dot))
	m.Spinner.Style = deps.SpinnerStyle

	m.Phase = PhaseIdle
	m.InputHistoryIndex = -1
	m.ActiveTools = make(map[string]*toolview.Entry)
	m.ToolStartTimes = make(map[string]time.Time)
	m.CurrentToolBlock = nil
	m.SelectedToolID = ""
	m.SubtaskCursor = 0
	m.SubtaskCursorUserSet = false
	m.SubtaskToolCursor = 0
	m.SubtaskToolCursorUserSet = false
	m.SubtaskToolDetailExpanded = false
	m.SubtaskToolDetailScroll = 0
}

func (m *Model) RestorePendingInput() {
	if m.PendingInput == "" {
		return
	}
	m.Textarea.SetValue(m.PendingInput)
	m.Textarea.CursorEnd()
	m.PendingInput = ""
}

// ResetNativeLists 清理与当前 session 关联的原生列表数据和交互状态。
// list.Model 实例可跨 session 复用，因此必须显式清空筛选、分页和选中项，
// 不能让下一会话继承旧 session 的查询或可操作条目。
func (m *Model) ResetNativeLists() {
	m.Skills = nil
	if m.SkillsList.Initialized() {
		m.SkillsList.Reset()
	}
	m.SkillsOverlayOpen = false
	m.SkillsLoading = false
	m.SkillsError = ""
	m.MCPServers = nil
	if m.MCPList.Initialized() {
		m.MCPList.Reset()
	}
	m.MCPOverlayOpen = false
	m.MCPLoading = false
	m.MCPError = ""
	m.MCPActionServer = ""
	if m.ModelList.Initialized() {
		m.ModelList.Reset()
	}
	m.ModelPickerOpen = false
}

// ResetRuntime 释放当前 Chat 页面与某个 session 绑定的临时展示状态。
// 组件实例会在下次进入 Chat 时重新初始化；这里不触及 daemon 或持久化 session 数据。
func (m *Model) ResetRuntime() {
	if m == nil {
		return
	}
	for i := range m.Messages {
		m.Messages[i] = Msg{}
	}
	m.Messages = nil
	m.TranscriptBlocks = nil
	m.TranscriptYOffset = 0
	m.TranscriptTotalLines = 0
	m.TranscriptWindowStart = 0
	m.TranscriptWindowEnd = 0
	m.TranscriptWindowSignature = transcriptWindowSignature{}
	m.Viewport.SetContentLines(nil)
	m.DisplayDiscard = DisplayDiscardSummary{}
	m.PendingInput = ""
	m.Textarea.SetValue("")
	m.InputHistoryIndex = -1
	m.InputHistoryDraft = ""
	m.InputHistoryActive = false
	m.LastAssistantText = ""
	m.Loading = false
	m.Compacting = false
	m.ResumeAvailable = false
	m.PendingSteering = nil
	m.SteeringSubmissions = nil
	m.SteeringTerminal = nil
	m.Phase = PhaseIdle
	m.PhaseStart = time.Time{}
	m.StatusLabel = ""
	m.StreamStart = time.Time{}
	m.FollowBottom = false
	m.ForceBottom = false
	m.ManualScrollPaused = false
	m.LastAssistantStartLine = 0
	m.LastAssistantLineCount = 0
	m.LastAssistantMsgIndex = 0
	m.ResponseNavAvailable = false
	m.ResponseNavJumped = false
	m.ResponseNavDismissed = false
	m.LastWaitingTool = ""
	m.ActiveInteraction = nil
	m.InteractionQueue = nil
	m.GuardCursor = 0
	m.GuardScroll = 0
	m.CmdSuggestion = ""
	m.CmdSuggestions = nil
	m.CmdSuggestionIdx = 0
	m.ResetNativeLists()
	m.ShowToolDetail = false
	m.ExpandedReasoningID = 0
	m.NextMessageID = 0
	m.ToolDetailScroll = 0
	m.SelectedToolID = ""
	m.SubtaskCursor = 0
	m.SubtaskCursorUserSet = false
	m.SubtaskToolCursor = 0
	m.SubtaskToolCursorUserSet = false
	m.SubtaskToolDetailExpanded = false
	m.SubtaskToolDetailScroll = 0
	for id := range m.ActiveTools {
		delete(m.ActiveTools, id)
	}
	m.ActiveTools = nil
	for id := range m.ToolStartTimes {
		delete(m.ToolStartTimes, id)
	}
	m.ToolStartTimes = nil
	m.CurrentToolBlock = nil
	m.CloseToolBlockWhenIdle = false
	m.Attachments = nil
	m.AttachmentMode = false
	m.AttachmentCursor = 0
	m.AttachmentDelete = false
	m.Memories = nil
	m.MemoryOverlayOpen = false
	m.MemoryLoading = false
	m.MemoryCursor = 0
	m.MemoryScroll = 0
	m.MemoryError = ""
	m.MemoryConfirm = MemoryConfirmNone
	m.MemoryConfirmText = ""
	m.Sessions = nil
	m.SessionsOverlayOpen = false
	m.SessionsLoading = false
	m.SessionCursor = 0
	m.SessionsError = ""
	m.SessionConfirm = SessionConfirmNone
	m.SessionConfirmID = ""
	m.SessionRowKinds = nil
}
