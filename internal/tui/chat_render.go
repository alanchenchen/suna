package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// viewChat 是 Chat 页面的布局入口。
// Chat 页只展示 daemon 推送的状态、消息、token 和工具事件；模型执行、上下文统计和速率计算均由 daemon/core 负责。
func (t *TUI) viewChat() string {
	if t.width == 0 {
		return ""
	}
	t.layoutChat()

	var sb strings.Builder
	petState := t.chatPetState()
	smallPet := strings.Split(renderMiniPet(petState), "\n")
	topMeta := t.chatTopMeta()
	conn := t.chatConnectionDot(petState)

	sb.WriteString(smallPet[0] + "\n")
	sb.WriteString(smallPet[1])
	gap := 2
	used := lipgloss.Width(smallPet[1]) + gap + lipgloss.Width(topMeta) + gap + lipgloss.Width(conn)
	pad := max(gap, t.width-used)
	sb.WriteString(strings.Repeat(" ", gap) + topMeta + strings.Repeat(" ", pad) + conn + "\n")
	sb.WriteString(smallPet[2] + "\n")
	sb.WriteString(styleDim.Render(strings.Repeat("─", t.width)) + "\n")

	content := t.vp.View()
	if t.pendingGuard != nil {
		content = overlayBlock(content, t.renderGuardOverlay(t.width))
	}
	if t.showToolDetail {
		content = overlayBlock(content, t.renderToolDetailOverlay(t.width))
	}
	if t.showHelp {
		content = overlayBlock(content, t.renderHelpOverlay(t.width))
	}
	sb.WriteString(content)
	sb.WriteString(styleDim.Render(strings.Repeat("─", t.width)) + "\n")
	sb.WriteString(t.renderInputArea())
	if len(t.cmdSuggestions) > 0 {
		sb.WriteString("\n" + t.renderCommandSuggestions())
	}
	sb.WriteString("\n" + t.renderChatStatusBar() + "\n")
	return sb.String()
}

func (t *TUI) renderGuardOverlay(width int) string {
	g := t.pendingGuard
	if g == nil {
		return ""
	}
	w := max(44, min(76, width-4))
	inner := max(20, w-8)
	var lines []string
	lines = append(lines, styleError.Render("⚠ "+t.tr("tui.guard.title")))
	lines = append(lines, "")
	lines = append(lines, styleDim.Render(t.tr("tui.guard.tool"))+" "+styleTool.Render(g.tool))
	lines = append(lines, styleDim.Render(t.tr("tui.guard.risk"))+" "+t.guardRiskStyle(g.risk).Render(g.risk))
	if strings.TrimSpace(g.reason) != "" {
		lines = append(lines, styleDim.Render(t.tr("tui.guard.reason"))+" "+g.reason)
	}
	if strings.TrimSpace(g.suggestion) != "" {
		lines = append(lines, styleDim.Render(t.tr("tui.guard.suggestion"))+" "+g.suggestion)
	}
	params := formatToolParams(g.params)
	if params != "" {
		lines = append(lines, "", styleDim.Render(t.tr("tui.tool.params")))
		for i, line := range strings.Split(params, "\n") {
			if i >= 8 {
				lines = append(lines, styleDim.Render("..."))
				break
			}
			for _, wrapped := range wrapLine(line, inner) {
				lines = append(lines, styleToolDim.Render(wrapped))
			}
		}
	}
	approve := t.guardButton(0, t.tr("tui.guard.approve"))
	reject := t.guardButton(1, t.tr("tui.guard.reject"))
	lines = append(lines, "", approve+"  "+reject, styleDim.Render(t.tr("tui.guard.help")))
	return boxStyle.Width(w).Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func (t *TUI) renderRestoreSummaryBox(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	width := max(36, min(76, t.width-6))
	inner := max(20, width-8)
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && strings.Contains(lines[0], "：") {
		lines = lines[1:]
	}
	var body []string
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimPrefix(line, "-"))
		if line == "" {
			continue
		}
		for _, wrapped := range wrapLine(line, inner) {
			body = append(body, styleDim.Render(wrapped))
		}
	}
	if len(body) == 0 {
		body = []string{styleDim.Render(content)}
	}
	title := styleHL.Render("上一轮操作摘要")
	return indentLines(boxStyle.Width(width).Padding(1, 2).Render(title+"\n"+strings.Join(body, "\n")), "  ")
}

