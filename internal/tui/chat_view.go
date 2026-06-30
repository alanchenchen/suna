package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/alanchenchen/suna/internal/tui/components/overlay"
	"github.com/alanchenchen/suna/internal/tui/components/scroll"
	textutil "github.com/alanchenchen/suna/internal/tui/components/text"
	"github.com/alanchenchen/suna/internal/tui/components/toolview"
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
	view := t.chat.View(chatpage.ViewDeps{
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
		GuardOverlay:       guardOverlay,
		Overlay:            overlay.OverlayBlock,
	})
	// spinnerPlaceholder 在渲染阶段写入 transcript，此处统一替换为当前 spinner 帧字符。
	// 替换发生在 View() 阶段，不触发 transcript 重建，spinner 动画仍然正常工作。
	spinChar := t.chat.Spinner.View()
	if imagePasteOverlay != "" {
		return strings.ReplaceAll(t.overlayImagePasteAboveInput(view, imagePasteOverlay, cmdSuggestions), spinnerPlaceholder, spinChar)
	}
	return strings.ReplaceAll(view, spinnerPlaceholder, spinChar)
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
	return styleHL.Render(textutil.TruncateRunes(modelRef, max(10, t.width/3)))
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
	copyHint := ""
	if t.copyMode {
		copyHint = styleDim.Render(" · ") + styleHL.Render(t.tr("tui.key.copy_mode")) + styleDim.Render(" [Ctrl+Y/Esc]")
	}
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
		return "  " + ctxPart + styleDim.Render(" · ") + styleDim.Render("↑? ↓? ↻? · ?t/s") + copyHint
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
	return "  " + joinNonEmpty(parts, styleDim.Render(" · ")) + copyHint
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
		return t.tr("tui.chat.input_help_running")
	}
	if t.hasDraft() {
		return t.tr("tui.chat.input_help_draft")
	}
	return t.tr("tui.chat.input_help_empty")
}

func (t *TUI) renderSubtaskBlock(block *toolBlock) string {
	if block == nil {
		return ""
	}
	ids := subtaskIDsInBlock(block)
	if len(ids) == 0 {
		return ""
	}
	active := block == t.chat.CurrentToolBlock
	if active {
		t.ensureSubtaskSelection()
	}
	width := max(40, t.width-8)
	innerWidth := max(24, width-8)
	sectionWidth := max(24, width-4)
	done, running, failed := t.subtaskStatusCounts(ids)
	title := fmt.Sprintf("%s "+t.tr("tui.subtask_panel.title"), t.subtaskBlockStatusIcon(done, running, failed, len(ids)), len(ids), running, done, failed)
	lines := make([]string, 0)
	selected := -1
	if active {
		selected = t.chat.SubtaskCursor
	}
	lines = append(lines, t.renderSubtaskRows(ids, innerWidth, selected)...)
	if active {
		if current := t.selectedSubtask(); current != nil {
			lines = append(lines, t.subtaskSectionTitle(t.tr("tui.subtask_panel.current"), sectionWidth))
			lines = append(lines, t.renderSelectedSubtaskSummary(current, innerWidth)...)
			lines = append(lines, t.subtaskSectionTitle(t.tr("tui.subtask_panel.tools"), sectionWidth))
			lines = append(lines, t.renderSelectedSubtaskTools(innerWidth)...)
			if t.chat.SubtaskToolDetailExpanded {
				lines = append(lines, t.subtaskSectionTitle(t.tr("tui.subtask_panel.tool_detail"), sectionWidth))
				lines = append(lines, t.renderSelectedSubtaskToolDetail(innerWidth)...)
			}
		}
		lines = append(lines, styleDim.Render(t.tr(t.subtaskPanelHelpKey())))
	}
	return textutil.IndentLines(renderTitledRoundBox(width, title, lines), transcriptBlockIndent)
}

func subtaskIDsInBlock(block *toolBlock) []string {
	if block == nil {
		return nil
	}
	ids := make([]string, 0, len(block.Order))
	for _, id := range block.Order {
		te := block.Entries[id]
		if toolview.IsSubtask(te) {
			ids = append(ids, id)
		}
	}
	return ids
}

func renderTitledRoundBox(width int, title string, lines []string) string {
	return renderTitledRoundBoxWithStyles(width, title, lines, styleHL, styleDim)
}

func renderThinkingRoundBox(width int, title string, lines []string) string {
	return renderTitledRoundBoxWithStyles(width, title, lines, styleBrand, styleBrand)
}

