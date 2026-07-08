package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/alanchenchen/suna/internal/tui/components/overlay"
	textutil "github.com/alanchenchen/suna/internal/tui/components/text"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
	tuiconfig "github.com/alanchenchen/suna/internal/tui/pages/config"
)

func (t *TUI) viewChat() string {
	if t.width == 0 {
		return ""
	}
	t.layoutChat()
	petState := t.chatPetState()
	separator := styleDim.Render(strings.Repeat("─", t.width))
	inputSeparator := renderInputSeparator(t.width)
	helpOverlay := ""
	if t.showHelp {
		helpOverlay = t.renderHelpOverlay(t.width)
	}
	toolOverlay := ""
	if t.chat.ShowToolDetail {
		toolOverlay = t.renderToolDetailOverlay(t.width)
	}
	skillsOverlay := ""
	if t.chat.SkillsOverlayOpen {
		skillsOverlay = t.renderSkillsOverlay(t.width)
	}
	mcpOverlay := ""
	if t.chat.MCPOverlayOpen {
		mcpOverlay = t.renderMCPOverlay(t.width)
	}
	memoryOverlay := ""
	if t.chat.MemoryOverlayOpen {
		memoryOverlay = t.renderMemoryOverlay(t.width)
	}
	sessionsOverlay := ""
	if t.chat.SessionsOverlayOpen {
		sessionsOverlay = t.renderSessionsOverlay(t.width)
	}
	guardOverlay := ""
	if t.chat.ActiveInteractionKind() == chatpage.InteractionGuardConfirm {
		guardOverlay = t.renderGuardOverlay(t.width)
	}
	imagePasteOverlay := ""
	if t.chat.ActiveImagePaste() != nil {
		imagePasteOverlay = t.renderPendingImagePasteOverlay(t.width)
	}
	cmdSuggestions := ""
	if len(t.chat.CmdSuggestions) > 0 {
		cmdSuggestions = t.renderCommandSuggestions()
	}
	preInputHint := t.renderPreInputHint()
	view := t.replaceLiveTranscriptPlaceholders(t.chat.View(chatpage.ViewDeps{
		Width:              t.width,
		MiniPet:            renderMiniPet(petState),
		TopMeta:            t.chatTopMeta(),
		Conn:               t.chatConnectionDot(petState),
		Content:            t.chat.Viewport.View(),
		Separator:          separator,
		InputSeparator:     inputSeparator,
		InputArea:          t.renderInputArea(),
		PreInputHint:       preInputHint,
		CommandSuggestions: cmdSuggestions,
		StatusBar:          t.renderChatStatusBar(),
		ToolDetailOverlay:  toolOverlay,
		HelpOverlay:        helpOverlay,
		SkillsOverlay:      skillsOverlay,
		MCPOverlay:         mcpOverlay,
		MemoryOverlay:      memoryOverlay,
		SessionsOverlay:    sessionsOverlay,
		GuardOverlay:       guardOverlay,
		Overlay:            overlay.OverlayBlock,
	}))
	if imagePasteOverlay != "" {
		return t.replaceLiveTranscriptPlaceholders(t.overlayImagePasteAboveInput(view, imagePasteOverlay, cmdSuggestions))
	}
	return view
}

func (t *TUI) layoutChat() {
	preInputHint := t.renderPreInputHint()
	inputArea := t.renderInputArea()
	cmdSuggestions := ""
	if len(t.chat.CmdSuggestions) > 0 {
		cmdSuggestions = t.renderCommandSuggestions()
	}
	layout := chatpage.ComputeLayout(chatpage.LayoutInput{
		Width:              t.width,
		Height:             t.height,
		InputAreaHeight:    chatpage.RenderedLineCount(inputArea),
		SuggestionHeight:   chatpage.RenderedLineCount(cmdSuggestions),
		PreInputHintHeight: chatpage.RenderedLineCount(preInputHint),
	})
	if layout.ViewportHeight == 0 && layout.InputWidth == 0 {
		return
	}
	oldWidth := t.chat.Viewport.Width()
	oldHeight := t.chat.Viewport.Height()
	t.chat.Viewport.SetWidth(t.width)
	t.chat.Viewport.SetHeight(layout.ViewportHeight)
	if oldWidth != t.width || oldHeight != layout.ViewportHeight {
		t.chat.SetTranscriptYOffset(t.chat.TranscriptYOffset)
	}
	t.chat.Textarea.SetWidth(layout.InputWidth)
}

