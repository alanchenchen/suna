package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/alanchenchen/suna/internal/protocol"
	textutil "github.com/alanchenchen/suna/internal/tui/components/text"
	"github.com/alanchenchen/suna/internal/tui/components/toolview"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
)

const chatMaxCommandSuggestions = chatpage.MaxCommandSuggestions

type phase = chatpage.Phase

type manualCompactRequestMsg struct{}

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

	return t.syncInputFocus()
}

func (t *TUI) syncContent() {
	t.chat.SyncTranscript(chatpage.TranscriptDeps{
		Width:         t.width,
		SunaLabel:     t.tr("tui.chat.suna"),
		AskHelp:       t.tr("tui.ask.help"),
		AskChoiceHelp: t.tr("tui.ask.choice_help"),
		RenderSunaHeader: func(label string) string {
			return "\n  " + styleAgentLine.Render("● "+label) + "\n"
		},
		RenderUserMessage:    t.renderUserMessage,
		RenderAssistant:      t.renderAssistantMessage,
		RenderReasoning:      t.renderReasoningMessage,
		RenderToolBlock:      t.renderToolBlock,
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
		RenderModelPicker:  t.renderModelPicker,
		RenderStatusLine:   t.renderCurrentStatusLine,
		HasVisibleProgress: t.hasVisibleActiveProgress,
	})
}

func (t *TUI) currentInputPolicy() chatpage.InputPolicy {
	return chatpage.CurrentInputPolicy(chatpage.InputPolicyState{
		Compacting:       t.chat.Compacting,
		Loading:          t.chat.Loading,
		PendingAskID:     t.chat.PendingAskID,
		PendingAskCustom: t.chat.PendingAskCustom,
		PendingGuard:     t.chat.PendingGuard != nil,
		StatusLabel:      t.currentStatusLabel(),
		SpinnerView:      t.chat.Spinner.View(),
		CompactRunning:   t.compactRunningLabel(),
		RespondingLabel:  t.tr("status.responding"),
	})
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

func (t *TUI) updateChat(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
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
			t.syncContent()
			return t, cmd
		}
		return t, nil

	case manualCompactRequestMsg:
		return t, t.compactCmd()

	case tea.PasteMsg:
		if t.inputLocked() {
			return t, nil
		}
		cmd := t.handlePaste(m.Content)
		t.syncContent()
		return t, cmd

	case tea.MouseMsg:
		if t.mouseInComposer(m) {
			return t, nil
		}
		if t.chat.PendingGuard != nil {
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
		var cmd tea.Cmd
		t.chat.Viewport, cmd = t.chat.Viewport.Update(msg)
		t.chat.FollowBottom = t.chat.Viewport.AtBottom()
		return t, cmd
	}

	if t.chat.ConfirmDiscardDraft {
		t.chat.ConfirmDiscardDraft = false
	}

	var cmd tea.Cmd
	t.chat.Textarea, cmd = t.chat.Textarea.Update(msg)

	t.updateCmdSuggestionState()
	t.layoutChat()

	return t, cmd
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

	if t.chat.PendingAskID != "" {
		askID := t.chat.PendingAskID
		t.chat.PendingAskID = ""
		options := t.chat.PendingAskOptions
		t.chat.PendingAskOptions = nil
		t.chat.PendingAskCustom = true
		answer := input
		if len(options) > 0 {
			if idx, ok := parseOptionIndex(input, len(options)); ok {
				answer = options[idx]
			}
		}
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
	case chatpage.KeyTargetModelPicker:
		return t.updateModelPicker(ks)
	case chatpage.KeyTargetSkills:
		return t.updateSkillsOverlay(ks)
	case chatpage.KeyTargetMCP:
		return t.updateMCPOverlay(ks)
	case chatpage.KeyTargetPendingImagePaste:
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

func (t *TUI) updateChatKeyNormal(ks string, msg tea.Msg) (tea.Model, tea.Cmd) {
	switch {
	case ks == "ctrl+c":
		t.doQuit()
		return t, tea.Quit
	case ks == "?":
		t.showHelp = !t.showHelp
		return t, nil
	case ks == "enter":
		return t.updateChatEnter()
	case ks == "shift+enter" || ks == "ctrl+j":
		t.chat.InsertNewline()
		t.layoutChat()
		return t, nil
	case ks == "esc":
		return t.updateChatEsc()
	case ks == "ctrl+t":
		t.toggleToolDetail()
		return t, nil
	case ks == "ctrl+r":
		t.chat.ToggleReasoningDetail()
		t.syncContent()
		return t, nil
	case ks == "pgup":
		t.scrollChatPage(-1)
		return t, nil
	case ks == "pgdown":
		t.scrollChatPage(1)
		return t, nil
	case ks == "up":
		t.moveChatCursor(-1)
		return t, nil
	case ks == "down":
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
	t.chat.ConfirmDiscardDraft = false
	if len(t.chat.CmdSuggestions) > 0 {
		cmd := t.acceptCommandSuggestion()
		if cmd != nil {
			return t, cmd
		}
		return t, t.syncInputFocus()
	}
	if t.chat.PendingAskID != "" && len(t.chat.PendingAskOptions) > 0 && t.chat.Textarea.Value() == "" {
		idx := t.chat.PendingAskCursor
		if idx >= 0 && idx < len(t.chat.PendingAskOptions) {
			answer := t.chat.PendingAskOptions[idx]
			askID := t.chat.PendingAskID
			t.chat.PendingAskID = ""
			t.chat.PendingAskOptions = nil
			t.chat.PendingAskCustom = true
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
	t.chat.ConfirmDiscardDraft = true
	t.layoutChat()
	return t, t.syncInputFocus()
}

func (t *TUI) toggleToolDetail() {
	t.chat.ToggleToolDetail(t.visibleToolIDs())
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
		t.chat.Viewport.HalfPageUp()
		t.chat.FollowBottom = false
		return
	}
	t.chat.Viewport.HalfPageDown()
	t.chat.FollowBottom = t.chat.Viewport.AtBottom()
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
	if t.chat.PendingAskID != "" && len(t.chat.PendingAskOptions) > 0 {
		t.chat.PendingAskCursor += delta
		if t.chat.PendingAskCursor < 0 {
			t.chat.PendingAskCursor = 0
		}
		if t.chat.PendingAskCursor >= len(t.chat.PendingAskOptions) {
			t.chat.PendingAskCursor = len(t.chat.PendingAskOptions) - 1
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
	if len(parts) == 1 {
		return t.listMemoryCmd()
	}
	t.appendNonToolMessage(chatMsg{Role: "system", Content: t.i18n.T("memory.list_hint")})
	return nil
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
	out := ""
	for _, r := range s {
		if lipgloss.Width(out+string(r)+"…") > maxWidth {
			break
		}
		out += string(r)
	}
	return out + "…"
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