func renderTitledRoundBoxWithStyles(width int, title string, lines []string, titleStyle, borderStyle lipgloss.Style) string {
	// 手工绘制带标题边框时，width 表示整块外宽；边框之间的可用宽度是 width-2。
	// 内容行在进入这里前已经按内宽截断，这里只负责补齐，避免 ANSI 样式导致右边框漂移。
	width = max(12, width)
	contentWidth := max(8, width-2)
	titleText := strings.TrimSpace(title)
	if lipgloss.Width(titleText) > contentWidth-3 {
		titleText = textutil.TruncateRunes(titleText, max(1, contentWidth-3))
	}
	titlePrefix := "─ "
	titleSuffix := " "
	titleWidth := lipgloss.Width(titlePrefix) + lipgloss.Width(titleText) + lipgloss.Width(titleSuffix)
	topRest := strings.Repeat("─", max(0, contentWidth-titleWidth))
	top := borderStyle.Render("╭"+titlePrefix) + titleStyle.Render(titleText) + borderStyle.Render(titleSuffix+topRest+"╮")
	body := make([]string, 0, len(lines)+2)
	body = append(body, top)
	for _, line := range lines {
		content := ansi.Truncate(" "+line+" ", contentWidth, "…")
		pad := strings.Repeat(" ", max(0, contentWidth-lipgloss.Width(content)))
		body = append(body, borderStyle.Render("│")+content+pad+borderStyle.Render("│"))
	}
	body = append(body, borderStyle.Render("╰"+strings.Repeat("─", contentWidth)+"╯"))
	return strings.Join(body, "\n")
}

func (t *TUI) renderSubtaskRows(ids []string, innerWidth int, selected int) []string {
	rows := make([]string, 0, len(ids))
	for i, id := range ids {
		te := t.findTool(id)
		if te == nil {
			continue
		}
		cursor := "  "
		labelStyle := lipgloss.NewStyle()
		if i == selected {
			cursor = styleCursor.Render("▶ ")
			labelStyle = styleHL
		}
		icon := t.subtaskStatusIcon(te)
		prefixWidth := 4 // cursor(2) + icon(1) + space(1)
		dur := t.subtaskDuration(te)
		durWidth := 0
		if dur != "" {
			durWidth = lipgloss.Width(" · " + dur)
		}
		rawLabel := toolview.PlainIntentLabel(te)
		rawActivity := t.subtaskActivity(te, innerWidth)
		label, activity := fitSubtaskRowParts(rawLabel, rawActivity, innerWidth-prefixWidth-durWidth)
		line := fmt.Sprintf("%s%s %s", cursor, icon, labelStyle.Render(label))
		if activity != "" {
			line += styleDim.Render(" · " + activity)
		}
		if dur != "" {
			line += styleDim.Render(" · " + dur)
		}
		rows = append(rows, line)
	}
	return rows
}

func (t *TUI) renderSelectedSubtaskSummary(te *toolEntry, innerWidth int) []string {
	label := textutil.TruncateRunes(toolview.PlainIntentLabel(te), max(12, innerWidth))
	parts := []string{styleHL.Render(label)}
	if te != nil && te.Status == toolview.StatusError {
		if reason := t.subtaskFailureReason(te); reason != "" {
			parts = append(parts, styleDim.Render(t.tr("tui.subtask_panel.error")+": ")+styleToolErr.Render(textutil.TruncateRunes(reason, max(12, innerWidth-8))))
		}
	}
	if model := subtaskParamLabel(te, "model"); model != "" {
		parts = append(parts, styleDim.Render(t.tr("tui.tool.model")+": ")+styleToolDim.Render(textutil.TruncateRunes(model, max(10, innerWidth-8))))
	}
	if tools := subtaskParamLabel(te, "tools"); tools != "" {
		parts = append(parts, styleDim.Render(t.tr("tui.tool.tools")+": ")+styleToolDim.Render(textutil.TruncateRunes(tools, max(10, innerWidth-8))))
	}
	if task := subtaskParamLabel(te, "task"); task != "" {
		parts = append(parts, styleDim.Render(t.tr("tui.tool.task")+": ")+styleToolDim.Render(textutil.TruncateRunes(task, max(12, innerWidth-8))))
	}
	return parts
}