func (t *TUI) chatPetState() petState {
	if !t.chat.Loading {
		return petIdle
	}
	if t.chat.Phase == phaseThinking {
		return petThinking
	}
	return petWorking
}

func (t *TUI) chatConnectionDot(state petState) string {
	badge := t.mcpBadge()
	conn := ""
	if t.localCli == nil || !t.localCli.Connected() {
		conn = styleDim.Render("○")
	} else {
		switch state {
		case petWorking:
			conn = styleToolRun.Render("●")
		case petThinking:
			conn = styleBrand.Render("●")
		default:
			conn = styleAgent.Render("●")
		}
	}
	if badge != "" {
		return badge + styleDim.Render("·") + conn
	}
	return conn
}

func (t *TUI) mcpBadge() string {
	active, _, _ := chatpage.MCPSummaryCounts(t.chat.MCPServers)
	total := len(t.chat.MCPServers)
	if total == 0 {
		return styleDim.Render("MCP 0")
	}
	style := styleToolOk
	if active == 0 {
		style = styleDim
	}
	return style.Render(fmt.Sprintf("MCP %d/%d", active, total))
}

func (t *TUI) chatTopMeta() string {
	provider, model := t.providerName, t.modelName
	if p, m := t.activeProviderModel(); p != "" || m != "" {
		provider, model = p, m
	}
	if provider == "" {
		provider = "-"
	}
	if model == "" {
		model = "-"
	}
	modelRef := provider + "/" + model
	reasoning := ""
	if mc, ok := t.activeConfigModel(); ok {
		reasoning = t.reasoningDisplay(mc)
	}
	if reasoning != "" {
		modelRef += "·" + strings.ReplaceAll(reasoning, " / ", "/")
	}
	available := max(10, t.width/2)
	return styleHL.Render(textutil.TruncateRunes(modelRef, available))
}

func (t *TUI) observingRun() bool {
	return t.currentSession.ID != "" && t.chat.Loading && !t.currentRunCanControl
}

func (t *TUI) renderHandoffBlock() string {
	if t.currentSession.ID == "" {
		return ""
	}
	guest := t.handoffRole == handoffRoleGuest
	otherClients := max(0, t.currentSession.ClientCount-1)
	// 单窗口本会话不显示 Handoff 块；只有有其他窗口接入，或当前窗口是接入会话时才持续提示。
	if !guest && otherClients == 0 {
		return ""
	}
	name := strings.TrimSpace(t.currentSession.Title)
	if name == "" {
		name = filepath.Base(t.currentSession.CWD)
	}
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = "session"
	}
	cwd := strings.TrimSpace(t.currentSession.CWD)
	if cwd == "" {
		cwd = "-"
	}
	width := max(30, t.width-6)
	contentWidth := max(24, width-4)

	primary := t.tr("handoff.shared")
	if guest {
		primary = t.tr("handoff.joined")
	}
	name = textutil.TruncateRunes(name, max(8, contentWidth/3))
	cwd = textutil.TruncateRunes(cwd, max(10, contentWidth-lipgloss.Width(primary)-lipgloss.Width(name)-6))
	line1 := styleBrand.Render(primary) + styleDim.Render(" · ") + styleHL.Render(name) + styleDim.Render(" · ") + styleDim.Render(cwd)
	if !guest && otherClients > 0 {
		line1 += styleDim.Render(" · ") + styleDim.Render(t.i18n.Tf("handoff.window_count", otherClients))
	}

	state := t.tr("handoff.idle_continue")
	if t.chat.Loading && t.currentRunCanControl {
		state = t.tr("handoff.your_run")
	} else if t.observingRun() {
		state = t.tr("handoff.observing_run")
	}
	var line2 string
	if guest && otherClients > 0 {
		otherText := t.i18n.Tf("handoff.other_window_count", otherClients)
		stateWidth := max(10, contentWidth-lipgloss.Width(otherText)-3)
		line2 = styleHL.Render(textutil.TruncateRunes(state, stateWidth)) + styleDim.Render(" · ") + styleDim.Render(otherText)
	} else {
		line2 = styleHL.Render(textutil.TruncateRunes(state, contentWidth))
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBrand).
		Padding(0, 1).
		Width(width).
		Render(line1 + "\n" + line2)
	return textutil.IndentLines(box, "  ")
}

