package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/alanchenchen/suna/internal/logging"
	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/tui/clipboard"
	attachmentmodel "github.com/alanchenchen/suna/internal/tui/components/attachment"
	textutil "github.com/alanchenchen/suna/internal/tui/components/text"
	"github.com/alanchenchen/suna/internal/tui/components/toolview"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
)

const chatMaxCommandSuggestions = chatpage.MaxCommandSuggestions

type phase = chatpage.Phase

type manualCompactRequestMsg struct{}
type transcriptSyncMsg struct{}
type inputCursorBlinkMsg struct{}
type clipboardImagePasteMsg struct {
	StartedAt time.Time
	Pending   pendingImagePaste
	Blocked   bool
	Err       error
}

// transcriptSyncFrameInterval 只限制 TUI 聊天正文的同步频率，不影响 daemon 收流；
// 16ms 约等于 60fps，比 8ms/125fps 更适合终端渲染，能降低 VSCode renderer 压力。
const transcriptSyncFrameInterval = 16 * time.Millisecond

const (
	phaseIdle             = chatpage.PhaseIdle
	phaseFirstLLM         = chatpage.PhaseFirstLLM
	phaseLLM              = chatpage.PhaseLLM
	phaseThinking         = chatpage.PhaseThinking
	phaseTool             = chatpage.PhaseTool
	phaseWaitingAfterTool = chatpage.PhaseWaitingAfterTool
)

var (
	styleUserLine   = lipgloss.NewStyle().Foreground(ColorUser).Bold(true)
	styleAgentLine  = lipgloss.NewStyle().Foreground(ColorAgent).Bold(true)
	styleToolPill   = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(ColorTool).Padding(0, 1).Bold(true)
	styleToolOk     = lipgloss.NewStyle().Foreground(ColorAgent).Bold(true)
	styleToolErr    = lipgloss.NewStyle().Foreground(ColorError).Bold(true)
	styleToolRun    = lipgloss.NewStyle().Foreground(ColorBrand).Bold(true)
	styleToolDim    = lipgloss.NewStyle().Foreground(ColorDim)
	styleToolIntent = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	styleToolAdd    = lipgloss.NewStyle().Foreground(ColorAgent).Bold(true)
	styleToolDel    = lipgloss.NewStyle().Foreground(ColorError).Bold(true)
	styleMetaPill   = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(ColorBrand).Padding(0, 1).Bold(true)
	styleGuardOK    = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(ColorAgent).Padding(0, 1).Bold(true)
	styleGuardWarn  = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(ColorTool).Padding(0, 1).Bold(true)
	styleGuardErr   = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(ColorError).Padding(0, 1).Bold(true)
	styleFilePath   = lipgloss.NewStyle().Foreground(ColorHL).Bold(true)
	styleSysLine    = lipgloss.NewStyle().Foreground(ColorDim)
	styleErrLine    = lipgloss.NewStyle().Foreground(ColorError).Bold(true)
)

type toolStatus = toolview.Status

const (
	toolRunning = toolview.StatusRunning
	toolDone    = toolview.StatusDone
	toolError   = toolview.StatusError
)

type toolEntry = toolview.Entry
type guardInfo = toolview.GuardInfo
type toolBlock = toolview.Block

type commandSpec = chatpage.CommandSpec

const (
	// displayMemoryLimitBytes 限制 TUI 自身可控的聊天展示数据；超过后按 turn 从顶部释放到低水位。
	displayMemoryLimitBytes = 32 * 1024 * 1024
)

func (t *TUI) initChatComponents() tea.Cmd {
	t.chat.InitComponents(chatpage.ComponentDeps{
		Placeholder:    t.tr("tui.chat.input_placeholder"),
		TextareaStyles: textareaStyles(),
		SpinnerStyle:   lipgloss.NewStyle().Foreground(ColorBrand),
	})

	t.syncContent()
	t.layoutChat()
	t.syncContent()
	t.chat.RestorePendingInput()

	t.inputCursorVisible = true
	return tea.Batch(t.syncInputFocus(), t.inputCursorBlinkCmd())
}

func (t *TUI) syncContent() {
	started := time.Now()
	t.transcriptSyncDirty = false
	t.chat.SyncTranscript(chatpage.TranscriptDeps{
		Width:         t.width,
		MarkdownWidth: max(24, t.width-8),
		Theme:         currentTheme.Name,
		ReasoningMode: reasoningRenderMode(t.chat.ShowReasoningDetail),
		SunaLabel:     t.tr("tui.chat.suna"),
		AskHelp:       t.tr("tui.ask.help"),
		AskChoiceHelp: t.tr("tui.ask.choice_help"),
		RenderSunaHeader: func(label string) string {
			return "\n  " + styleAgentLine.Render("● "+label) + "\n"
		},
		RenderDisplayDiscard: t.renderDisplayDiscardSummary,
		RenderUserMessage:    t.renderUserMessage,
		RenderAssistant:      t.renderAssistantMessage,
		RenderReasoning:      t.renderReasoningMessage,
		RenderToolBlock:      t.renderToolBlock,
		RenderSubtaskBlock:   t.renderSubtaskBlock,
		RenderError:          t.renderErrorMessage,
		RenderRestoreSummary: t.renderRestoreSummaryBox,
		RenderSkillLoad:      t.renderSkillLoadMessage,
		RenderSkillReview:    t.renderSkillReviewMessage,
		RenderSystem: func(content string) string {
			return t.renderSystemMessage(content)
		},
		RenderAskSelected: func(opt string) string {
			return fmt.Sprintf("  %s %s\n", styleToolOk.Render("●"), styleAgentLine.Render(opt))
		},
		RenderAskOption: func(opt string) string {
			return fmt.Sprintf("  %s %s\n", styleToolDim.Render("○"), styleSysLine.Render(opt))
		},
		RenderAskHelp: func(help string) string {
			return styleDim.Render("  "+help) + "\n\n"
		},
		RenderModelPicker:       t.renderModelPicker,
		RenderStatusLine:        t.renderCurrentStatusLine,
		RenderCompactStatusLine: t.renderCompactStatusLine,
		HasVisibleProgress:      t.hasVisibleActiveProgress,
	})
	if elapsed := time.Since(started); elapsed >= chatpage.SlowPerfLogThreshold() {
		logging.Info("perf", "slow_tui_sync_content", logging.Event{"component": "tui", "duration_ms": elapsed.Milliseconds(), "messages": len(t.chat.Messages), "total_lines": t.chat.TranscriptTotalLines, "streaming": t.chat.HasStreamingMessage()})
	}
}

func (t *TUI) scheduleTranscriptSync() tea.Cmd {
	t.transcriptSyncDirty = true
	if t.transcriptSyncScheduled {
		return nil
	}
	t.transcriptSyncScheduled = true
	return tea.Tick(transcriptSyncFrameInterval, func(time.Time) tea.Msg {
		return transcriptSyncMsg{}
	})
}

