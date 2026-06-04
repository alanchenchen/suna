package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/alanchenchen/suna/internal/protocol"
)

const inputMaxHeight = 6

type phase int

const (
	phaseIdle phase = iota
	phaseFirstLLM
	phaseLLM
	phaseThinking
	phaseTool
	phaseWaitingAfterTool
)

var (
	styleUserLine  = lipgloss.NewStyle().Foreground(ColorUser).Bold(true)
	styleAgentLine = lipgloss.NewStyle().Foreground(ColorAgent).Bold(true)
	styleToolPill  = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(ColorTool).Padding(0, 1).Bold(true)
	styleToolOk    = lipgloss.NewStyle().Foreground(ColorAgent).Bold(true)
	styleToolErr   = lipgloss.NewStyle().Foreground(ColorError).Bold(true)
	styleToolRun   = lipgloss.NewStyle().Foreground(ColorBrand).Bold(true)
	styleToolDim   = lipgloss.NewStyle().Foreground(ColorDim)
	styleToolAdd   = lipgloss.NewStyle().Foreground(ColorAgent).Bold(true)
	styleToolDel   = lipgloss.NewStyle().Foreground(ColorError).Bold(true)
	styleMetaPill  = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(ColorBrand).Padding(0, 1).Bold(true)
	styleGuardOK   = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(ColorAgent).Padding(0, 1).Bold(true)
	styleGuardWarn = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(ColorTool).Padding(0, 1).Bold(true)
	styleGuardErr  = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(ColorError).Padding(0, 1).Bold(true)
	styleFilePath  = lipgloss.NewStyle().Foreground(ColorHL).Bold(true)
	styleSysLine   = lipgloss.NewStyle().Foreground(ColorDim)
	styleErrLine   = lipgloss.NewStyle().Foreground(ColorError).Bold(true)
)

type toolStatus int

const (
	toolRunning toolStatus = iota
	toolDone
	toolError
)

type toolEntry struct {
	id              string
	localID         string
	parentID        string
	name            string
	rawName         string
	intent          string
	params          string
	paramsRaw       map[string]any
	summary         string
	status          toolStatus
	startedAt       time.Time
	endedAt         time.Time
	duration        time.Duration
	result          string
	resultTruncated bool
	resultBytes     int
	metadata        map[string]any
	guard           *guardInfo
}

type guardInfo struct {
	risk       string
	decision   string
	source     string
	reason     string
	suggestion string
}

type toolBlock struct {
	entries map[string]*toolEntry
	order   []string
}

type commandSpec struct {
	cmd     string
	descKey string
}

func (t *TUI) initChatComponents() tea.Cmd {
	t.vp = viewport.New()
	t.vp.SoftWrap = false
	t.vp.MouseWheelEnabled = true
	t.vp.MouseWheelDelta = 3

	ta := textarea.New()
	ta.Prompt = "> "
	ta.Placeholder = t.tr("tui.chat.input_placeholder")
	ta.DynamicHeight = true
	ta.MaxHeight = inputMaxHeight
	ta.MinHeight = 1
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetStyles(textareaStyles())
	t.ta = ta

	t.sp = spinner.New(spinner.WithSpinner(spinner.Dot))
	t.sp.Style = lipgloss.NewStyle().Foreground(ColorBrand)

	t.phase = phaseIdle
	t.activeTools = make(map[string]*toolEntry)
	t.toolStartTimes = make(map[string]time.Time)
	t.currentToolBlock = nil
	t.selectedToolID = ""

	t.syncContent()
	t.layoutChat()
	t.syncContent()

	if t.pendingInput != "" {
		t.ta.SetValue(t.pendingInput)
		t.ta.CursorEnd()
		t.pendingInput = ""
	}

	return t.syncInputFocus()
}