func (t *TUI) mouseInComposer(msg tea.MouseMsg) bool {
	m := msg.Mouse()
	cmdSuggestions := ""
	if len(t.chat.CmdSuggestions) > 0 {
		cmdSuggestions = t.renderCommandSuggestions()
	}
	return chatpage.MouseInComposer(chatpage.ComposerHitInput{
		Height:             t.height,
		Y:                  m.Y,
		InputAreaHeight:    chatpage.RenderedLineCount(t.renderInputArea()),
		SuggestionHeight:   chatpage.RenderedLineCount(cmdSuggestions),
		PreInputHintHeight: chatpage.RenderedLineCount(t.renderPreInputHint()),
	})
}

func (t *TUI) overlayImagePasteAboveInput(view, panel, cmdSuggestions string) string {
	if strings.TrimSpace(panel) == "" {
		return view
	}
	lines := strings.Split(view, "\n")
	panelLines := strings.Split(strings.TrimRight(panel, "\n"), "\n")
	if len(lines) == 0 || len(panelLines) == 0 {
		return view
	}
	composerRows := 2 + chatpage.RenderedLineCount(t.renderInputArea()) + chatpage.RenderedLineCount(t.renderPreInputHint()) // 输入分割线 + 输入区 + token 状态栏 + 预输入提示
	if cmdSuggestions != "" {
		composerRows += chatpage.RenderedLineCount(cmdSuggestions)
	}
	composerStart := len(lines) - composerRows
	if composerStart < 0 {
		composerStart = 0
	}
	// 图片粘贴提示应贴近输入区，但不能在小窗口/内容很少时覆盖顶部 pet 和模型状态。
	minStart := min(4, len(lines))
	available := composerStart - minStart
	if available < len(panelLines) {
		return view
	}
	start := composerStart - len(panelLines)
	for i, line := range panelLines {
		idx := start + i
		padded := leftAlignInputOverlayLine(line, t.width)
		if idx >= len(lines) {
			lines = append(lines, padded)
			continue
		}
		lines[idx] = padded
	}
	return strings.Join(lines, "\n")
}

func leftAlignInputOverlayLine(line string, width int) string {
	if width <= 0 {
		return line
	}
	left := 2
	if width <= left {
		left = 0
	}
	available := max(1, width-left)
	if lipgloss.Width(line) > available {
		line = ansi.Truncate(line, available, "…")
	}
	return strings.Repeat(" ", left) + line
}

