package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

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
	guardOverlay := ""
	if t.chat.PendingGuard != nil {
		guardOverlay = t.renderGuardOverlay(t.width)
	}
	cmdSuggestions := ""
	if len(t.chat.CmdSuggestions) > 0 {
		cmdSuggestions = t.renderCommandSuggestions()
	}
	return t.chat.View(chatpage.ViewDeps{
		Width:              t.width,
		MiniPet:            renderMiniPet(petState),
		TopMeta:            t.chatTopMeta(),
		Conn:               t.chatConnectionDot(petState),
		Content:            t.chat.Viewport.View(),
		Separator:          separator,
		InputArea:          t.renderInputArea(),
		CommandSuggestions: cmdSuggestions,
		StatusBar:          t.renderChatStatusBar(),
		ToolDetailOverlay:  toolOverlay,
		HelpOverlay:        helpOverlay,
		SkillsOverlay:      skillsOverlay,
		MCPOverlay:         mcpOverlay,
		GuardOverlay:       guardOverlay,
		Overlay:            overlay.OverlayBlock,
	})
}

func (t *TUI) layoutChat() {
	layout := chatpage.ComputeLayout(chatpage.LayoutInput{
		Width:            t.width,
		Height:           t.height,
		InputHeight:      t.chat.Textarea.Height(),
		AttachmentHeight: chatpage.AttachmentPanelHeight(t.renderAttachmentPanel()),
		SuggestionCount:  len(t.chat.CmdSuggestions),
		ConfirmDiscard:   t.chat.ConfirmDiscardDraft,
	})
	if layout.ViewportHeight == 0 && layout.InputWidth == 0 {
		return
	}
	t.chat.Viewport.SetWidth(t.width)
	t.chat.Viewport.SetHeight(layout.ViewportHeight)
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
	if t.contextWindow <= 0 {
		return styleHL.Render(modelRef)
	}
	ctxTokens := t.contextTokens
	ctx := "?"
	if ctxTokens > 0 {
		ctx = fmtTok(ctxTokens)
	}
	pct := int(float64(ctxTokens) / float64(t.contextWindow) * 100)
	if ctxTokens <= 0 {
		pct = 0
	}
	return styleHL.Render(textutil.TruncateRunes(modelRef, max(10, t.width/3))) + strings.Repeat(" ", 2) + styleDim.Render(fmt.Sprintf("ctx(%d%%) %s/%s", pct, ctx, fmtTok(t.contextWindow)))
}

func (t *TUI) mouseInComposer(msg tea.MouseMsg) bool {
	m := msg.Mouse()
	return chatpage.MouseInComposer(chatpage.ComposerHitInput{
		Height:           t.height,
		Y:                m.Y,
		InputHeight:      t.chat.Textarea.Height(),
		AttachmentHeight: chatpage.AttachmentPanelHeight(t.renderAttachmentPanel()),
		SuggestionCount:  len(t.chat.CmdSuggestions),
		ConfirmDiscard:   t.chat.ConfirmDiscardDraft,
	})
}

func (t *TUI) renderChatStatusBar() string {
	copyHint := ""
	if t.copyMode {
		copyHint = styleDim.Render(" · ") + styleHL.Render(t.tr("tui.key.copy_mode")) + styleDim.Render(" [Ctrl+Y/Esc]")
	}
	if !t.hasUsage {
		return "  " + styleDim.Render("↑? ↓? ⟳? · ?t/s") + copyHint
	}
	tokParts := []string{
		styleUser.Render("↑" + fmtTok(t.lastInputTok)),
		styleAgent.Render("↓" + fmtTok(t.lastOutputTok)),
		styleDim.Render("⟳" + fmtTok(t.lastCachedTok)),
	}
	parts := []string{joinNonEmpty(tokParts, " ")}
	if t.lastTokensPerSec > 0 {
		parts = append(parts, fmt.Sprintf("%.0ft/s", t.lastTokensPerSec))
	} else if t.lastOutputTok > 0 && t.lastDuration.Seconds() > 0 {
		parts = append(parts, fmt.Sprintf("%.0ft/s", float64(t.lastOutputTok)/t.lastDuration.Seconds()))
	} else {
		parts = append(parts, "0t/s")
	}
	return "  " + joinNonEmpty(parts, styleDim.Render(" · ")) + copyHint
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

func (t *TUI) renderInputArea() string {
	confirm := ""
	if t.chat.ConfirmDiscardDraft {
		confirm = styleError.Render(t.tr("tui.chat.discard_draft")) + " " + styleDim.Render(t.tr("tui.chat.discard_draft_help"))
	}
	separator := "  " + styleDim.Render(strings.Repeat("─", max(10, t.width-4)))
	return t.chat.InputArea(chatpage.InputAreaView{
		Textarea:          t.chat.Textarea.View(),
		LockedPlaceholder: styleDim.Render(t.lockedInputPlaceholder()),
		Locked:            t.inputLocked(),
		HasDraft:          t.hasDraft(),
		Confirm:           confirm,
		Hint:              t.resumeHint(),
		AttachmentPanel:   t.renderAttachmentPanel(),
		Separator:         separator,
	})
}

func (t *TUI) lockedInputPlaceholder() string {
	return t.currentInputPolicy().DisplayPlaceholder(t.tr("status.responding"), t.tr("tui.key.cancel"))
}

func (t *TUI) resumeHint() string {
	if !t.chat.ResumeAvailable || t.inputLocked() {
		return ""
	}
	return styleDim.Render(t.tr("session.resume_hint"))
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
	if t.chat.PendingGuard == nil {
		return nil
	}
	id := t.chat.PendingGuard.ID
	guardToolID := t.chat.PendingGuard.ToolCallID
	if decision == "reject" {
		t.markToolRejected(guardToolID)
	}
	t.advanceGuardQueue()
	restartSpinner := false
	if t.chat.PendingGuard == nil {
		t.chat.Textarea.Blur()
		t.chat.ResumeToolPhase(time.Now())
		restartSpinner = true
	}
	cmd := t.guardReplyCmd(id, decision)
	if restartSpinner {
		return tea.Batch(cmd, t.chat.Spinner.Tick)
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
	if strings.TrimSpace(g.Reason) != "" {
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