func (t *TUI) flushScheduledTranscriptSync() {
	t.transcriptSyncScheduled = false
	if !t.transcriptSyncDirty || t.mode != uipage.Chat {
		return
	}
	t.trimDisplayHistoryIfNeeded()
	t.syncContent()
}

func (t *TUI) trimDisplayHistoryIfNeeded() bool {
	return t.chat.TrimDisplayHistory(displayMemoryLimitBytes)
}

const inputCursorBlinkInterval = 530 * time.Millisecond

func (t *TUI) inputCursorBlinkCmd() tea.Cmd {
	return tea.Tick(inputCursorBlinkInterval, func(time.Time) tea.Msg {
		return inputCursorBlinkMsg{}
	})
}

func (t *TUI) updateInputCursorBlink() tea.Cmd {
	t.inputCursorVisible = !t.inputCursorVisible
	return t.inputCursorBlinkCmd()
}

func (t *TUI) currentInputPolicy() chatpage.InputPolicy {
	return chatpage.CurrentInputPolicy(chatpage.InputPolicyState{
		Compacting:      t.chat.Compacting,
		Loading:         t.chat.Loading,
		InteractionKind: t.chat.ActiveInteractionKind(),
		AskAllowCustom:  activeAskAllowCustom(t.chat.ActiveAsk()),
		StatusLabel:     t.currentInputStatusLabel(),
		SpinnerView:     t.chat.Spinner.View(),
		CompactRunning:  t.compactRunningLabel(),
		RespondingLabel: t.tr("status.responding"),
	})
}

func activeAskAllowCustom(ask *chatpage.AskUserView) bool {
	return ask != nil && ask.AllowCustom
}

func (t *TUI) inputLocked() bool {
	return t.currentInputPolicy().Locked
}

func (t *TUI) allowLockedInputKey(ks string) bool {
	return chatpage.AllowLockedInputKey(ks, t.chat.Compacting)
}

func (t *TUI) syncInputFocus() tea.Cmd {
	if t.chat.SyncInputFocus(t.inputLocked()) {
		return t.chat.Textarea.Focus()
	}
	return nil
}

const textStreamSpinnerSuppressWindow = 120 * time.Millisecond

func (t *TUI) updateChat(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case transcriptSyncMsg:
		t.flushScheduledTranscriptSync()
		return t, nil

	case tea.WindowSizeMsg:
		t.width = m.Width
		t.height = m.Height
		t.ready = true
		t.layoutChat()
		t.syncContent()
		return t, nil

	case tea.KeyPressMsg:
		return t.updateChatKey(m.String(), msg)

	case spinner.TickMsg:
		if t.chat.Loading || t.chat.Compacting {
			var cmd tea.Cmd
			t.chat.Spinner, cmd = t.chat.Spinner.Update(msg)
			// 文本流本身会高频刷新 transcript；短时间内不再让 spinner tick 额外触发完整同步。
			if !t.recentTextStreamActive(time.Now()) {
				t.syncContent()
			}
			return t, cmd
		}
		return t, nil

	case manualCompactRequestMsg:
		return t, t.compactCmd()

	case tea.PasteMsg:
		if t.inputLocked() {
			return t, nil
		}
		t.lastPasteAt = time.Now()
		cmd := t.handlePaste(m.Content)
		t.syncContent()
		return t, cmd

	case clipboardImagePasteMsg:
		if t.inputLocked() || t.lastPasteAt.After(m.StartedAt) {
			return t, nil
		}
		if m.Blocked {
			t.appendNonToolMessage(chatMsg{Role: "error", Content: t.tr("tui.attachment.clipboard_image_too_large")})
			t.syncContent()
			return t, nil
		}
		if m.Err != nil || len(m.Pending.Data) == 0 {
			return t, nil
		}
		t.chat.EnqueueImagePaste(m.Pending)
		t.layoutChat()
		t.syncContent()
		return t, nil

	case tea.MouseMsg:
		if t.chat.SubtaskToolDetailExpanded && t.hasActiveSubtaskPanel() {
			if mm, ok := any(m).(tea.MouseWheelMsg); ok {
				switch mm.Mouse().Button {
				case tea.MouseWheelUp:
					t.scrollSubtaskToolDetail(-t.chat.Viewport.MouseWheelDelta)
				case tea.MouseWheelDown:
					t.scrollSubtaskToolDetail(t.chat.Viewport.MouseWheelDelta)
				}
				return t, nil
			}
		}
		if t.mouseInComposer(m) {
			return t, nil
		}
		if t.chat.ActiveInteractionKind() == chatpage.InteractionGuardConfirm {
			if mm, ok := any(m).(tea.MouseWheelMsg); ok {
				if mm.Mouse().Button == tea.MouseWheelUp {
					t.scrollGuardOverlay(-3)
				} else if mm.Mouse().Button == tea.MouseWheelDown {
					t.scrollGuardOverlay(3)
				}
				t.syncContent()
			}
			return t, nil
		}
		if t.chat.ShowToolDetail {
			if mm, ok := any(m).(tea.MouseWheelMsg); ok {
				if mm.Mouse().Button == tea.MouseWheelUp {
					t.scrollToolDetailOverlay(-3)
				} else if mm.Mouse().Button == tea.MouseWheelDown {
					t.scrollToolDetailOverlay(3)
				}
			}
			return t, nil
		}
		if mm, ok := any(m).(tea.MouseWheelMsg); ok {
			mouse := mm.Mouse()
			if !mouse.Mod.Contains(tea.ModShift) {
				delta := 0
				switch mouse.Button {
				case tea.MouseWheelUp:
					delta = -t.chat.Viewport.MouseWheelDelta
				case tea.MouseWheelDown:
					delta = t.chat.Viewport.MouseWheelDelta
				}
				if delta != 0 {
					// Chat viewport 只持有当前 transcript window 的切片，不能先交给 Bubble viewport
					// 处理垂直滚轮；否则会被当前窗口的局部 max offset 截断，无法跨 window 滚动。
					if t.chat.ScrollTranscript(delta) {
						t.syncContent()
					}
					return t, nil
				}
			}
		}
		var cmd tea.Cmd
		oldOffset := t.chat.Viewport.YOffset()
		t.chat.Viewport, cmd = t.chat.Viewport.Update(msg)
		delta := t.chat.Viewport.YOffset() - oldOffset
		if delta != 0 {
			if t.chat.ScrollTranscript(delta) {
				t.syncContent()
			}
		} else {
			t.chat.FollowBottom = t.chat.TranscriptAtBottom()
		}
		return t, cmd
	}

	if t.chat.HasDiscardDraftConfirm() {
		t.chat.CancelDiscardDraft()
	}

	var cmd tea.Cmd
	t.chat.Textarea, cmd = t.chat.Textarea.Update(msg)

	t.updateCmdSuggestionState()
	t.layoutChat()

	return t, cmd
}