func (t *TUI) guardButton(idx int, label string) string {
	if t.guardCursor == idx {
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

func (t *TUI) layoutChat() {
	if t.width == 0 || t.height == 0 {
		return
	}
	inputH := max(1, t.ta.Height())
	suggestionH := 0
	if len(t.cmdSuggestions) > 0 {
		suggestionH = min(4, len(t.cmdSuggestions)) + 2
	}
	fixedH := 6 + inputH + suggestionH
	vpHeight := max(3, t.height-fixedH)
	t.vp.SetWidth(t.width)
	t.vp.SetHeight(vpHeight)
	t.ta.SetWidth(max(20, t.width-4))
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
	width := max(24, t.width-4)
	var lines []string
	for i, c := range t.cmdSuggestions {
		prefix := "  "
		style := lipgloss.NewStyle()
		if i == t.cmdSuggestionIdx {
			prefix = styleCursor.Render("▶ ")
			style = styleHL
		}
		line := prefix + style.Render(fmt.Sprintf("%-16s", c.cmd)) + styleDim.Render(t.tr(c.descKey))
		lines = append(lines, line)
	}
	return boxStyle.Width(width).Render(strings.Join(lines, "\n"))
}

func (t *TUI) renderModelPicker() string {
	models := t.configModelsSnapshot()
	if len(models) == 0 {
		return "  " + styleDim.Render(t.tr("cmd.model_none")) + "\n"
	}
	var lines []string
	lines = append(lines, styleHL.Render(t.tr("cmd.model_choose")))
	for i, mc := range models {
		cursor := "  "
		st := lipgloss.NewStyle()
		if i == t.modelPickerCursor {
			cursor = styleCursor.Render("▶ ")
			st = styleHL
		}
		mark := modelStatusMark(mc, t.isActiveModelRef(mc.Ref()))
		lines = append(lines, cursor+st.Render(mark+" "+mc.Ref())+styleDim.Render("  "+t.modelSummary(mc)))
	}
	lines = append(lines, styleDim.Render(t.tr("cmd.model_picker_help")))
	return indentLines(boxStyle.Width(max(40, min(72, t.width-6))).Padding(1, 2).Render(strings.Join(lines, "\n")), "  ") + "\n"
}

func (t *TUI) renderInputArea() string {
	view := strings.TrimRight(t.ta.View(), "\n")
	if view == "" {
		view = "> "
	}
	return "  " + strings.ReplaceAll(view, "\n", "\n  ")
}

func (t *TUI) chatPetState() petState {
	if !t.loading {
		return petIdle
	}
	if t.phase == phaseThinking {
		return petThinking
	}
	return petWorking
}

func (t *TUI) chatConnectionDot(state petState) string {
	if t.localCli == nil || !t.localCli.Connected() {
		return styleDim.Render("○")
	}
	switch state {
	case petWorking:
		return styleToolRun.Render("●")
	case petThinking:
		return styleBrand.Render("●")
	default:
		return styleAgent.Render("●")
	}
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
	if t.contextWindow <= 0 {
		return styleHL.Render(modelRef)
	}
	ctxTokens := t.contextTokens
	ctx := "?"
	if ctxTokens > 0 {
		ctx = fmtTok(ctxTokens)
	}
	return styleHL.Render(truncateRunes(modelRef, max(10, t.width/3))) + strings.Repeat(" ", 4) + styleDim.Render("ctx "+ctx+"/"+fmtTok(t.contextWindow))
}

func toolParamSummary(name string, params map[string]any) string {
	if len(params) == 0 {
		return ""
	}
	pick := func(keys ...string) string {
		for _, key := range keys {
			if v, ok := params[key]; ok {
				s := fmt.Sprintf("%v", v)
				if s != "" {
					return truncateRunes(s, 32)
				}
			}
		}
		return ""
	}
	switch name {
	case "readfile", "writefile", "editfile", "listdir":
		return pick("path")
	case "exec":
		return pick("command")
	case "readhttp", "writehttp":
		return pick("url")
	case "spawn":
		return pick("task")
	case "askuser":
		return pick("question")
	default:
		return pick("name", "id", "path", "query")
	}
}

func overlayBlock(base, overlay string) string {
	if overlay == "" {
		return base
	}
	baseLines := strings.Split(base, "\n")
	overlayLines := strings.Split(strings.TrimRight(overlay, "\n"), "\n")
	for i, line := range overlayLines {
		if i < len(baseLines) {
			baseLines[i] = line
		} else {
			baseLines = append(baseLines, line)
		}
	}
	return strings.Join(baseLines, "\n")
}

func indentLines(s, prefix string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func indentWrappedPlain(s, prefix string, width int) string {
	if s == "" {
		return prefix
	}
	var out []string
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		for _, wrapped := range wrapLine(line, width) {
			out = append(out, prefix+wrapped)
		}
	}
	return strings.Join(out, "\n")
}

func renderInlineUserMessage(content string, width int) string {
	lines := strings.Split(indentWrappedPlain(content, "", width), "\n")
	if len(lines) == 0 {
		return "  " + styleUserLine.Render("●")
	}
	lines[0] = "  " + styleUserLine.Render("● ") + lines[0]
	for i := 1; i < len(lines); i++ {
		lines[i] = "    " + lines[i]
	}
	return strings.Join(lines, "\n")
}

func truncateRunes(s string, maxWidth int) string {
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes))+3 > maxWidth {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "..."
}

func wrapLine(s string, maxWidth int) []string {
	if maxWidth <= 0 || lipgloss.Width(s) <= maxWidth {
		return []string{s}
	}
	runes := []rune(s)
	var lines []string
	for len(runes) > 0 {
		end := len(runes)
		for end > 0 && lipgloss.Width(string(runes[:end])) > maxWidth {
			end--
		}
		if end <= 0 {
			end = 1
		}
		lines = append(lines, string(runes[:end]))
		runes = runes[end:]
	}
	return lines
}

func parseOptionIndex(input string, maxOptions int) (int, bool) {
	input = strings.TrimSpace(input)
	if n, err := fmt.Sscanf(input, "%d", new(int)); n == 1 && err == nil {
		var idx int
		fmt.Sscanf(input, "%d", &idx)
		if idx >= 1 && idx <= maxOptions {
			return idx - 1, true
		}
	}
	return -1, false
}
