package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/tui/components/overlay"
	textutil "github.com/alanchenchen/suna/internal/tui/components/text"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
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
	modelOverlay := ""
	if t.chat.ModelPickerOpen {
		modelOverlay = t.renderModelOverlay(t.width)
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
	attachmentsOverlay := ""
	if t.chat.AttachmentsOverlayOpen {
		attachmentsOverlay = t.renderAttachmentsOverlay(t.width)
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
		MiniPet:            renderMiniPet(petState, t.petFrame),
		TopMeta:            t.chatTopMeta(),
		Conn:               t.chatConnectionDot(),
		Content:            t.chat.Viewport.View(),
		Separator:          separator,
		InputSeparator:     inputSeparator,
		InputArea:          t.renderInputArea(),
		PreInputHint:       preInputHint,
		CommandSuggestions: cmdSuggestions,
		StatusBar:          t.renderChatStatusBar(),
		ToolDetailOverlay:  toolOverlay,
		HelpOverlay:        helpOverlay,
		ModelOverlay:       modelOverlay,
		SkillsOverlay:      skillsOverlay,
		MCPOverlay:         mcpOverlay,
		MemoryOverlay:      memoryOverlay,
		SessionsOverlay:    sessionsOverlay,
		AttachmentsOverlay: attachmentsOverlay,
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
	if !t.petHappyUntil.IsZero() && time.Now().Before(t.petHappyUntil) {
		return petHappy
	}
	if !t.chat.Loading {
		return petIdle
	}
	if t.chat.Phase == phaseThinking {
		return petThinking
	}
	return petWorking
}

func (t *TUI) chatConnectionDot() string {
	badge := t.mcpBadge()
	conn := ""
	if t.localCli == nil || !t.localCli.Connected() {
		conn = styleDim.Render("○")
	} else {
		// 连接点表达 daemon 健康状态：会话运行状态已由 pet 动画承担，避免重复。
		// 有 MCP 服务器错误时降级为警告色，daemon 断开时显示空心点。
		if t.hasMCPError() {
			conn = styleToolRun.Render("●")
		} else {
			conn = styleAgent.Render("●")
		}
	}
	if badge != "" {
		return badge + " " + conn
	}
	return conn
}

// hasMCPError 检查是否有 MCP 服务器处于错误状态，用于连接点降级为警告色。
func (t *TUI) hasMCPError() bool {
	for _, server := range t.chat.MCPServers {
		if server.State == protocol.MCPServerError || server.Error != "" {
			return true
		}
	}
	return false
}

func (t *TUI) mcpBadge() string {
	active := 0
	for _, server := range t.chat.MCPServers {
		if server.State == protocol.MCPServerActive {
			active++
		}
	}
	total := len(t.chat.MCPServers)
	if total == 0 {
		// 没有配置 MCP 服务器时不显示徽标，右上角只保留连接点，避免长期占位噪音。
		return ""
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
	thinking := t.chatTopThinkingMeta()
	available := max(10, t.width/2)
	if thinking == "" {
		return styleHL.Render(textutil.TruncateRunes(modelRef, available))
	}
	thinkingWidth := lipgloss.Width(thinking)
	if available <= thinkingWidth+12 {
		return styleHL.Render(textutil.TruncateRunes(modelRef, available))
	}
	modelWidth := available - thinkingWidth
	return styleHL.Render(textutil.TruncateRunes(modelRef, modelWidth)) + thinking
}

func (t *TUI) chatTopThinkingMeta() string {
	mc, ok := t.modelByRef(t.currentSession.ModelRef)
	if !ok {
		return ""
	}
	reasoning := strings.TrimSpace(t.reasoningDisplay(mc))
	if reasoning == "" {
		return ""
	}
	if _, level, found := strings.Cut(reasoning, " / "); found {
		reasoning = level
	}
	return styleDim.Render(" ┊ ") + styleThinkingIcon.Render("◇") + styleThinkingLabel.Render(" Think ") + styleThinkingValue.Render(reasoning)
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
	bar := ""
	if t.contextWindow > 0 {
		bar = t.renderContextBar(pct) + " "
	}
	ctxPart := styleDim.Render(fmt.Sprintf("ctx %s/%s ", ctx, window)) + bar + ctxPct
	// 左侧最前显示当前会话项目目录（basename），空间不足时优先隐藏 cwd 而非用量。
	// 预留 70 列给 ctx 状态与右侧用量，剩余宽度给 cwd；窄终端下 cwd 自动截断/隐藏。
	cwdPart := t.statusBarCWD(max(0, t.width-70))
	if !t.hasUsage {
		// 无用量数据时也走左右分栏：右侧占位右对齐，窄终端截断，避免初始状态全部挤在左侧。
		right := styleDim.Render("↑? ↓? cached ? · ?t/s")
		left := "  " + cwdPart + "  " + ctxPart
		available := max(20, t.width-2)
		rightWidth := lipgloss.Width(right)
		if lipgloss.Width(left)+rightWidth > available {
			right = textutil.TruncateRunes(right, max(8, available-lipgloss.Width(left)-1))
			rightWidth = lipgloss.Width(right)
		}
		pad := max(1, available-lipgloss.Width(left)-rightWidth)
		return left + strings.Repeat(" ", pad) + right
	}
	tokParts := []string{
		styleUser.Render("↑" + fmtTok(t.lastInputTok)),
		styleAgent.Render("↓" + fmtTok(t.lastOutputTok)),
		styleBrand.Render("cached " + fmtTok(t.lastCachedTok)),
	}
	parts := []string{joinNonEmpty(tokParts, " ")}
	if t.lastTokensPerSec > 0 {
		parts = append(parts, fmt.Sprintf("%.0ft/s", t.lastTokensPerSec))
	} else if t.lastOutputTok > 0 && t.lastDuration.Seconds() > 0 {
		parts = append(parts, fmt.Sprintf("%.0ft/s", float64(t.lastOutputTok)/t.lastDuration.Seconds()))
	} else {
		parts = append(parts, "0t/s")
	}
	right := joinNonEmpty(parts, styleDim.Render(" · "))
	// 左右分栏：左侧上下文状态，右侧用量右对齐；空间不足时优先保留上下文，截断用量。
	left := "  " + cwdPart + "  " + ctxPart
	available := max(20, t.width-2)
	rightWidth := lipgloss.Width(right)
	if lipgloss.Width(left)+rightWidth > available {
		right = textutil.TruncateRunes(right, max(8, available-lipgloss.Width(left)-1))
		rightWidth = lipgloss.Width(right)
	}
	pad := max(1, available-lipgloss.Width(left)-rightWidth)
	return left + strings.Repeat(" ", pad) + right
}

// statusBarCWD 返回状态栏左侧的项目目录片段（📁 basename），
// 复用 windowTitleWorkspace 的 basename 提取与控制字符清理；
// 无会话目录时回退启动目录，两者都为空则返回空串（不显示）。
// maxWidth 为片段最大宽度，超出时截断 basename；宽度不足时返回空串（隐藏）。
func (t *TUI) statusBarCWD(maxWidth int) string {
	workspace := windowTitleWorkspace(t.currentSession.CWD)
	if workspace == "" {
		workspace = windowTitleWorkspace(t.launchCWD)
	}
	if workspace == "" {
		return ""
	}
	// 前缀 "📁 " 占 3 列（emoji 2 列 + 空格 1 列），末尾保留 1 空格与 ctx 分隔。
	const prefix = "📁 "
	avail := maxWidth - lipgloss.Width(prefix) - 1
	if avail <= 0 {
		return ""
	}
	workspace = textutil.TruncateRunes(workspace, avail)
	return styleDim.Render(prefix + workspace + " ")
}

// renderContextBar 渲染上下文占用进度条（█ 填充 / ░ 空余），颜色随占用比例变化。
func (t *TUI) renderContextBar(pct int) string {
	const barWidth = 8
	// 四舍五入，避免低占用（如 9%）在窄条下显示为全空。
	filled := (barWidth*pct + 50) / 100
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	return t.contextPercentStyle(pct).Render(bar)
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
	lastGroup := chatpage.CommandGroup("")
	for i, c := range view.Items {
		if c.Group != lastGroup {
			if titleKey := chatpage.CommandGroupTitle(c.Group); titleKey != "" {
				lines = append(lines, styleDim.Render(t.tr(titleKey)))
			}
			lastGroup = c.Group
		}
		prefix := "  "
		style := lipgloss.NewStyle()
		if i == view.Selected {
			prefix = styleCursor.Render("▎ ")
			style = styleHL
		}
		line := prefix + style.Render(fmt.Sprintf("%-16s", c.Cmd)) + styleDim.Render(t.tr(c.DescKey))
		lines = append(lines, line)
	}
	lines = append(lines, styleDim.Render(t.tr("tui.command.suggestion_help")))
	return boxStyle.Width(width).Render(strings.Join(lines, "\n"))
}

func (t *TUI) renderModelOverlay(width int) string {
	return t.renderNativeListOverlay(chatpage.NativeListModels, &t.chat.ModelList, width, t.nativeListText().Select, "cmd.model_none", "", "")
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
	presentation := t.currentInteractionPresentation()
	confirm := ""
	if t.chat.HasDiscardDraftConfirm() {
		confirm = styleError.Render(t.tr("tui.chat.discard_draft")) + " " + styleDim.Render(t.tr("tui.chat.discard_draft_help"))
	}
	width := max(40, t.width-4)
	text := strings.TrimRight(t.chat.Textarea.View(), "\n")
	runStatus := ""
	if t.canSteerCurrentRun() {
		status := t.currentInputStatusLabel()
		if t.chat.Compacting {
			status = t.compactElapsedLabel()
		}
		runStatus = renderInlineRunStatus(width, strings.TrimSpace(t.chat.Spinner.View()+" "+status), t.tr("tui.chat.input_help_running"))
	} else if t.cancelling && t.chat.Loading {
		runStatus = styleDim.Render(strings.TrimSpace(t.chat.Spinner.View() + " " + t.tr("status.cancelling")))
	}
	// 输入区 placeholder 只按原始输入值判断，不能复用 HasDraft()。
	// HasDraft() 会 trim 空白用于发送/退出判断；如果用户刚输入空格或换行，
	// 这里仍应立刻隐藏 placeholder，避免 Bubble textarea 与外层占位文案不同步。
	emptyInput := !presentation.Locked && t.chat.Textarea.Value() == "" && len(t.chat.Attachments) == 0
	inlineRunHelp := false
	if presentation.Locked && !t.hasDraft() {
		status := t.lockedInputPlaceholder()
		if presentation.InputPolicy.AllowCancel {
			text = renderInlineRunStatus(width, status, t.tr("tui.chat.input_help_running"))
			inlineRunHelp = true
		} else {
			text = styleDim.Render(status)
		}
	}
	if presentation.GuardActive {
		text = styleError.Render(t.tr("tui.guard.input_waiting"))
	} else if presentation.TerminalSelection {
		text = renderInlineRunStatus(width, t.tr("tui.selection_mode.hint"), t.tr("tui.selection_mode.back"))
	} else if emptyInput {
		text = styleDim.Render(t.tr("tui.chat.input_placeholder"))
	}
	bar := renderInputComposerBar(width, strings.Split(text, "\n"), emptyInput, t.inputCursorVisible)
	parts := make([]string, 0, 7)
	if runStatus != "" {
		parts = append(parts, "  "+runStatus)
	}
	if queue := t.renderSteeringQueueLine(width); queue != "" {
		parts = append(parts, "  "+queue)
	}
	if panel := t.renderAttachmentPanel(); panel != "" {
		parts = append(parts, textutil.IndentLines(panel, "  "))
	}
	parts = append(parts, textutil.IndentLines(bar, "  "))
	if help := t.inputHelp(); help != "" && !inlineRunHelp {
		parts = append(parts, "  "+styleDim.Render(help))
	}
	if confirm != "" {
		parts = append(parts, "  "+confirm)
	}
	return strings.Join(parts, "\n")
}

func (t *TUI) renderSteeringQueueLine(width int) string {
	confirmed := len(t.chat.PendingSteering)
	submitting := 0
	latestSubmitting := ""
	for _, item := range t.chat.SteeringSubmissions {
		if item.Resolved {
			continue
		}
		submitting++
		latestSubmitting = item.Text
	}
	if confirmed == 0 && submitting == 0 {
		return ""
	}
	if confirmed == 0 {
		return styleDim.Render(t.i18n.Tf("tui.chat.queue_submitting", submitting))
	}
	latest := t.chat.PendingSteering[confirmed-1]
	text := singleLineSteeringPreview(steeringMessageText(latest))
	if submitting > 0 {
		text = singleLineSteeringPreview(latestSubmitting)
	}
	count := confirmed + submitting
	label := t.tr("tui.chat.queue_one")
	if count > 1 {
		label = t.i18n.Tf("tui.chat.queue_many", count)
	}
	help := ""
	// 最新提交尚无 daemon ID，不能撤回；隐藏提示以保证文案与实际操作对象一致。
	if submitting == 0 && latest.CanControl {
		help = t.tr("tui.chat.queue_undo")
	}
	available := max(12, width-lipgloss.Width(label)-lipgloss.Width(help)-8)
	text = ansi.Truncate(text, available, "…")
	line := styleBrand.Render("↳ ") + styleDim.Render(label+" · ") + styleToolDim.Render(text)
	if help != "" {
		line += styleDim.Render("  " + help)
	}
	return line
}

func singleLineSteeringPreview(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func renderInlineRunStatus(width int, status, help string) string {
	contentWidth := max(8, width-4)
	statusWidth := lipgloss.Width(status)
	helpWidth := lipgloss.Width(help)
	if statusWidth+helpWidth+1 <= contentWidth {
		gap := strings.Repeat(" ", contentWidth-statusWidth-helpWidth)
		return styleDim.Render(status) + gap + styleDim.Render(help)
	}
	return styleDim.Render(status + " · " + help)
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
	presentation := t.currentInteractionPresentation()
	if presentation.GuardActive {
		return t.tr("tui.guard.input_waiting")
	}
	if presentation.TerminalSelection {
		return t.tr("tui.selection_mode.hint")
	}
	policy := presentation.InputPolicy
	if policy.Placeholder != "" {
		return policy.Placeholder
	}
	return t.tr("status.responding")
}

func (t *TUI) renderPreInputHint() string {
	presentation := t.currentInteractionPresentation()
	if presentation.GuardActive {
		return styleError.Render("  ⚠ "+t.tr("tui.guard.input_waiting")) + styleDim.Render(" · ") + styleDim.Render(t.tr("tui.guard.help"))
	}
	if presentation.TerminalSelection {
		return ""
	}
	if block := t.renderHandoffBlock(); block != "" {
		return block
	}
	hint := t.inputHint()
	if hint == "" {
		return ""
	}
	return hint
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
	presentation := t.currentInteractionPresentation()
	if presentation.TerminalSelection || presentation.GuardActive {
		return ""
	}
	if presentation.Locked {
		if t.chat.Compacting {
			return ""
		}
		if t.observingRun() {
			return t.tr("tui.chat.input_help_observing")
		}
		return ""
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
	label := ""
	key := ""
	arrow := ""
	switch {
	case !t.chat.TranscriptAtBottom() && t.chat.NewContentWhilePaused:
		arrow, label, key = "↓", t.tr("session.response_nav_new"), "End"
	case !t.chat.TranscriptAtBottom():
		arrow, label, key = "↓", t.tr("session.response_nav_latest"), "End"
	case t.chat.ResponseNavAvailable:
		arrow, label, key = "↑", t.tr("session.response_nav_start"), "Home"
	default:
		return ""
	}
	content := styleBrand.Render(arrow) + " " + styleDim.Render(label) + styleDim.Render("   "+key)
	return lipgloss.NewStyle().Width(max(1, t.width)).Align(lipgloss.Center).Render(content)
}

func (t *TUI) updateGuardConfirm(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	keys := chatpage.DefaultKeyMap
	switch {
	case key.Matches(msg, keys.Quit):
		t.doQuit()
		return t, tea.Quit
	case key.Matches(msg, keys.GuardPrevious), key.Matches(msg, keys.GuardNext):
		if t.chat.GuardCursor == 0 {
			t.chat.GuardCursor = 1
		} else {
			t.chat.GuardCursor = 0
		}
		t.syncContent()
		return t, nil
	case key.Matches(msg, keys.GuardScrollUp):
		if msg.String() == "pgup" {
			t.scrollGuardOverlay(-max(1, t.guardOverlayBodyHeight()-1))
		} else {
			t.scrollGuardOverlay(-1)
		}
		t.syncContent()
		return t, nil
	case key.Matches(msg, keys.GuardScrollDown):
		if msg.String() == "pgdown" {
			t.scrollGuardOverlay(max(1, t.guardOverlayBodyHeight()-1))
		} else {
			t.scrollGuardOverlay(1)
		}
		t.syncContent()
		return t, nil
	case key.Matches(msg, keys.GuardReject):
		return t, t.submitGuardDecision("reject")
	case key.Matches(msg, keys.GuardConfirm):
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
	if !t.chat.HasBlockingInteraction() && t.canResumeRunAfterInteraction() {
		t.currentRunCanControl = true
		t.chat.Textarea.Blur()
		t.chat.ResumeToolPhase(time.Now())
		restartSpinner = true
	} else if !t.chat.HasBlockingInteraction() {
		t.chat.ResetPhase()
		_ = t.syncInputFocus()
	} else {
		// 队列可能已推进到允许自定义输入的 AskUser；焦点必须跟随新的交互呈现恢复。
		_ = t.syncInputFocus()
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