func (t *TUI) renderChatStatusBar() string {
	ctx := "?"
	if t.contextTokens > 0 {
		ctx = fmtTok(t.contextTokens)
	}
	window := "?"
	pct := 0
	if t.contextWindow > 0 {
		window = fmtTok(t.contextWindow)
		window = strings.ReplaceAll(strings.ReplaceAll(window, ".0k", "k"), ".0M", "M")
		if t.contextTokens > 0 {
			pct = int(float64(t.contextTokens) / float64(t.contextWindow) * 100)
		}
	}
	ctxPct := styleDim.Render(fmt.Sprintf("(%d%%)", pct))
	if t.contextWindow > 0 {
		ctxPct = styleDim.Render("(") + t.contextPercentStyle(pct).Render(fmt.Sprintf("%d%%", pct)) + styleDim.Render(")")
	}
	ctxPart := styleDim.Render(fmt.Sprintf("ctx %s/%s ", ctx, window)) + ctxPct
	if !t.hasUsage {
		return "  " + ctxPart + styleDim.Render(" · ") + styleDim.Render("↑? ↓? ↻? · ?t/s")
	}
	tokParts := []string{
		styleUser.Render("↑" + fmtTok(t.lastInputTok)),
		styleAgent.Render("↓" + fmtTok(t.lastOutputTok)),
		styleDim.Render("↻" + fmtTok(t.lastCachedTok)),
	}
	parts := []string{ctxPart, joinNonEmpty(tokParts, " ")}
	if t.lastTokensPerSec > 0 {
		parts = append(parts, fmt.Sprintf("%.0ft/s", t.lastTokensPerSec))
	} else if t.lastOutputTok > 0 && t.lastDuration.Seconds() > 0 {
		parts = append(parts, fmt.Sprintf("%.0ft/s", float64(t.lastOutputTok)/t.lastDuration.Seconds()))
	} else {
		parts = append(parts, "0t/s")
	}
	return "  " + joinNonEmpty(parts, styleDim.Render(" · "))
}

func (t *TUI) contextPercentStyle(pct int) lipgloss.Style {
	if pct >= 85 {
		return styleError
	}
	if pct >= 60 {
		return lipgloss.NewStyle().Bold(true).Foreground(ColorTool)
	}
	return styleBrand
}

func (t *TUI) renderCommandSuggestions() string {
	view := t.chat.CommandSuggestionsView()
	if !view.Visible {
		return ""
	}
	width := max(24, t.width-4)
	var lines []string
	for i, c := range view.Items {
		prefix := "  "
		style := lipgloss.NewStyle()
		if i == view.Selected {
			prefix = styleCursor.Render("▶ ")
			style = styleHL
		}
		line := prefix + style.Render(fmt.Sprintf("%-16s", c.Cmd)) + styleDim.Render(t.tr(c.DescKey))
		lines = append(lines, line)
	}
	lines = append(lines, styleDim.Render(t.tr("tui.command.suggestion_help")))
	return boxStyle.Width(width).Render(strings.Join(lines, "\n"))
}

func (t *TUI) renderModelPicker() string {
	models := t.configModelsSnapshot()
	rows := make([]chatpage.ModelPickerRow, 0, len(models))
	for _, mc := range models {
		rows = append(rows, chatpage.ModelPickerRow{Ref: mc.Ref(), Summary: t.modelSummary(mc), Mark: tuiconfig.ModelStatusMark(mc, t.isActiveModelRef(mc.Ref()))})
	}
	view := t.chat.ModelPickerView(rows, chatpage.ModelPickerLabels{
		Empty: t.tr("cmd.model_none"),
		Title: t.tr("cmd.model_choose"),
		Help:  t.tr("cmd.model_picker_help"),
	}, max(40, min(72, t.width-6)))
	if len(view.Rows) == 0 {
		return "  " + styleDim.Render(view.Empty) + "\n"
	}
	var lines []string
	lines = append(lines, styleHL.Render(view.Title))
	for i, row := range view.Rows {
		cursor := "  "
		st := lipgloss.NewStyle()
		if i == view.Selected {
			cursor = styleCursor.Render("▶ ")
			st = styleHL
		}
		lines = append(lines, cursor+st.Render(row.Mark+" "+row.Ref)+styleDim.Render("  "+row.Summary))
	}
	lines = append(lines, styleDim.Render(view.Help))
	return textutil.IndentLines(boxStyle.Width(view.Width).Padding(1, 2).Render(strings.Join(lines, "\n")), "  ") + "\n"
}