func (t *TUI) renderSelectedSubtaskTools(innerWidth int) []string {
	children := t.selectedSubtaskTools()
	if len(children) == 0 {
		if t.selectedSubtaskWaitingForTool() {
			return []string{styleToolRun.Render("◐ ") + styleDim.Render(t.tr("tui.subtask_panel.waiting_tool"))}
		}
		return []string{styleDim.Render(t.tr("tui.subtask_panel.no_tools"))}
	}
	t.ensureSubtaskSelection()
	height := min(len(children), t.subtaskTimelineHeight())
	start := t.chat.SubtaskToolCursor - height + 1
	if start < 0 {
		start = 0
	}
	if start+height > len(children) {
		start = max(0, len(children)-height)
	}
	end := min(len(children), start+height)
	rows := make([]string, 0, height+2)
	if start > 0 {
		rows = append(rows, styleDim.Render(fmt.Sprintf(t.tr("tui.subtask_panel.more_above"), start)))
	}
	for i := start; i < end; i++ {
		child := children[i]
		cursor := "  "
		labelStyle := lipgloss.NewStyle()
		if i == t.chat.SubtaskToolCursor {
			cursor = styleCursor.Render("▶ ")
			labelStyle = styleHL
		}
		icon := t.subtaskStatusIcon(child)
		prefixWidth := 4 // cursor(2) + icon(1) + space(1)
		dur := ""
		if child.Duration > 0 {
			dur = fmt.Sprintf("%.1fs", child.Duration.Seconds())
		}
		durWidth := 0
		if dur != "" {
			durWidth = lipgloss.Width(" · " + dur)
		}
		remaining := max(0, innerWidth-prefixWidth-durWidth)
		label := t.subtaskToolTimelineLabel(child, max(4, remaining))
		line := fmt.Sprintf("%s%s %s", cursor, icon, labelStyle.Render(label))
		if dur != "" {
			line += styleDim.Render(" · " + dur)
		}
		rows = append(rows, line)
	}
	if end < len(children) {
		rows = append(rows, styleDim.Render(fmt.Sprintf(t.tr("tui.subtask_panel.more_below"), len(children)-end)))
	} else if t.selectedSubtaskWaitingForTool() {
		rows = append(rows, styleToolRun.Render("◐ ")+styleDim.Render(" "+t.tr("tui.subtask_panel.waiting_tool")))
	}
	return rows
}

func (t *TUI) subtaskTimelineHeight() int {
	// 详情展开时优先保留顶部子任务信息和下方详情区，小终端下进一步压缩工具列表高度。
	if t.chat.SubtaskToolDetailExpanded {
		return min(5, max(3, t.height/10))
	}
	return min(7, max(subtaskTimelineMaxRows, t.height/8))
}

func (t *TUI) subtaskToolTimelineLabel(child *toolEntry, width int) string {
	if child == nil {
		return ""
	}
	if intent := strings.TrimSpace(toolview.PlainIntentLabel(child)); intent != "" {
		return textutil.TruncateRunes(intent, width)
	}
	if semantic := strings.TrimSpace(toolview.SemanticSummary(child, width, t.toolRenderDeps().Labels)); semantic != "" {
		return textutil.TruncateRunes(semantic, width)
	}
	name := strings.TrimSpace(child.Name)
	if name == "" {
		name = toolview.DisplayName(child.RawName)
	}
	return textutil.TruncateRunes(name, width)
}

func (t *TUI) subtaskActiveToolLabel(child *toolEntry, width int) string {
	if child == nil {
		return ""
	}
	name := strings.TrimSpace(child.Name)
	if name == "" {
		name = toolview.DisplayName(child.RawName)
	}
	semantic := strings.TrimSpace(toolview.SemanticSummary(child, max(8, width-lipgloss.Width(name)-1), t.toolRenderDeps().Labels))
	if semantic == "" || semantic == name {
		return textutil.TruncateRunes(name, width)
	}
	return textutil.TruncateRunes(strings.TrimSpace(name+" "+semantic), width)
}

func (t *TUI) selectedSubtaskWaitingForTool() bool {
	parent := t.selectedSubtask()
	if parent == nil || parent.Status != toolview.StatusRunning {
		return false
	}
	for _, child := range t.selectedSubtaskTools() {
		if child.Status == toolview.StatusRunning {
			return false
		}
	}
	return true
}

func (t *TUI) renderSelectedSubtaskToolDetail(innerWidth int) []string {
	te := t.selectedSubtaskTool()
	if te == nil {
		return []string{styleDim.Render(t.tr("tui.subtask_panel.no_tools"))}
	}
	deps := t.toolDetailDeps()
	deps.Width = max(44, innerWidth)
	source := toolview.DetailLineSource(te, deps)
	body, start, total := scroll.Window(source, t.subtaskToolDetailHeight(), &t.chat.SubtaskToolDetailScroll)
	if total == 0 {
		return []string{styleDim.Render(t.tr("tui.subtask_panel.no_detail"))}
	}
	lines := append([]string(nil), body...)
	end := min(total, start+t.subtaskToolDetailHeight())
	lines = append(lines, styleDim.Render(fmt.Sprintf("PgUp/PgDn/%s %s %d-%d/%d", t.tr("tui.subtask_panel.wheel"), t.tr("tui.overlay.scroll"), start+1, end, total)))
	return lines
}