func (t *TUI) recentTextStreamActive(now time.Time) bool {
	if t.lastTextStreamAt.IsZero() {
		return false
	}
	if now.Sub(t.lastTextStreamAt) > textStreamSpinnerSuppressWindow {
		return false
	}
	for i := len(t.chat.Messages) - 1; i >= 0; i-- {
		msg := t.chat.Messages[i]
		if msg.Streaming && (msg.Role == "assistant" || msg.Role == "reasoning") {
			return true
		}
		if msg.Role == "assistant" || msg.Role == "reasoning" || msg.Role == "user" || msg.Role == "error" || msg.Role == "system" {
			break
		}
	}
	return false
}

func (t *TUI) updateDiscardDraftConfirm(ks string, msg tea.Msg) (tea.Model, tea.Cmd) {
	switch ks {
	case "ctrl+c":
		t.doQuit()
		return t, tea.Quit
	case "enter":
		t.discardDraft()
		return t, t.syncInputFocus()
	case "esc":
		t.chat.CancelDiscardDraft()
		t.layoutChat()
		return t, t.syncInputFocus()
	}

	t.chat.CancelDiscardDraft()
	var cmd tea.Cmd
	t.chat.Textarea, cmd = t.chat.Textarea.Update(msg)
	t.updateCmdSuggestionState()
	t.layoutChat()
	return t, cmd
}

func (t *TUI) handleSend() tea.Cmd {
	input := strings.TrimSpace(t.chat.Textarea.Value())
	attachments := append([]attachmentItem(nil), t.chat.Attachments...)
	if input == "" && len(attachments) == 0 && t.chat.ResumeAvailable {
		t.chat.Textarea.Reset()
		return t.resumeAgent()
	}
	t.chat.Textarea.Reset()
	if input == "" && len(attachments) == 0 {
		return t.syncInputFocus()
	}
	t.appendNonToolMessage(chatMsg{Role: "user", Content: userMessageContent{Text: input, Attachments: attachments}})
	t.scrollToBottomOnNextSync()
	t.chat.Attachments = nil
	t.chat.AttachmentMode = false
	t.chat.AttachmentDelete = false
	t.chat.AttachmentCursor = 0
	t.syncContent()

	if ask := t.chat.ActiveAsk(); ask != nil {
		askID := ask.ID
		options := append([]string(nil), ask.Options...)
		answer := input
		if len(options) > 0 {
			if idx, ok := parseOptionIndex(input, len(options)); ok {
				answer = options[idx]
			}
		}
		t.chat.CompleteInteraction()
		t.startLLMWait()
		return tea.Batch(t.askReplyCmd(askID, answer), t.chat.Spinner.Tick)
	}

	if strings.HasPrefix(input, "/") && chatpage.IsRegisteredSlashCommand(input) {
		cmd := t.handleCommand(input)
		t.syncContent()
		if cmd != nil {
			return cmd
		}
		return t.syncInputFocus()
	}
	return t.runAgent(input, attachments)
}

func (t *TUI) hasDraft() bool {
	return t.chat.HasDraft()
}
func (t *TUI) discardDraft() {
	t.chat.Textarea.Reset()
	t.chat.CancelDiscardDraft()
	t.chat.ResetDraft()
	t.layoutChat()
}
func (t *TUI) resetPhase() {
	t.finishStreamingMessages()
	t.chat.Compacting = false
	t.compactAuto = false
	t.chat.ResetPhase()
	_ = t.syncInputFocus()
}
func (t *TUI) scrollToBottomOnNextSync() {
	t.chat.FollowBottom = true
	t.chat.ForceBottom = true
}
func (t *TUI) setInputValue(input string) {
	if t.chat.SetInputValue(input, t.mode == uipage.Chat) {
		t.layoutChat()
	}
}
func (t *TUI) resetConversationStats() {
	t.sessionInputTok = 0
	t.sessionOutputTok = 0
	t.sessionCachedTok = 0
	t.lastInputTok = 0
	t.lastOutputTok = 0
	t.lastCachedTok = 0
	t.lastDuration = 0
	t.lastTokensPerSec = 0
	t.hasUsage = false
	t.contextTokens = 0
	if t.daemonStatus.ContextTokens != 0 {
		t.daemonStatus.ContextTokens = 0
	}
}

func (t *TUI) updateCmdSuggestionState() {
	val := t.chat.Textarea.Value()
	if strings.HasPrefix(val, "/") && !strings.Contains(strings.TrimPrefix(val, "/"), " ") {
		t.updateCmdSuggestions(val)
		return
	}
	t.chat.ClearCommandSuggestions()
}
func (t *TUI) updateCmdSuggestions(input string) {
	t.chat.UpdateCommandSuggestions(input, chatMaxCommandSuggestions)
}
func (t *TUI) acceptCommandSuggestion() tea.Cmd {
	suggestion, ok := t.chat.AcceptCommandSuggestion()
	if !ok {
		return nil
	}
	cmd := t.handleCommand(suggestion.Cmd)
	t.syncContent()
	return cmd
}

func (t *TUI) updateChatKey(ks string, msg tea.Msg) (tea.Model, tea.Cmd) {
	switch t.chat.RouteKey(ks, t.inputLocked(), t.chat.Compacting) {
	case chatpage.KeyTargetDiscardDraft:
		return t.updateDiscardDraftConfirm(ks, msg)
	case chatpage.KeyTargetGuard:
		return t.updateGuardConfirm(ks)
	case chatpage.KeyTargetAskUser:
		return t.updateAskUser(ks, msg)
	case chatpage.KeyTargetModelPicker:
		return t.updateModelPicker(ks)
	case chatpage.KeyTargetSkills:
		return t.updateSkillsOverlay(ks)
	case chatpage.KeyTargetMCP:
		return t.updateMCPOverlay(ks)
	case chatpage.KeyTargetMemory:
		return t.updateMemoryOverlay(ks)
	case chatpage.KeyTargetImagePasteConfirm:
		cmd := t.updatePendingImagePaste(ks)
		t.syncContent()
		return t, cmd
	case chatpage.KeyTargetAttachment:
		if t.updateAttachmentMode(ks) {
			t.syncContent()
			return t, nil
		}
	case chatpage.KeyTargetBlocked:
		return t, nil
	}
	return t.updateChatKeyNormal(ks, msg)
}

