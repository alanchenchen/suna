package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/tui/clipboard"
	attachmentmodel "github.com/alanchenchen/suna/internal/tui/components/attachment"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
)

const inputCursorBlinkInterval = 530 * time.Millisecond

func (t *TUI) inputCursorBlinkCmd() tea.Cmd {
	return tea.Tick(inputCursorBlinkInterval, func(time.Time) tea.Msg {
		return inputCursorBlinkMsg{}
	})
}

// startInputCursorBlink 启动唯一的闪烁 tick 链；已启动时不重复起链，避免多条 tick 叠加导致翻转过快。
func (t *TUI) startInputCursorBlink() tea.Cmd {
	t.inputCursorVisible = true
	if t.inputCursorBlinking {
		return nil
	}
	t.inputCursorBlinking = true
	return t.inputCursorBlinkCmd()
}

// updateInputCursorBlink 只在 chat 输入态翻转可见性；其他页面保持常亮，但 tick 链永不断，回到 chat 后必然继续闪烁。
func (t *TUI) updateInputCursorBlink() tea.Cmd {
	if t.mode == uipage.Chat && !t.currentInteractionPresentation().Locked {
		t.inputCursorVisible = !t.inputCursorVisible
	} else {
		t.inputCursorVisible = true
	}
	return t.inputCursorBlinkCmd()
}

func (t *TUI) currentInteractionPresentation() chatpage.InteractionPresentation {
	return chatpage.CurrentInteractionPresentation(chatpage.InputPolicyState{
		Compacting:      t.chat.Compacting,
		Loading:         t.chat.Loading,
		ObservingRun:    t.observingRun(),
		InteractionKind: t.chat.ActiveInteractionKind(),
		AskAllowCustom:  activeAskAllowCustom(t.chat.ActiveAsk()),
		StatusLabel:     t.currentInputStatusLabel(),
		SpinnerView:     t.chat.Spinner.View(),
		CompactRunning:  t.compactRunningLabel(),
		RespondingLabel: t.tr("status.responding"),
		ObservingLabel:  t.tr("tui.chat.observe_input"),
	}, t.selectionMode)
}

func (t *TUI) currentInputPolicy() chatpage.InputPolicy {
	return t.currentInteractionPresentation().InputPolicy
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
	if t.chat.SyncInputFocus(t.currentInteractionPresentation().Locked) {
		return t.chat.Textarea.Focus()
	}
	return nil
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
	t.chat.ExitInputHistory()
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
	t.chat.ExitInputHistory()
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
		t.currentRunCanControl = true
		t.startLLMWait()
		return tea.Batch(t.askReplyCmd(askID, answer), t.startChatSpinner())
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
	if ks == "ctrl+c" {
		t.doQuit()
		return t, tea.Quit
	}
	switch t.chat.RouteKey(ks, t.inputLocked(), t.chat.Compacting) {
	case chatpage.KeyTargetDiscardDraft:
		return t.updateDiscardDraftConfirm(ks, msg)
	case chatpage.KeyTargetGuard:
		keyMsg, ok := msg.(tea.KeyPressMsg)
		if !ok {
			return t, nil
		}
		return t.updateGuardConfirm(keyMsg)
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
	case chatpage.KeyTargetSessions:
		return t.updateSessionsOverlay(ks)
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
	case ks == "enter":
		if t.hasActiveSubtaskPanel() {
			t.chat.SubtaskToolDetailExpanded = !t.chat.SubtaskToolDetailExpanded
			t.chat.SubtaskToolDetailScroll = 0
			t.syncContent()
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
			t.syncContent()
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
			t.syncContent()
			return t, nil
		}
		t.scrollChatPage(-1)
		return t, nil
	case ks == "pgdown":
		if t.chat.SubtaskToolDetailExpanded && t.hasActiveSubtaskPanel() {
			t.scrollSubtaskToolDetail(max(1, t.subtaskToolDetailHeight()-1))
			t.syncContent()
			return t, nil
		}
		t.scrollChatPage(1)
		return t, nil
	case ks == "up":
		if t.hasActiveSubtaskPanel() {
			t.moveSubtaskToolCursor(-1)
			t.syncContent()
			return t, nil
		}
		if t.canBrowseInputHistory() && t.chat.BrowseInputHistory(-1) {
			t.layoutChat()
			return t, nil
		}
		t.moveChatCursor(-1)
		return t, nil
	case ks == "down":
		if t.hasActiveSubtaskPanel() {
			t.moveSubtaskToolCursor(1)
			t.syncContent()
			return t, nil
		}
		if t.canBrowseInputHistory() && t.chat.BrowseInputHistory(1) {
			t.layoutChat()
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
			t.currentRunCanControl = true
			t.startLLMWait()
			t.syncContent()
			return t, tea.Batch(t.askReplyCmd(askID, answer), t.startChatSpinner())
		}
	}
	if !t.chat.Loading {
		return t, t.handleSend()
	}
	return t, nil
}

func (t *TUI) leaveCurrentSessionForWelcome() tea.Cmd {
	// Welcome 不保留当前 Chat session 的展示 runtime；daemon 仍保留持久化 session。
	t.chat.ResetRuntime()
	t.resetConversationStats()
	t.transcriptSyncDirty = false
	t.transcriptSyncScheduled = false
	t.chatSpinnerTicking = false
	t.lastTextStreamAt = time.Time{}
	t.lastPasteAt = time.Time{}
	t.mode = uipage.Welcome
	t.currentSession = protocol.SessionInfo{}
	t.currentRunCanControl = false
	t.handoffRole = handoffRoleHost
	t.welcomeActivePicker = false
	t.welcomeIdlePicker = false
	t.welcomeDeleteConfirm = false
	t.welcomeDeleteID = ""
	t.selectionMode = false
	t.attachmentStatus = protocol.AttachmentStatusResult{}
	t.updateSessionShortcuts()
	return tea.Batch(t.detachSessionCmd(), t.refreshDaemonStatusCmd())
}

func (t *TUI) updateChatEsc() (tea.Model, tea.Cmd) {
	if t.chat.SubtaskToolDetailExpanded && t.hasActiveSubtaskPanel() {
		t.chat.SubtaskToolDetailExpanded = false
		t.chat.SubtaskToolDetailScroll = 0
		t.syncContent()
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
		if !t.currentRunCanControl {
			return t, t.leaveCurrentSessionForWelcome()
		}
		t.currentRunCanControl = false
		t.resetPhase()
		t.appendNonToolMessage(chatMsg{Role: "system", Content: t.i18n.T("status.cancelled")})
		t.syncContent()
		return t, tea.Batch(t.cancelCmd(), t.syncInputFocus())
	}
	if !t.hasDraft() {
		return t, t.leaveCurrentSessionForWelcome()
	}
	// Esc 的帮助文案是“清空”，因此这里直接清空草稿；不再弹确认，避免输入区确认态闪烁。
	t.discardDraft()
	return t, t.syncInputFocus()
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

func (t *TUI) canBrowseInputHistory() bool {
	return !t.chat.ShowToolDetail && len(t.chat.CmdSuggestions) == 0 && t.chat.ActiveAsk() == nil && !t.chat.AttachmentMode && !t.chat.AttachmentDelete
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