const subtaskTimelineMaxRows = 5

func renderInputSeparator(width int) string {
	if width <= 0 {
		return ""
	}
	lineWidth := max(12, width-4)
	return "  " + styleDim.Render(strings.Repeat("─", lineWidth))
}

func (t *TUI) renderInputArea() string {
	confirm := ""
	if t.chat.HasDiscardDraftConfirm() {
		confirm = styleError.Render(t.tr("tui.chat.discard_draft")) + " " + styleDim.Render(t.tr("tui.chat.discard_draft_help"))
	}
	width := max(40, t.width-4)
	text := strings.TrimRight(t.chat.Textarea.View(), "\n")
	emptyInput := !t.inputLocked() && !t.hasDraft()
	if t.inputLocked() && !t.hasDraft() {
		text = styleDim.Render(t.lockedInputPlaceholder())
	}
	if emptyInput {
		text = styleDim.Render(t.tr("tui.chat.input_placeholder"))
	}
	bar := renderInputComposerBar(width, strings.Split(text, "\n"), emptyInput, t.inputCursorVisible)
	parts := make([]string, 0, 5)
	if panel := t.renderAttachmentPanel(); panel != "" {
		parts = append(parts, textutil.IndentLines(panel, "  "))
	}
	parts = append(parts, textutil.IndentLines(bar, "  "))
	if help := t.inputHelp(); help != "" {
		parts = append(parts, "  "+styleDim.Render(help))
	}
	if confirm != "" {
		parts = append(parts, "  "+confirm)
	}
	return strings.Join(parts, "\n")
}

func renderInputComposerBar(width int, lines []string, emptyInput bool, cursorVisible bool) string {
	contentWidth := max(8, width-4)
	prepared := make([]string, 0, max(1, len(lines)))
	for i, line := range lines {
		wrapped := textutil.WrapLine(line, contentWidth)
		if len(wrapped) == 0 {
			wrapped = []string{""}
		}
		for j, visualLine := range wrapped {
			prefix := styleBrand.Render("▌ ")
			if emptyInput && i == 0 && j == 0 && !cursorVisible {
				prefix = "  "
			}
			prepared = append(prepared, prefix+visualLine)
		}
	}
	if len(prepared) == 0 {
		if cursorVisible {
			prepared = append(prepared, styleBrand.Render("▌"))
		} else {
			prepared = append(prepared, " ")
		}
	}
	return strings.Join(prepared, "\n")
}

func (t *TUI) lockedInputPlaceholder() string {
	policy := t.currentInputPolicy()
	if policy.Placeholder != "" {
		return policy.Placeholder
	}
	return t.tr("status.responding")
}

func (t *TUI) renderPreInputHint() string {
	if block := t.renderHandoffBlock(); block != "" {
		return block
	}
	hint := t.inputHint()
	if hint == "" {
		return ""
	}
	return "  " + hint
}

func (t *TUI) inputHint() string {
	if t.chat.HasBlockingInteraction() {
		return ""
	}
	if hint := t.resumeHint(); hint != "" {
		return hint
	}
	return t.responseNavHint()
}

func (t *TUI) inputHelp() string {
	if t.inputLocked() {
		if t.chat.Compacting {
			return ""
		}
		if t.observingRun() {
			return t.tr("tui.chat.input_help_observing")
		}
		return t.tr("tui.chat.input_help_running")
	}
	if t.hasDraft() {
		return t.tr("tui.chat.input_help_draft")
	}
	return t.tr("tui.chat.input_help_empty")
}

func (t *TUI) resumeHint() string {
	if !t.chat.ResumeAvailable || t.inputLocked() {
		return ""
	}
	return styleDim.Render(t.tr("session.resume_hint"))
}