func (t *TUI) readClipboardImagePasteCmd(startedAt time.Time) tea.Cmd {
	return func() tea.Msg {
		// 有些终端可能先发 Ctrl+V key，再发 bracketed paste；短暂等待让 PasteMsg 优先落地。
		time.Sleep(40 * time.Millisecond)
		data, err := clipboard.ReadImage()
		if err != nil {
			return clipboardImagePasteMsg{StartedAt: startedAt, Err: err}
		}
		pending, ok, blocked := attachmentmodel.NewImageDataPaste("clipboard_image", "clipboard-image", "", data)
		if !ok {
			return clipboardImagePasteMsg{StartedAt: startedAt, Blocked: blocked}
		}
		return clipboardImagePasteMsg{StartedAt: startedAt, Pending: pending, Blocked: blocked}
	}
}

func (t *TUI) updateChatKeyNormal(ks string, msg tea.Msg) (tea.Model, tea.Cmd) {
	switch {
	case ks == "ctrl+c":
		t.doQuit()
		return t, tea.Quit
	case ks == "?":
		t.showHelp = !t.showHelp
		return t, nil
	case ks == "enter":
		if t.hasActiveSubtaskPanel() {
			t.chat.SubtaskToolDetailExpanded = !t.chat.SubtaskToolDetailExpanded
			t.chat.SubtaskToolDetailScroll = 0
			return t, nil
		}
		t.chat.ClearResponseNav()
		return t.updateChatEnter()
	case ks == "shift+enter" || ks == "ctrl+j":
		t.chat.InsertNewline()
		t.layoutChat()
		return t, nil
	case ks == "esc":
		return t.updateChatEsc()
	case ks == "ctrl+v":
		return t, t.readClipboardImagePasteCmd(time.Now())
	case ks == "ctrl+t":
		t.toggleToolDetail()
		return t, nil
	case ks == "tab":
		if t.hasActiveSubtaskPanel() {
			t.moveSubtaskCursor(1)
			return t, nil
		}
	case ks == "ctrl+r":
		t.chat.ToggleReasoningDetail()
		t.syncContent()
		return t, nil
	case ks == "ctrl+up":
		t.jumpToLastAssistantStart()
		return t, nil
	case ks == "ctrl+down":
		t.jumpToBottom()
		return t, nil
	case ks == "pgup":
		if t.chat.SubtaskToolDetailExpanded && t.hasActiveSubtaskPanel() {
			t.scrollSubtaskToolDetail(-max(1, t.subtaskToolDetailHeight()-1))
			return t, nil
		}
		t.scrollChatPage(-1)
		return t, nil
	case ks == "pgdown":
		if t.chat.SubtaskToolDetailExpanded && t.hasActiveSubtaskPanel() {
			t.scrollSubtaskToolDetail(max(1, t.subtaskToolDetailHeight()-1))
			return t, nil
		}
		t.scrollChatPage(1)
		return t, nil
	case ks == "up":
		if t.hasActiveSubtaskPanel() {
			t.moveSubtaskToolCursor(-1)
			return t, nil
		}
		t.moveChatCursor(-1)
		return t, nil
	case ks == "down":
		if t.hasActiveSubtaskPanel() {
			t.moveSubtaskToolCursor(1)
			return t, nil
		}
		t.moveChatCursor(1)
		return t, nil
	}

	var cmd tea.Cmd
	t.chat.Textarea, cmd = t.chat.Textarea.Update(msg)
	t.updateCmdSuggestionState()
	t.layoutChat()
	return t, cmd
}

func (t *TUI) updateChatEnter() (tea.Model, tea.Cmd) {
	t.chat.CancelDiscardDraft()
	if len(t.chat.CmdSuggestions) > 0 {
		cmd := t.acceptCommandSuggestion()
		if cmd != nil {
			return t, cmd
		}
		return t, t.syncInputFocus()
	}
	if ask := t.chat.ActiveAsk(); ask != nil && len(ask.Options) > 0 && t.chat.Textarea.Value() == "" {
		idx := ask.Cursor
		if idx >= 0 && idx < len(ask.Options) {
			answer := ask.Options[idx]
			askID := ask.ID
			t.chat.CompleteInteraction()
			t.appendNonToolMessage(chatMsg{Role: "user", Content: answer})
			t.scrollToBottomOnNextSync()
			t.startLLMWait()
			t.syncContent()
			return t, tea.Batch(t.askReplyCmd(askID, answer), t.chat.Spinner.Tick)
		}
	}
	if !t.chat.Loading {
		return t, t.handleSend()
	}
	return t, nil
}

func (t *TUI) updateChatEsc() (tea.Model, tea.Cmd) {
	if t.chat.SubtaskToolDetailExpanded && t.hasActiveSubtaskPanel() {
		t.chat.SubtaskToolDetailExpanded = false
		t.chat.SubtaskToolDetailScroll = 0
		return t, nil
	}
	if t.chat.ShowToolDetail {
		t.chat.ShowToolDetail = false
		return t, nil
	}
	if t.showHelp {
		t.showHelp = false
		return t, nil
	}
	if t.chat.Loading {
		t.resetPhase()
		t.appendNonToolMessage(chatMsg{Role: "system", Content: t.i18n.T("status.cancelled")})
		t.syncContent()
		return t, tea.Batch(t.cancelCmd(), t.syncInputFocus())
	}
	if !t.hasDraft() {
		t.mode = uipage.Welcome
		return t, t.refreshDaemonStatusCmd()
	}
	// Esc 的帮助文案是“清空”，因此这里直接清空草稿；不再弹确认，避免输入区确认态闪烁。
	t.discardDraft()
	return t, t.syncInputFocus()
}

func (t *TUI) toggleToolDetail() {
	t.chat.ToggleToolDetail(t.visibleToolIDs())
}

func (t *TUI) hasActiveSubtaskPanel() bool {
	return len(t.visibleSubtaskIDs()) > 0
}

func (t *TUI) selectedSubtaskID() string {
	ids := t.visibleSubtaskIDs()
	if len(ids) == 0 {
		return ""
	}
	t.clampSubtaskCursor()
	return ids[t.chat.SubtaskCursor]
}

func (t *TUI) selectedSubtask() *toolEntry {
	return t.findTool(t.selectedSubtaskID())
}

func (t *TUI) selectedSubtaskTools() []*toolEntry {
	parent := t.selectedSubtask()
	if parent == nil || t.chat.CurrentToolBlock == nil {
		return nil
	}
	return toolview.SubtaskChildren(t.chat.CurrentToolBlock, parent.ID)
}

func (t *TUI) selectedSubtaskTool() *toolEntry {
	children := t.selectedSubtaskTools()
	if len(children) == 0 {
		return nil
	}
	if !t.chat.SubtaskToolCursorUserSet {
		t.chat.SubtaskToolCursor = t.defaultSubtaskToolCursor()
	}
	t.clampSubtaskToolCursor()
	return children[t.chat.SubtaskToolCursor]
}