func (t *TUI) syncContent() {
	// 用户没有主动上滚时保持贴底；主动上滚后不打断阅读。
	// 发送新消息这类明确动作会设置 forceBottom，下一次内容同步必须回到底部。
	followBottom := t.forceBottom || t.followBottom || t.vp.AtBottom()
	if t.forceBottom {
		t.forceBottom = false
	}
	var sb strings.Builder
	inSunaBlock := false
	renderSunaHeader := func() {
		if inSunaBlock {
			return
		}
		// reasoning、tool 和最终回答归为同一个 Suna 回合，避免思考/工具块看起来像用户消息的一部分。
		sb.WriteString("\n  " + styleAgentLine.Render("● "+t.tr("tui.chat.suna")) + "\n")
		inSunaBlock = true
	}

	for i := range t.messages {
		msg := &t.messages[i]
		switch msg.role {
		case "user":
			sb.WriteString("\n" + t.renderUserMessage(msg.content, max(20, t.width-8)) + "\n")
			inSunaBlock = false
		case "assistant":
			renderSunaHeader()
			sb.WriteString(t.renderAssistantMessage(msg) + "\n")
		case "reasoning":
			renderSunaHeader()
			sb.WriteString(t.renderReasoningMessage(msg))
		case "tool":
			if v, ok := msg.content.(*toolBlock); ok {
				renderSunaHeader()
				sb.WriteString(t.renderToolBlock(v))
			}
		case "error":
			content, _ := msg.content.(string)
			sb.WriteString("\n" + t.renderErrorMessage(content) + "\n")
			inSunaBlock = false
		case "restore_summary":
			content, _ := msg.content.(string)
			sb.WriteString("\n" + t.renderRestoreSummaryBox(content) + "\n")
			inSunaBlock = false
		case "panel":
			content, _ := msg.content.(string)
			sb.WriteString("\n" + content + "\n")
			inSunaBlock = false
		case "skill":
			if p, ok := msg.content.(protocol.SkillLoadParams); ok {
				sb.WriteString("\n" + t.renderSkillLoadMessage(p) + "\n")
			}
			inSunaBlock = false
		default:
			content, _ := msg.content.(string)
			sb.WriteString("\n" + styleSysLine.Render("  ◆ "+content) + "\n")
			inSunaBlock = false
		}
	}

	if t.pendingAskID != "" && len(t.pendingAskOptions) > 0 {
		for i, opt := range t.pendingAskOptions {
			if i == t.pendingAskCursor {
				sb.WriteString(fmt.Sprintf("  %s %s\n",
					styleToolOk.Render("●"),
					styleAgentLine.Render(opt)))
			} else {
				sb.WriteString(fmt.Sprintf("  %s %s\n",
					styleToolDim.Render("○"),
					styleSysLine.Render(opt)))
			}
		}
		helpKey := "tui.ask.help"
		if !t.pendingAskCustom {
			helpKey = "tui.ask.choice_help"
		}
		sb.WriteString(styleDim.Render("  "+t.tr(helpKey)) + "\n\n")
	}
	if t.modelPickerOpen {
		sb.WriteString(t.renderModelPicker())
	}

	if t.loading && t.phaseStart.After(time.Time{}) && !t.hasVisibleActiveProgress() {
		renderSunaHeader()
		sb.WriteString(t.renderCurrentStatusLine())
	}
	t.vp.SetContent(sb.String())
	if followBottom {
		t.vp.GotoBottom()
		t.followBottom = true
	} else {
		t.followBottom = t.vp.AtBottom()
	}
}

func (t *TUI) renderErrorMessage(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	width := max(24, t.width-8)
	bodyWidth := max(20, width-4)
	wrapped := lipgloss.NewStyle().Width(bodyWidth).Render(content)
	lines := strings.Split(wrapped, "\n")
	for i := range lines {
		if i == 0 {
			lines[i] = styleErrLine.Render("  ✗ " + lines[i])
		} else {
			lines[i] = styleErrLine.Render("    " + lines[i])
		}
	}
	return strings.Join(lines, "\n")
}