func (t *TUI) responseNavHint() string {
	if t.inputLocked() || !t.chat.ResponseNavAvailable || t.chat.ResponseNavDismissed {
		return ""
	}
	key := "session.response_nav_hint"
	if t.chat.ResponseNavJumped {
		key = "session.response_nav_jumped"
	}
	return styleDim.Render(t.tr(key))
}

func (t *TUI) updateGuardConfirm(ks string) (tea.Model, tea.Cmd) {
	switch ks {
	case "ctrl+c":
		t.doQuit()
		return t, tea.Quit
	case "left", "right":
		if t.chat.GuardCursor == 0 {
			t.chat.GuardCursor = 1
		} else {
			t.chat.GuardCursor = 0
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
		if t.chat.GuardCursor == 0 {
			return t, t.submitGuardDecision("approve")
		}
		return t, t.submitGuardDecision("reject")
	}
	return t, nil
}
func (t *TUI) submitGuardDecision(decision string) tea.Cmd {
	guard := t.chat.ActiveGuard()
	if guard == nil {
		return nil
	}
	id := guard.ID
	guardToolID := guard.ToolCallID
	if decision == "reject" {
		t.markToolRejected(guardToolID)
	}
	t.advanceGuardQueue()
	restartSpinner := false
	if t.chat.ActiveGuard() == nil {
		t.currentRunCanControl = true
		t.chat.Textarea.Blur()
		t.chat.ResumeToolPhase(time.Now())
		restartSpinner = true
	}
	cmd := t.guardReplyCmd(id, decision)
	if restartSpinner && !t.chat.HasBlockingInteraction() {
		return tea.Batch(cmd, t.startChatSpinner())
	}
	return cmd
}
func (t *TUI) enqueueGuardConfirm(g *guardConfirmView) { t.chat.EnqueueGuardConfirm(g) }
func (t *TUI) advanceGuardQueue()                      { t.chat.AdvanceGuardQueue() }

func (t *TUI) renderGuardOverlay(width int) string {
	view := t.chat.GuardOverlayView(width, t.overlayMaxHeight(), chatpage.GuardOverlayLabels{
		Title:      t.tr("tui.guard.title"),
		Tool:       t.tr("tui.guard.tool"),
		Risk:       t.tr("tui.guard.risk"),
		Review:     t.tr("tui.guard.review"),
		Reason:     t.tr("tui.guard.reason"),
		Suggestion: t.tr("tui.guard.suggestion"),
		Params:     t.tr("tui.tool.params"),
		Approve:    t.tr("tui.guard.approve"),
		Reject:     t.tr("tui.guard.reject"),
		Help:       t.tr("tui.guard.help"),
		Hidden:     t.tr("tui.overlay.content_hidden"),
		Scroll:     t.tr("tui.overlay.scroll"),
	})
	g := view.Guard
	if g == nil {
		return ""
	}
	body := t.guardOverlayBodyLines(view)
	body, start, total := scrollWindow(body, view.BodyHeight, &t.chat.GuardScroll)

	var lines []string
	lines = append(lines, styleError.Render("⚠ "+view.Labels.Title))
	lines = append(lines, "")
	lines = append(lines, styleDim.Render(view.Labels.Tool)+" "+styleTool.Render(g.Tool))
	lines = append(lines, styleDim.Render(view.Labels.Risk)+" "+t.guardRiskStyle(g.Risk).Render(g.Risk))
	if len(body) > 0 {
		lines = append(lines, "")
		lines = append(lines, body...)
	}
	approve := t.guardButton(0, view.Labels.Approve)
	reject := t.guardButton(1, view.Labels.Reject)
	lines = append(lines, "", approve+"  "+reject, styleDim.Render(chatpage.GuardHelpText(start, view.BodyHeight, total, view.Labels)))
	return boxStyle.Width(view.Width).Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func (t *TUI) guardOverlayBodyLines(view chatpage.GuardOverlayView) []string {
	g := view.Guard
	if g == nil {
		return nil
	}
	var body []string
	if strings.TrimSpace(g.ReviewCode) != "" || strings.TrimSpace(g.ReviewMessage) != "" {
		body = append(body, styleDim.Render(view.Labels.Review))
		review := strings.TrimSpace(g.ReviewMessage)
		if code := strings.TrimSpace(g.ReviewCode); code != "" {
			if review != "" {
				review += " (" + code + ")"
			} else {
				review = code
			}
		}
		body = append(body, splitWrapped(review, view.Inner, 0)...)
	}
	if strings.TrimSpace(g.Reason) != "" {
		if len(body) > 0 {
			body = append(body, "")
		}
		body = append(body, styleDim.Render(view.Labels.Reason))
		body = append(body, splitWrapped(g.Reason, view.Inner, 0)...)
	}
	if strings.TrimSpace(g.Suggestion) != "" {
		if len(body) > 0 {
			body = append(body, "")
		}
		body = append(body, styleDim.Render(view.Labels.Suggestion))
		body = append(body, splitWrapped(g.Suggestion, view.Inner, 0)...)
	}
	params := chatpage.GuardBodyParams(g)
	if params != "" {
		if len(body) > 0 {
			body = append(body, "")
		}
		body = append(body, styleDim.Render(view.Labels.Params))
		body = append(body, splitWrapped(params, view.Inner, 0)...)
	}
	return body
}

func (t *TUI) guardOverlayBodyHeight() int {
	return t.chat.GuardOverlayView(t.width, t.overlayMaxHeight(), chatpage.GuardOverlayLabels{}).BodyHeight
}

func (t *TUI) guardHelpText(start, height, total int) string {
	return chatpage.GuardHelpText(start, height, total, chatpage.GuardOverlayLabels{
		Help:   t.tr("tui.guard.help"),
		Hidden: t.tr("tui.overlay.content_hidden"),
		Scroll: t.tr("tui.overlay.scroll"),
	})
}

func (t *TUI) guardButton(idx int, label string) string {
	if t.chat.GuardCursor == idx {
		return styleCursor.Render("▶ ") + styleHL.Render(label)
	}
	return styleDim.Render("  " + label)
}

func (t *TUI) guardRiskStyle(risk string) lipgloss.Style {
	switch strings.ToLower(risk) {
	case "high":
		return styleError
	case "medium":
		return styleTool
	default:
		return styleAgent
	}
}

func (t *TUI) overlayMaxHeight() int {
	if t.chat.Viewport.Height() > 0 {
		return max(8, t.chat.Viewport.Height())
	}
	if t.height > 0 {
		return max(8, t.height-8)
	}
	return 16
}

func scrollWindow(lines []string, height int, offset *int) ([]string, int, int) {
	total := len(lines)
	if height <= 0 || total == 0 {
		if offset != nil {
			*offset = 0
		}
		return nil, 0, total
	}
	maxOffset := max(0, total-height)
	start := 0
	if offset != nil {
		if *offset < 0 {
			*offset = 0
		}
		if *offset > maxOffset {
			*offset = maxOffset
		}
		start = *offset
	}
	end := min(total, start+height)
	return lines[start:end], start, total
}

func (t *TUI) scrollGuardOverlay(delta int) {
	view := t.chat.GuardOverlayView(t.width, t.overlayMaxHeight(), chatpage.GuardOverlayLabels{})
	maxOffset := max(0, len(t.guardOverlayBodyLines(view))-view.BodyHeight)
	t.chat.GuardScroll += delta
	if t.chat.GuardScroll < 0 {
		t.chat.GuardScroll = 0
	}
	if t.chat.GuardScroll > maxOffset {
		t.chat.GuardScroll = maxOffset
	}
}