func (t *TUI) moveSubtaskCursor(delta int) {
	ids := t.visibleSubtaskIDs()
	if len(ids) == 0 {
		t.chat.SubtaskCursor = 0
		t.chat.SubtaskCursorUserSet = false
		t.chat.SubtaskToolCursor = 0
		t.chat.SubtaskToolCursorUserSet = false
		t.chat.SubtaskToolDetailScroll = 0
		return
	}
	t.chat.SubtaskCursor += delta
	t.chat.SubtaskCursorUserSet = true
	if t.chat.SubtaskCursor < 0 {
		t.chat.SubtaskCursor = len(ids) - 1
	}
	if t.chat.SubtaskCursor >= len(ids) {
		t.chat.SubtaskCursor = 0
	}
	t.chat.SubtaskToolCursor = t.defaultSubtaskToolCursor()
	t.chat.SubtaskToolCursorUserSet = false
	t.chat.SubtaskToolDetailScroll = 0
}

func (t *TUI) moveSubtaskToolCursor(delta int) {
	children := t.selectedSubtaskTools()
	if len(children) == 0 {
		t.chat.SubtaskToolCursor = 0
		t.chat.SubtaskToolCursorUserSet = false
		t.chat.SubtaskToolDetailScroll = 0
		return
	}
	t.chat.SubtaskToolCursor += delta
	t.chat.SubtaskToolCursorUserSet = true
	if t.chat.SubtaskToolCursor < 0 {
		t.chat.SubtaskToolCursor = 0
	}
	if t.chat.SubtaskToolCursor >= len(children) {
		t.chat.SubtaskToolCursor = len(children) - 1
	}
	t.chat.SubtaskToolDetailScroll = 0
}

func (t *TUI) clampSubtaskCursor() {
	ids := t.visibleSubtaskIDs()
	if len(ids) == 0 {
		t.chat.SubtaskCursor = 0
		return
	}
	if t.chat.SubtaskCursor < 0 {
		t.chat.SubtaskCursor = 0
	}
	if t.chat.SubtaskCursor >= len(ids) {
		t.chat.SubtaskCursor = len(ids) - 1
	}
}

func (t *TUI) clampSubtaskToolCursor() {
	children := t.selectedSubtaskTools()
	if len(children) == 0 {
		t.chat.SubtaskToolCursor = 0
		return
	}
	if t.chat.SubtaskToolCursor < 0 {
		t.chat.SubtaskToolCursor = 0
	}
	if t.chat.SubtaskToolCursor >= len(children) {
		t.chat.SubtaskToolCursor = len(children) - 1
	}
}

func (t *TUI) ensureSubtaskSelection() {
	if !t.chat.SubtaskCursorUserSet {
		t.chat.SubtaskCursor = t.defaultSubtaskCursor()
	}
	t.clampSubtaskCursor()
	if !t.chat.SubtaskToolCursorUserSet {
		t.chat.SubtaskToolCursor = t.defaultSubtaskToolCursor()
	}
	t.clampSubtaskToolCursor()
}

func (t *TUI) defaultSubtaskCursor() int {
	ids := t.visibleSubtaskIDs()
	if len(ids) == 0 {
		return 0
	}
	for i, id := range ids {
		if te := t.findTool(id); te != nil && te.Status == toolview.StatusRunning {
			return i
		}
	}
	return 0
}

func (t *TUI) defaultSubtaskToolCursor() int {
	children := t.selectedSubtaskTools()
	if len(children) == 0 {
		return 0
	}
	lastDone := 0
	for i, child := range children {
		if child.Status == toolview.StatusRunning {
			return i
		}
		if child.Status == toolview.StatusDone || child.Status == toolview.StatusError {
			lastDone = i
		}
	}
	return lastDone
}

func (t *TUI) scrollSubtaskToolDetail(delta int) {
	te := t.selectedSubtaskTool()
	if te == nil {
		t.chat.SubtaskToolDetailScroll = 0
		return
	}
	deps := t.toolDetailDeps()
	source := toolview.DetailLineSource(te, deps)
	maxOffset := max(0, source.Len()-t.subtaskToolDetailHeight())
	t.chat.SubtaskToolDetailScroll += delta
	if t.chat.SubtaskToolDetailScroll < 0 {
		t.chat.SubtaskToolDetailScroll = 0
	}
	if t.chat.SubtaskToolDetailScroll > maxOffset {
		t.chat.SubtaskToolDetailScroll = maxOffset
	}
}

func (t *TUI) jumpToLastAssistantStart() {
	if t.chat.JumpToLastAssistantStart() {
		t.syncContent()
		t.layoutChat()
	}
}

func (t *TUI) jumpToBottom() {
	t.chat.JumpToBottom()
	t.syncContent()
	t.layoutChat()
}

func (t *TUI) scrollChatPage(direction int) {
	if t.chat.ShowToolDetail {
		delta := max(1, t.toolDetailPageStep())
		if direction < 0 {
			delta = -delta
		}
		t.scrollToolDetailOverlay(delta)
		return
	}
	if direction < 0 {
		if t.chat.PageTranscript(-1) {
			t.syncContent()
		}
		t.chat.FollowBottom = false
		return
	}
	if t.chat.PageTranscript(1) {
		t.syncContent()
	}
	t.chat.FollowBottom = t.chat.TranscriptAtBottom()
}

func (t *TUI) moveChatCursor(delta int) {
	if t.chat.ShowToolDetail {
		t.moveSelectedTool(delta)
		return
	}
	if len(t.chat.CmdSuggestions) > 0 {
		t.chat.CmdSuggestionIdx += delta
		if t.chat.CmdSuggestionIdx < 0 {
			t.chat.CmdSuggestionIdx = 0
		}
		if t.chat.CmdSuggestionIdx >= len(t.chat.CmdSuggestions) {
			t.chat.CmdSuggestionIdx = len(t.chat.CmdSuggestions) - 1
		}
		return
	}
	if ask := t.chat.ActiveAsk(); ask != nil && len(ask.Options) > 0 {
		ask.Cursor += delta
		if ask.Cursor < 0 {
			ask.Cursor = 0
		}
		if ask.Cursor >= len(ask.Options) {
			ask.Cursor = len(ask.Options) - 1
		}
		t.syncContent()
		return
	}
	if t.updateAttachmentMode(mapDirectionKey(delta)) {
		t.syncContent()
	}
}

func mapDirectionKey(delta int) string {
	if delta < 0 {
		return "up"
	}
	return "down"
}