func (t *TUI) renderThinkingBox(content string, running bool, startedAt, endedAt time.Time) string {
	width := max(24, min(t.width-8, 62))
	inner := width - 4
	elapsed := reasoningElapsed(running, startedAt, endedAt)
	title := " ◎ " + t.tr("tui.chat.thinking") + " "
	if running {
		title = fmt.Sprintf(" ◎ %s %s %.1fs ", t.tr("tui.chat.thinking"), t.sp.View(), elapsed.Seconds())
	} else if elapsed > 0 {
		title = fmt.Sprintf(" ◎ %s %.1fs ", t.tr("tui.chat.thinking"), elapsed.Seconds())
	}
	display := strings.TrimSpace(content)
	if running && display == "" {
		display = t.tr("status.thinking")
	}
	if !t.showReasoningDetail {
		display = extractLastSentence(display)
		if display == "" {
			display = t.tr("tui.chat.thought_done")
		}
		display += "    [Ctrl+R " + t.tr("tui.key.reasoning_detail") + "]"
	} else {
		if running {
			display = renderStreamingText(strings.TrimSpace(content), inner)
		} else {
			display = RenderMarkdown(strings.TrimSpace(content), inner)
		}
	}
	lines := strings.Split(strings.TrimRight(display, "\n"), "\n")
	if running && !t.showReasoningDetail && len(lines) > 4 {
		lines = append([]string{"..."}, lines[len(lines)-4:]...)
	}
	if t.showReasoningDetail && len(lines) > 15 {
		if running {
			lines = append([]string{"..."}, lines[len(lines)-15:]...)
		} else {
			lines = append(lines[:15], "...")
		}
	}
	var sb strings.Builder
	sb.WriteString("    " + styleDim.Render("┌─"+title+strings.Repeat("─", max(0, width-lipgloss.Width(title)-3))+"┐") + "\n")
	for _, line := range lines {
		for _, wrapped := range wrapLine(line, inner) {
			sb.WriteString("    " + styleDim.Render("│ ") + wrapped + strings.Repeat(" ", max(0, inner-lipgloss.Width(wrapped))) + styleDim.Render(" │") + "\n")
		}
	}
	sb.WriteString("    " + styleDim.Render("└"+strings.Repeat("─", width-2)+"┘") + "\n")
	return sb.String()
}

func reasoningElapsed(running bool, startedAt, endedAt time.Time) time.Duration {
	if startedAt.IsZero() {
		return 0
	}
	if running {
		return time.Since(startedAt).Truncate(100 * time.Millisecond)
	}
	if endedAt.IsZero() || endedAt.Before(startedAt) {
		return 0
	}
	return endedAt.Sub(startedAt).Truncate(100 * time.Millisecond)
}

func (t *TUI) renderSkillLoadMessage(p protocol.SkillLoadParams) string {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = "unknown"
	}
	body := styleMetaPill.Render(t.tr("tui.skill.loaded")) + " " + styleHL.Render(name)
	return indentLines(boxStyle.BorderForeground(ColorBrand).Width(max(36, min(72, t.width-6))).Padding(1, 2).Render(body), "  ")
}

func (t *TUI) hasVisibleActiveProgress() bool {
	if t.hasRunningTools() {
		return true
	}
	for i := len(t.messages) - 1; i >= 0; i-- {
		msg := t.messages[i]
		switch msg.role {
		case "reasoning":
			return msg.streaming
		case "assistant", "user", "error", "system", "restore_summary", "panel":
			return false
		}
	}
	return false
}

func (t *TUI) renderCurrentStatusLine() string {
	label := t.currentStatusLabel()
	if label == "" {
		label = t.tr("status.responding")
	}
	elapsed := 0.0
	if !t.phaseStart.IsZero() {
		elapsed = time.Since(t.phaseStart).Seconds()
	}
	cancel := styleDim.Render(" · Esc " + t.tr("tui.key.cancel"))
	return fmt.Sprintf("    %s %s %s%s\n", t.sp.View(), styleDim.Render(label), styleDim.Render(fmt.Sprintf("%.1fs", elapsed)), cancel)
}