func (t *TUI) subtaskSectionTitle(title string, width int) string {
	text := "─ " + title + " "
	return styleDim.Render(text + strings.Repeat("─", max(0, width-lipgloss.Width(text))))
}

func (t *TUI) subtaskPanelHelpKey() string {
	if t.chat.SubtaskToolDetailExpanded {
		return "tui.subtask_panel.help_expanded"
	}
	return "tui.subtask_panel.help"
}

func (t *TUI) subtaskToolDetailHeight() int {
	// 工具详情是可展开区域，不能在矮终端里挤掉上方的子任务列表和工具 timeline。
	return max(4, min(6, t.height/6))
}

func subtaskParamLabel(te *toolEntry, key string) string {
	if te == nil || te.ParamsRaw == nil {
		return ""
	}
	value, ok := te.ParamsRaw[key]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

func (t *TUI) subtaskStatusCounts(ids []string) (done, running, failed int) {
	for _, id := range ids {
		switch te := t.findTool(id); {
		case te == nil:
		case te.Status == toolview.StatusDone:
			done++
		case te.Status == toolview.StatusError:
			failed++
		default:
			running++
		}
	}
	return done, running, failed
}

func (t *TUI) subtaskBlockStatusIcon(done, running, failed, total int) string {
	if running > 0 {
		// 使用占位符，避免 spinner tick 触发全量 transcript 重建；viewChat() 统一替换。
		return spinnerPlaceholder
	}
	if failed > 0 {
		return "✗"
	}
	if total > 0 && done == total {
		return "✓"
	}
	return "◷"
}

func (t *TUI) subtaskStatusIcon(te *toolEntry) string {
	if te == nil {
		return styleDim.Render("◷")
	}
	switch te.Status {
	case toolview.StatusDone:
		return styleToolOk.Render("✓")
	case toolview.StatusError:
		return styleToolErr.Render("✗")
	default:
		return styleToolRun.Render("◐")
	}
}

func (t *TUI) subtaskActivity(te *toolEntry, width int) string {
	if te == nil || t.chat.CurrentToolBlock == nil {
		return ""
	}
	if te.Status == toolview.StatusError {
		return t.subtaskFailureReason(te)
	}
	children := toolview.SubtaskChildren(t.chat.CurrentToolBlock, te.ID)
	var latest *toolEntry
	for _, child := range children {
		if child.Status == toolview.StatusRunning {
			return t.subtaskActiveToolLabel(child, width)
		}
		latest = child
	}
	if latest != nil {
		return t.subtaskActiveToolLabel(latest, width)
	}
	if te.Status == toolview.StatusDone {
		return t.tr("tui.subtask_panel.done")
	}
	return t.tr("tui.subtask_panel.waiting")
}

func fitSubtaskRowParts(label, activity string, width int) (string, string) {
	label = strings.TrimSpace(label)
	activity = strings.TrimSpace(activity)
	if width <= 0 {
		return "", ""
	}
	if activity == "" {
		return textutil.TruncateRunes(label, width), ""
	}
	sepWidth := lipgloss.Width(" · ")
	if width <= sepWidth+4 {
		return textutil.TruncateRunes(label, width), ""
	}
	labelWidth := lipgloss.Width(label)
	activityWidth := lipgloss.Width(activity)
	if labelWidth+sepWidth+activityWidth <= width {
		return label, activity
	}
	// 子任务 intent 是主信息，工具活动是辅助信息；根据当前可用宽度动态分配，避免宽屏仍按固定半宽过早截断。
	minActivityWidth := min(20, max(8, width/4))
	labelMax := min(labelWidth, max(12, width-sepWidth-minActivityWidth))
	activityMax := width - sepWidth - labelMax
	if activityMax < 8 {
		return textutil.TruncateRunes(label, width), ""
	}
	return textutil.TruncateRunes(label, labelMax), textutil.TruncateRunes(activity, activityMax)
}

func (t *TUI) subtaskFailureReason(te *toolEntry) string {
	if te == nil {
		return ""
	}
	return toolview.ShortToolError(te.Result)
}

func (t *TUI) subtaskDuration(te *toolEntry) string {
	if te == nil {
		return ""
	}
	if te.Duration > 0 {
		return fmt.Sprintf("%.1fs", te.Duration.Seconds())
	}
	if !te.StartedAt.IsZero() {
		return fmt.Sprintf("%.0fs", time.Since(te.StartedAt).Seconds())
	}
	return ""
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
		t.chat.Textarea.Blur()
		t.chat.ResumeToolPhase(time.Now())
		restartSpinner = true
	}
	cmd := t.guardReplyCmd(id, decision)
	if restartSpinner && !t.chat.HasBlockingInteraction() {
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