func (t *TUI) handleCommand(input string) tea.Cmd {
	if t.localCli == nil {
		t.appendNonToolMessage(chatMsg{Role: "error", Content: t.i18n.T("error.not_connected")})
		t.scrollToBottomOnNextSync()
		return nil
	}

	parts := strings.Fields(input)
	if len(parts) == 0 {
		return nil
	}
	cmd := parts[0]
	t.scrollToBottomOnNextSync()

	switch cmd {
	case "/new":
		t.chat.Messages = []chatMsg{}
		t.chat.DisplayDiscard = chatpage.DisplayDiscardSummary{}
		t.chat.Attachments = nil
		t.chat.ResumeAvailable = false
		t.resetConversationStats()
		t.resetPhase()
		t.chat.LastAssistantText = ""
		return t.newSessionCmd()
	case "/model":
		if len(parts) > 1 {
			return t.switchModelRef(parts[1])
		}
		t.openModelPicker()
		t.syncContent()
		return nil
	case "/memory":
		return t.handleMemory(parts)
	case "/compact":
		t.compactAuto = false
		t.chat.Compacting = true
		t.chat.Loading = true
		t.chat.Phase = phaseFirstLLM
		t.chat.PhaseStart = time.Now()
		t.chat.Textarea.Blur()
		t.syncContent()
		return tea.Batch(deferManualCompactRequestCmd(), t.chat.Spinner.Tick)
	case "/config":
		t.mode = uipage.Config
		t.config.FromMode = uipage.Chat
		t.config.SetupMode = false
		t.config.FormOpen = false
		t.config.Page = "home"
		return nil
	case "/skills":
		return t.handleSkills(parts)
	case "/mcp":
		return t.handleMCP(parts)
	case "/help":
		t.prevMode = uipage.Chat
		t.mode = uipage.Help
		t.initHelpPage()
		return nil
	default:
		t.appendNonToolMessage(chatMsg{Role: "error", Content: t.i18n.Tf("cmd.unknown", cmd)})
	}
	return nil
}

func (t *TUI) switchModelRef(ref string) tea.Cmd {
	if !strings.Contains(ref, "/") && t.providerName != "" {
		ref = t.providerName + "/" + ref
	}
	if _, ok := t.modelByRef(ref); !ok {
		t.appendNonToolMessage(chatMsg{Role: "error", Content: t.i18n.Tf("cmd.model_not_found", ref)})
		return nil
	}
	t.setActiveModelRef(ref)
	t.chat.ModelPickerOpen = false
	t.appendNonToolMessage(chatMsg{Role: "system", Content: t.i18n.Tf("cmd.model_switched", ref)})
	return t.sendConfigSet(protocol.ConfigSetParams{Action: protocol.ConfigActionActivateModel, ActiveModel: ref})
}

func (t *TUI) openModelPicker() {
	t.chat.OpenModelPicker(chatpage.ModelRefs(t.configModelsSnapshot()), t.configState.ActiveModel)
}

func (t *TUI) updateModelPicker(key string) (tea.Model, tea.Cmd) {
	models := t.configModelsSnapshot()
	refs := chatpage.ModelRefs(models)
	if len(refs) == 0 {
		t.chat.CloseModelPicker()
		return t, nil
	}
	switch key {
	case "esc":
		t.chat.CloseModelPicker()
	case "up":
		t.chat.MoveModelPicker(-1, len(refs))
	case "down":
		t.chat.MoveModelPicker(1, len(refs))
	case "enter":
		if ref, ok := t.chat.SelectedModelRef(refs); ok {
			return t, t.switchModelRef(ref)
		}
	}
	t.syncContent()
	return t, nil
}

func (t *TUI) handleMemory(parts []string) tea.Cmd {
	if len(parts) != 1 {
		t.appendNonToolMessage(chatMsg{Role: "system", Content: t.i18n.T("memory.list_hint")})
		return nil
	}
	t.chat.OpenMemoryOverlay()
	return t.listMemoryCmd()
}

func (t *TUI) handleSkills(parts []string) tea.Cmd {
	if len(parts) != 1 {
		t.appendNonToolMessage(chatMsg{Role: "system", Content: t.tr("tui.skills.usage")})
		return nil
	}
	t.chat.OpenSkillsOverlay()
	return t.listSkillsCmd()
}

func (t *TUI) updateSkillsOverlay(ks string) (tea.Model, tea.Cmd) {
	switch ks {
	case "esc":
		t.chat.CloseSkillsOverlay()
		return t, t.syncInputFocus()
	case "up":
		t.chat.MoveSkillsCursor(-1)
		return t, nil
	case "down":
		t.chat.MoveSkillsCursor(1)
		return t, nil
	case "enter", " ", "space":
		if action, ok := t.chat.SelectSkill(t.tr("tui.skills.cannot_toggle")); ok {
			return t, t.setSkillOverlayCmd(action.Name, action.Enabled)
		}
		return t, nil
	}
	return t, nil
}

func (t *TUI) setSkillOverlayCmd(name string, enabled bool) tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification(notifyConfigError, errNotConnected(t))
		}
		if err := t.localCli.SetSkill(protocol.SkillSetParams{Name: strings.TrimSpace(name), Enabled: enabled}); err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		if err := t.localCli.ListSkills(); err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		return nil
	}
}

func (t *TUI) updateMemoryOverlay(ks string) (tea.Model, tea.Cmd) {
	if t.chat.MemoryConfirm != chatpage.MemoryConfirmNone {
		switch ks {
		case "esc":
			t.chat.CancelMemoryConfirm()
			return t, nil
		case "enter":
			if t.chat.MemoryConfirm == chatpage.MemoryConfirmDelete {
				if action, ok := t.chat.ConfirmMemoryDelete(); ok {
					return t, t.deleteMemoryOverlayCmd(action.ID)
				}
				return t, nil
			}
			if t.chat.ConfirmMemoryClear() {
				return t, t.clearMemoryOverlayCmd()
			}
			return t, nil
		default:
			t.chat.UpdateMemoryConfirmText(ks)
			return t, nil
		}
	}
	switch ks {
	case "esc":
		t.chat.CloseMemoryOverlay()
		return t, t.syncInputFocus()
	case "up":
		t.chat.MoveMemoryCursor(-1)
		return t, nil
	case "down":
		t.chat.MoveMemoryCursor(1)
		return t, nil
	case "delete", "backspace", "ctrl+h":
		if t.chat.BeginMemoryDelete() {
			return t, nil
		}
		return t, nil
	case "enter":
		if t.chat.MemorySelectionIsClear() {
			t.chat.BeginMemoryClear()
		}
		return t, nil
	}
	return t, nil
}

func (t *TUI) deleteMemoryOverlayCmd(id string) tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification(notifyConfigError, errNotConnected(t))
		}
		if err := t.localCli.DeleteMemory(id); err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		return nil
	}
}

func (t *TUI) clearMemoryOverlayCmd() tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification(notifyConfigError, errNotConnected(t))
		}
		if err := t.localCli.ClearMemory(); err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		return nil
	}
}