func (t *TUI) currentStatusLabel() string {
	if n := t.runningToolCount(); n > 0 {
		return fmt.Sprintf("%s · %d running", t.tr("status.exec_tool"), n)
	}
	switch t.phase {
	case phaseFirstLLM:
		return t.tr("status.waiting_llm")
	case phaseLLM:
		return t.tr("status.responding")
	case phaseThinking:
		return t.tr("status.thinking")
	case phaseTool:
		return t.tr("status.exec_tool")
	case phaseWaitingAfterTool:
		return t.tr("status.waiting_after_tool")
	default:
		return ""
	}
}

func (t *TUI) hasDraft() bool {
	return strings.TrimSpace(t.ta.Value()) != "" || len(t.attachments) > 0
}

func (t *TUI) discardDraft() {
	t.confirmDiscardDraft = false
	t.ta.Reset()
	t.attachments = nil
	t.attachmentMode = false
	t.attachmentDelete = false
	t.attachmentCursor = 0
	t.cmdSuggestions = nil
	t.cmdSuggestionIdx = 0
	t.layoutChat()
}

func (t *TUI) updateCmdSuggestionState() {
	val := t.ta.Value()
	if strings.HasPrefix(val, "/") && !strings.Contains(strings.TrimPrefix(val, "/"), " ") {
		t.updateCmdSuggestions(val)
		return
	}
	t.cmdSuggestions = nil
	t.cmdSuggestionIdx = 0
}

func (t *TUI) updateGuardConfirm(ks string) (tea.Model, tea.Cmd) {
	switch ks {
	case "ctrl+c":
		t.doQuit()
		return t, tea.Quit
	case "left", "right":
		if t.guardCursor == 0 {
			t.guardCursor = 1
		} else {
			t.guardCursor = 0
		}
		t.syncContent()
		return t, nil
	case "up":
		t.scrollGuardOverlay(-1)
		t.syncContent()
		return t, nil
	case "down":
		t.scrollGuardOverlay(1)
		t.syncContent()
		return t, nil
	case "pgup":
		t.scrollGuardOverlay(-max(1, t.guardOverlayBodyHeight()-1))
		t.syncContent()
		return t, nil
	case "pgdown":
		t.scrollGuardOverlay(max(1, t.guardOverlayBodyHeight()-1))
		t.syncContent()
		return t, nil
	case "esc":
		return t, t.submitGuardDecision("reject")
	case "enter":
		if t.guardCursor == 0 {
			return t, t.submitGuardDecision("approve")
		}
		return t, t.submitGuardDecision("reject")
	}
	return t, nil
}

func (t *TUI) submitGuardDecision(decision string) tea.Cmd {
	if t.pendingGuard == nil {
		return nil
	}
	id := t.pendingGuard.id
	guardToolID := t.pendingGuard.toolCallID
	if decision == "reject" {
		t.markToolRejected(guardToolID)
	}
	t.advanceGuardQueue()
	restartSpinner := false
	if t.pendingGuard == nil {
		t.loading = true
		t.ta.Blur()
		t.phase = phaseTool
		t.phaseStart = time.Now()
		restartSpinner = true
	}
	cmd := t.guardReplyCmd(id, decision)
	if restartSpinner {
		return tea.Batch(cmd, t.sp.Tick)
	}
	return cmd
}

func (t *TUI) enqueueGuardConfirm(g *guardConfirmView) {
	if g == nil {
		return
	}
	if t.pendingGuard != nil {
		t.guardQueue = append(t.guardQueue, g)
		return
	}
	t.pendingGuard = g
	t.guardCursor = 1
	t.guardScroll = 0
	t.loading = false
	t.phase = phaseIdle
	t.phaseStart = time.Time{}
}

func (t *TUI) advanceGuardQueue() {
	if len(t.guardQueue) == 0 {
		t.pendingGuard = nil
		t.guardCursor = 0
		t.guardScroll = 0
		return
	}
	t.pendingGuard = t.guardQueue[0]
	t.guardScroll = 0
	copy(t.guardQueue, t.guardQueue[1:])
	t.guardQueue[len(t.guardQueue)-1] = nil
	t.guardQueue = t.guardQueue[:len(t.guardQueue)-1]
	t.guardCursor = 1
	t.loading = false
	t.phase = phaseIdle
	t.phaseStart = time.Time{}
}