func (t *TUI) handleMCP(parts []string) tea.Cmd {
	if len(parts) != 1 {
		t.appendNonToolMessage(chatMsg{Role: "system", Content: t.tr("tui.mcp.usage")})
		return nil
	}
	t.chat.OpenMCPOverlay()
	return t.listMCPCmd()
}

func (t *TUI) updateMCPOverlay(ks string) (tea.Model, tea.Cmd) {
	switch ks {
	case "esc":
		t.chat.CloseMCPOverlay()
		return t, t.syncInputFocus()
	case "up":
		t.chat.MoveMCPCursor(-1)
		return t, nil
	case "down":
		t.chat.MoveMCPCursor(1)
		return t, nil
	case " ", "space":
		if action, ok := t.chat.SelectMCPForToggle(); ok {
			t.chat.SetMCPActionServer(action.Name)
			return t, t.setMCPOverlayCmd(action.Name, action.Active)
		}
		return t, nil
	case "enter":
		if name, ok := t.chat.SelectMCPForReload(); ok {
			t.chat.SetMCPActionServer(name)
			return t, t.reloadMCPOverlayCmd(name)
		}
		return t, nil
	}
	return t, nil
}

func (t *TUI) setMCPOverlayCmd(name string, active bool) tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification(notifyMCPError, errNotConnected(t))
		}
		if err := t.localCli.ToggleMCP(protocol.MCPSetParams{Name: strings.TrimSpace(name), Active: active}); err != nil {
			return ipcErrorNotification(notifyMCPError, err)
		}
		if err := t.localCli.ListMCP(); err != nil {
			return ipcErrorNotification(notifyMCPError, err)
		}
		return nil
	}
}

func (t *TUI) reloadMCPOverlayCmd(name string) tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification(notifyMCPError, errNotConnected(t))
		}
		if err := t.localCli.ReloadMCP(protocol.MCPReloadParams{Name: strings.TrimSpace(name)}); err != nil {
			return ipcErrorNotification(notifyMCPError, err)
		}
		if err := t.localCli.ListMCP(); err != nil {
			return ipcErrorNotification(notifyMCPError, err)
		}
		return nil
	}
}