func (t *TUI) resetPhase() {
	t.finishStreamingMessages()
	t.loading = false
	t.phase = phaseIdle
	t.phaseStart = time.Time{}
	t.activeTools = make(map[string]*toolEntry)
	t.toolStartTimes = make(map[string]time.Time)
	t.currentToolBlock = nil
	_ = t.syncInputFocus()
}

func (t *TUI) moveSelectedTool(delta int) {
	ids := t.visibleToolIDs()
	if len(ids) == 0 {
		return
	}
	idx := 0
	for i, id := range ids {
		if id == t.selectedToolID {
			idx = i
			break
		}
	}
	idx += delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(ids) {
		idx = len(ids) - 1
	}
	if t.selectedToolID != ids[idx] {
		t.selectedToolID = ids[idx]
		t.toolDetailScroll = 0
	}
}

func (t *TUI) allCommands() []commandSpec {
	return []commandSpec{
		{"/new", "tui.command.new.desc"},
		{"/model", "tui.command.model.desc"},
		{"/memory", "tui.command.memory.desc"},
		{"/skills", "tui.command.skills.desc"},
		{"/compact", "tui.command.compact.desc"},
		{"/config", "tui.command.config.desc"},
		{"/help", "tui.command.help.desc"},
	}
}

func (t *TUI) updateCmdSuggestions(input string) {
	t.cmdSuggestions = nil
	for _, c := range t.allCommands() {
		if strings.HasPrefix(c.cmd, input) && c.cmd != input {
			t.cmdSuggestions = append(t.cmdSuggestions, c)
			if len(t.cmdSuggestions) == 4 {
				break
			}
		}
	}
	if t.cmdSuggestionIdx >= len(t.cmdSuggestions) {
		t.cmdSuggestionIdx = 0
	}
}

func (t *TUI) acceptCommandSuggestion() tea.Cmd {
	if len(t.cmdSuggestions) == 0 || t.cmdSuggestionIdx >= len(t.cmdSuggestions) {
		return nil
	}
	cmdText := t.cmdSuggestions[t.cmdSuggestionIdx].cmd
	t.ta.Reset()
	t.cmdSuggestions = nil
	t.cmdSuggestionIdx = 0
	cmd := t.handleCommand(cmdText)
	t.syncContent()
	return cmd
}

func (t *TUI) scrollToBottomOnNextSync() {
	t.followBottom = true
	t.forceBottom = true
}

func (t *TUI) handleSend() tea.Cmd {
	input := strings.TrimSpace(t.ta.Value())
	attachments := append([]attachmentItem(nil), t.attachments...)
	t.ta.Reset()
	if input == "" && len(attachments) == 0 {
		return t.syncInputFocus()
	}
	t.appendNonToolMessage(chatMsg{role: "user", content: userMessageContent{text: input, attachments: attachments}})
	t.scrollToBottomOnNextSync()
	t.attachments = nil
	t.attachmentMode = false
	t.attachmentDelete = false
	t.attachmentCursor = 0
	t.syncContent()

	if t.pendingAskID != "" {
		askID := t.pendingAskID
		t.pendingAskID = ""
		options := t.pendingAskOptions
		t.pendingAskOptions = nil
		t.pendingAskCustom = true
		answer := input
		if len(options) > 0 {
			if idx, ok := parseOptionIndex(input, len(options)); ok {
				answer = options[idx]
			}
		}
		t.startLLMWait()
		return tea.Batch(t.askReplyCmd(askID, answer), t.sp.Tick)
	}

	if strings.HasPrefix(input, "/") && t.isRegisteredSlashCommand(input) {
		cmd := t.handleCommand(input)
		t.syncContent()
		if cmd != nil {
			return cmd
		}
		return t.syncInputFocus()
	}
	return t.runAgent(input, attachments)
}

func (t *TUI) setInputValue(input string) {
	if t.mode == "chat" && t.ta.Placeholder != "" {
		t.ta.SetValue(input)
		t.ta.CursorEnd()
		t.layoutChat()
		return
	}
	t.pendingInput = input
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