func (t *TUI) renderMCPOverlay(width int) string {
	view := t.chat.MCPOverlayView(width, t.overlayMaxHeight())
	var body []string
	if view.Loading {
		body = append(body, styleDim.Render(t.tr("tui.mcp.loading")))
	} else if view.Empty {
		body = append(body, styleDim.Render(t.tr("tui.mcp.empty")))
	} else {
		for _, row := range view.Rows {
			body = append(body, t.renderMCPRowView(row, view.Inner))
		}
	}
	body, start, total := scrollWindow(body, view.Height, &t.chat.MCPScroll)
	title := t.tr("tui.mcp.title", view.Active, view.Total, view.Tools, view.Issues)
	lines := []string{styleHL.Render(title), ""}
	lines = append(lines, body...)
	if view.Error != "" {
		lines = append(lines, "", styleError.Render(view.Error))
	}
	lines = append(lines, "", styleDim.Render(t.mcpHelpText(start, view.Height, total)))
	return boxStyle.Width(view.Width).Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func (t *TUI) renderMCPRowView(row chatpage.MCPRowView, width int) string {
	cursor := "  "
	nameStyle := lipgloss.NewStyle()
	if row.Selected {
		cursor = styleCursor.Render("▶ ")
		nameStyle = styleHL
	}
	mark := mcpActiveMark(row)
	name := truncateDisplay(row.Server.Name, max(12, width/3))
	transport := strings.TrimSpace(row.Server.Transport)
	if transport == "" {
		transport = "stdio"
	}
	status := fmt.Sprintf("%s · %s %d", transport, t.tr("tui.mcp.tools"), row.Server.ToolCount)
	if row.Loading {
		status = t.tr("tui.mcp.reloading")
	} else if row.Issue {
		status = t.tr("tui.mcp.error")
	} else if !row.Active {
		status = t.tr("tui.mcp.inactive")
	}
	line := fmt.Sprintf("%s%s %-22s %s", cursor, mark, nameStyle.Render(name), mcpStatusStyle(row).Render(truncateDisplay(status, max(10, width-30))))
	cmd := strings.TrimSpace(row.Server.Command)
	if cmd != "" {
		line += "  " + styleToolDim.Render(truncateDisplay(cmd, max(8, width-lipgloss.Width(line)-2)))
	}
	if row.Issue && row.Server.Error != "" {
		line += "  " + styleToolErr.Render(truncateDisplay(row.Server.Error, max(8, width-lipgloss.Width(line)-2)))
	}
	return line
}

func (t *TUI) mcpHelpText(start, height, total int) string {
	text := t.tr("tui.mcp.help")
	if total > height {
		text += fmt.Sprintf(" · %d-%d/%d", start+1, min(total, start+height), total)
	}
	return text
}

func mcpActiveMark(row chatpage.MCPRowView) string {
	if row.Loading {
		return styleToolRun.Render("◌")
	}
	if row.Issue {
		return styleToolErr.Render("!")
	}
	if row.Active {
		return styleToolOk.Render("●")
	}
	return styleDim.Render("○")
}

func mcpStatusStyle(row chatpage.MCPRowView) lipgloss.Style {
	if row.Loading {
		return styleToolRun
	}
	if row.Issue {
		return styleToolErr
	}
	if row.Active {
		return styleToolOk
	}
	return styleDim
}

func (t *TUI) renderSkillsOverlay(width int) string {
	view := t.chat.SkillsOverlayView(width, t.overlayMaxHeight())
	var body []string
	if view.Loading {
		body = append(body, styleDim.Render(t.tr("tui.skills.loading")))
	} else if view.Empty {
		body = append(body, styleDim.Render(t.tr("tui.skills.empty")))
	} else {
		for _, row := range view.Rows {
			body = append(body, t.renderSkillRowView(row, view.Inner))
		}
	}
	body, start, total := scrollWindow(body, view.Height, &t.chat.SkillsScroll)
	title := t.tr("tui.skills.title", view.Active, view.Total, view.Issues)
	lines := []string{styleHL.Render(title), ""}
	lines = append(lines, body...)
	if view.Error != "" {
		lines = append(lines, "", styleError.Render(view.Error))
	}
	lines = append(lines, "", styleDim.Render(t.skillsHelpText(start, view.Height, total)))
	return boxStyle.Width(view.Width).Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func (t *TUI) renderSkillRowView(row chatpage.SkillRowView, width int) string {
	cursor := "  "
	nameStyle := lipgloss.NewStyle()
	if row.Selected {
		cursor = styleCursor.Render("▶ ")
		nameStyle = styleHL
	}
	mark := skillActiveMark(row.Active)
	status := t.tr("tui.skills.inactive")
	statusStyle := styleDim
	if row.Active {
		status = t.tr("tui.skills.active")
		statusStyle = styleToolOk
	}
	name := truncateDisplay(row.Skill.Name, max(12, width-24))
	line := fmt.Sprintf("%s%s %-24s %-10s", cursor, mark, nameStyle.Render(name), statusStyle.Render(status))
	if row.Issue {
		line += "  " + styleTool.Render(skillIssueText(t, row.Skill))
	}
	return line
}

func (t *TUI) renderMemoryOverlay(width int) string {
	view := t.chat.MemoryOverlayView(width, t.overlayMaxHeight())
	if view.Confirm != chatpage.MemoryConfirmNone {
		return t.renderMemoryConfirmOverlay(view)
	}
	var body []string
	body = append(body, styleDim.Render(t.tr("tui.memory.description")), "")
	if view.Loading {
		body = append(body, styleDim.Render(t.tr("tui.memory.loading")))
	} else {
		for _, row := range view.Rows {
			body = append(body, t.renderMemoryRowView(row, view.Inner)...)
		}
	}
	body, start, total := scrollWindow(body, view.Height, &t.chat.MemoryScroll)
	title := t.tr("tui.memory.title", view.Total)
	lines := []string{styleHL.Render(title), ""}
	lines = append(lines, body...)
	if view.Error != "" {
		lines = append(lines, "", styleError.Render(view.Error))
	}
	lines = append(lines, "", styleDim.Render(t.memoryHelpText(start, view.Height, total)))
	return boxStyle.Width(view.Width).Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func (t *TUI) renderMemoryRowView(row chatpage.MemoryRowView, width int) []string {
	cursor := "  "
	contentStyle := styleToolDim
	if row.Selected {
		cursor = styleCursor.Render("▶ ")
		contentStyle = styleHL
	}
	if row.Kind == chatpage.MemoryRowClear {
		return []string{"", cursor + styleError.Render(t.tr("tui.memory.clear_item"))}
	}
	badge := row.Memory.Kind
	if row.Memory.IsCore {
		badge = "core " + badge
	}
	content := strings.TrimSpace(row.Memory.Content)
	if content == "" {
		content = "-"
	}
	wrapped := textutil.WrapLine(content, max(12, width-12))
	if len(wrapped) == 0 {
		wrapped = []string{""}
	}
	lines := []string{fmt.Sprintf("%s%s %s", cursor, styleTool.Render("["+badge+"]"), contentStyle.Render(wrapped[0]))}
	for _, line := range wrapped[1:] {
		lines = append(lines, "    "+contentStyle.Render(line))
	}
	return lines
}

func (t *TUI) renderMemoryConfirmOverlay(view chatpage.MemoryOverlayView) string {
	var lines []string
	switch view.Confirm {
	case chatpage.MemoryConfirmDelete:
		lines = append(lines, styleHL.Render(t.tr("tui.memory.delete_confirm_title")), "")
		if t.chat.MemoryCursor >= 0 && t.chat.MemoryCursor < len(t.chat.Memories) {
			lines = append(lines, styleToolDim.Render(t.chat.Memories[t.chat.MemoryCursor].Content))
		}
		lines = append(lines, "", styleDim.Render(t.tr("tui.memory.delete_confirm_help")))
	case chatpage.MemoryConfirmClear:
		lines = append(lines, styleHL.Render(t.tr("tui.memory.clear_confirm_title")), "")
		lines = append(lines, styleDim.Render(t.tr("tui.memory.clear_confirm_body", view.Total)), "")
		lines = append(lines, t.tr("tui.memory.clear_confirm_input", view.ConfirmText))
		lines = append(lines, "", styleDim.Render(t.tr("tui.memory.clear_confirm_help")))
	}
	return boxStyle.Width(view.Width).Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func (t *TUI) memoryHelpText(start, height, total int) string {
	text := t.tr("tui.memory.help")
	if total > height {
		text += fmt.Sprintf(" · %d-%d/%d", start+1, min(total, start+height), total)
	}
	return text
}

func (t *TUI) renderSkillRow(i int, s protocol.SkillInfo, width int) string {
	return t.renderSkillRowView(chatpage.SkillRowView{Skill: s, Selected: i == t.chat.SkillsCursor, Active: chatpage.SkillIsActive(s), Issue: chatpage.SkillHasIssue(s)}, width)
}

func (t *TUI) skillsHelpText(start, height, total int) string {
	text := t.tr("tui.skills.help")
	if total > height {
		text += fmt.Sprintf(" · %d-%d/%d", start+1, min(total, start+height), total)
	}
	return text
}

func skillIssueText(t *TUI, s protocol.SkillInfo) string {
	if strings.TrimSpace(s.Error) != "" {
		return t.tr("tui.skills.issue_error")
	}
	if len(s.Reasons) > 0 {
		return t.tr("tui.skills.issue_reasons", len(s.Reasons))
	}
	return t.tr("tui.skills.issue_review")
}

func skillActiveMark(active bool) string {
	if active {
		return styleToolOk.Render("●")
	}
	return styleDim.Render("○")
}

func truncateDisplay(s string, maxWidth int) string {
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	if maxWidth <= 1 {
		return "…"
	}
	var out strings.Builder
	width := 0
	ellipsisWidth := lipgloss.Width("…")
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if width+rw+ellipsisWidth > maxWidth {
			break
		}
		out.WriteRune(r)
		width += rw
	}
	return out.String() + "…"
}

func clampSkillCursor(cursor, n int) int {
	if n <= 0 {
		return 0
	}
	if cursor < 0 {
		return 0
	}
	if cursor >= n {
		return n - 1
	}
	return cursor
}

func (t *TUI) renderMemoryList(memories []protocol.MemoryItem) string {
	width := max(36, min(t.width-6, 92))
	inner := max(24, width-8)
	var lines []string
	lines = append(lines, styleHL.Render(t.tr("tui.memory.active_title")))
	for _, m := range memories {
		lines = append(lines, renderMemoryItem(m, inner)...)
	}
	return boxStyle.Width(width).Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func renderMemoryItem(m protocol.MemoryItem, width int) []string {
	badge := fmt.Sprintf("%s:%d", m.Kind, m.Priority)
	if m.IsCore {
		badge = "core " + badge
	}
	head := styleTool.Render("[" + badge + "]")
	content := strings.TrimSpace(m.Content)
	if content == "" {
		content = "-"
	}
	wrapped := textutil.WrapLine(content, max(12, width-4))
	if len(wrapped) == 0 {
		wrapped = []string{""}
	}
	lines := []string{"  " + styleDim.Render("• ") + head}
	for _, line := range wrapped {
		lines = append(lines, "    "+styleToolDim.Render(line))
	}
	return lines
}

func lipglossWidthPlain(s string) int {
	return lipgloss.Width(s)
}
