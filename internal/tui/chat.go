package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const inputMaxHeight = 6

type phase int

const (
	phaseIdle phase = iota
	phaseFirstLLM
	phaseLLM
	phaseThinking
	phaseTool
)

var (
	styleUserLine  = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	styleAgentLine = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	styleToolPill  = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("11")).Padding(0, 1).Bold(true)
	styleToolOk    = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	styleToolErr   = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	styleToolRun   = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	styleToolDim   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleReasoning = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
	styleSysLine   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleErrLine   = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
)

type toolStatus int

const (
	toolRunning toolStatus = iota
	toolDone
	toolError
)

type toolEntry struct {
	id       string
	name     string
	intent   string
	params   string
	status   toolStatus
	duration time.Duration
	result   string
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
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithDisabled())
	styles := textarea.DefaultStyles(false)
	styles.Focused.Text = lipgloss.NewStyle()
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(ColorUser).Bold(true)
	styles.Focused.CursorLine = lipgloss.NewStyle()
	styles.Blurred.Text = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	styles.Blurred.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	styles.Blurred.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color("25"))
	styles.Focused.EndOfBuffer = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	styles.Blurred.EndOfBuffer = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	ta.SetStyles(styles)
	t.ta = ta

	t.sp = spinner.New(spinner.WithSpinner(spinner.Dot))
	t.sp.Style = lipgloss.NewStyle().Foreground(ColorBrand)

	t.phase = phaseIdle
	t.activeTools = make(map[string]*toolEntry)
	t.toolStartTimes = make(map[string]time.Time)

	t.syncContent()
	t.layoutChat()
	t.syncContent()

	if t.pendingInput != "" {
		t.ta.SetValue(t.pendingInput)
		t.ta.CursorEnd()
		t.pendingInput = ""
	}

	return t.ta.Focus()
}

func (t *TUI) syncContent() {
	var sb strings.Builder

	for _, msg := range t.messages {
		switch msg.role {
		case "user":
			content, _ := msg.content.(string)
			sb.WriteString(styleUserLine.Render("  ▶ " + t.tr("tui.chat.you")))
			sb.WriteString("\n  " + content + "\n\n")
		case "assistant":
			content, _ := msg.content.(string)
			sb.WriteString(styleAgentLine.Render("  ● " + t.tr("tui.chat.suna")))
			sb.WriteString("\n" + indentLines(RenderMarkdown(content, t.width-6), "  ") + "\n")
		case "reasoning":
			content, _ := msg.content.(string)
			sb.WriteString(t.renderThinkingBox(content, false))
		case "tool":
			if te, ok := msg.content.(*toolEntry); ok {
				sb.WriteString(t.renderToolEntry(te))
			}
		case "error":
			content, _ := msg.content.(string)
			sb.WriteString(styleErrLine.Render("  ✗ " + content))
			sb.WriteString("\n\n")
		default:
			content, _ := msg.content.(string)
			sb.WriteString(styleSysLine.Render("  ◆ " + content))
			sb.WriteString("\n\n")
		}
	}

	if t.loading && t.phase == phaseFirstLLM && t.phaseStart.After(time.Time{}) {
		sb.WriteString("  " + t.sp.View())
		sb.WriteString("\n")
	}

	if t.loading && t.phaseStart.After(time.Time{}) && t.phase == phaseThinking {
		elapsed := time.Since(t.phaseStart)
		sb.WriteString(t.renderThinkingBox(fmt.Sprintf("%s %.1fs", t.i18n.T("status.thinking"), elapsed.Seconds()), true))
	}

	for _, te := range t.activeTools {
		if te.status == toolRunning {
			start, ok := t.toolStartTimes[te.id]
			if !ok {
				start = time.Now()
			}
			elapsed := time.Since(start)
			intent := te.intent
			if intent != "" {
				intent = styleToolDim.Render("(" + intent + ")")
			}
			sb.WriteString(fmt.Sprintf("  %s %s %s %.1fs\n", styleToolRun.Render("⋯"), styleToolPill.Render(te.name), intent, elapsed.Seconds()))
		}
	}

	t.vp.SetContent(sb.String())
	t.vp.GotoBottom()
}

func (t *TUI) renderThinkingBox(content string, running bool) string {
	width := max(24, min(t.width-6, 62))
	inner := width - 4
	title := " ◎ " + t.tr("tui.chat.thinking") + " "
	display := strings.TrimSpace(content)
	if !t.showToolDetail && !running {
		display = extractLastSentence(display)
		if display == "" {
			display = t.tr("tui.chat.thought_done")
		}
		display += "    [Ctrl+T " + t.tr("tui.key.detail") + "]"
	}
	lines := strings.Split(display, "\n")
	if t.showToolDetail && len(lines) > 15 {
		lines = append(lines[:15], "...")
	}
	var sb strings.Builder
	sb.WriteString("  " + styleDim.Render("┌─"+title+strings.Repeat("─", max(0, width-lipgloss.Width(title)-3))+"┐") + "\n")
	for _, line := range lines {
		line = truncateRunes(line, inner)
		sb.WriteString("  " + styleDim.Render("│ ") + line + strings.Repeat(" ", max(0, inner-lipgloss.Width(line))) + styleDim.Render(" │") + "\n")
	}
	sb.WriteString("  " + styleDim.Render("└"+strings.Repeat("─", width-2)+"┘") + "\n")
	return sb.String()
}

func (t *TUI) phaseLabel() string {
	switch t.phase {
	case phaseFirstLLM:
		return ""
	case phaseLLM:
		return t.i18n.T("status.waiting_llm")
	case phaseThinking:
		return t.i18n.T("status.thinking")
	case phaseTool:
		return t.i18n.T("status.exec_tool")
	default:
		return ""
	}
}

func (t *TUI) renderToolEntry(te *toolEntry) string {
	var statusIcon string
	var dur string
	if te.duration > 0 {
		dur = fmt.Sprintf(" %.1fs", te.duration.Seconds())
	}
	switch te.status {
	case toolDone:
		statusIcon = styleToolOk.Render("✓")
	case toolError:
		statusIcon = styleToolErr.Render("✗")
	default:
		statusIcon = styleToolDim.Render("·")
	}
	intent := styleToolDim.Render(te.intent)
	line := fmt.Sprintf("  %s %s%s %s", statusIcon, styleToolPill.Render(te.name), dur, intent)
	if t.showToolDetail && te.result != "" {
		resultLines := strings.Split(te.result, "\n")
		if len(resultLines) > 5 {
			resultLines = append(resultLines[:5], "...")
		}
		for _, l := range resultLines {
			line += "\n    " + styleToolDim.Render(l)
		}
	}
	return line + "\n"
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
		ks := m.String()
		switch {
		case ks == "ctrl+c":
			t.doQuit()
			return t, tea.Quit
		case ks == "?" || ks == "f1":
			t.showHelp = !t.showHelp
			return t, nil
		case ks == "enter":
			if len(t.cmdSuggestions) > 0 {
				t.acceptCommandSuggestion()
				return t, t.ta.Focus()
			}
			if !t.loading {
				return t, t.handleSend()
			}
			return t, nil
		case ks == "shift+enter", ks == "alt+enter":
			var cmd tea.Cmd
			t.ta, cmd = t.ta.Update(msg)
			t.layoutChat()
			return t, cmd
		case ks == "esc":
			if t.showHelp {
				t.showHelp = false
				return t, nil
			}
			if t.loading {
				t.ipcCli.Cancel()
				t.resetPhase()
				t.messages = append(t.messages, chatMsg{role: "system", content: t.i18n.T("status.cancelled")})
				t.syncContent()
				return t, t.ta.Focus()
			}
			if t.ta.Value() == "" {
				t.mode = "welcome"
				return t, nil
			}
			t.ta.Reset()
			return t, t.ta.Focus()
		case ks == "ctrl+n":
			if !t.loading {
				t.ipcCli.NewSession()
				t.messages = []chatMsg{}
				t.resetPhase()
				t.lastAssistantText = ""
				t.syncContent()
			}
			return t, nil
		case ks == "ctrl+o":
			t.mode = "config"
			t.configFromMode = "chat"
			t.configSetupMode = false
			t.configFormOpen = false
			return t, nil
		case ks == "ctrl+t":
			t.showToolDetail = !t.showToolDetail
			t.syncContent()
			return t, nil
		case ks == "pgup", ks == "ctrl+u":
			t.vp.HalfPageUp()
			return t, nil
		case ks == "pgdown", ks == "ctrl+d":
			t.vp.HalfPageDown()
			return t, nil
		case ks == "up" && len(t.cmdSuggestions) > 0:
			if t.cmdSuggestionIdx > 0 {
				t.cmdSuggestionIdx--
			}
			return t, nil
		case ks == "down" && len(t.cmdSuggestions) > 0:
			if t.cmdSuggestionIdx < len(t.cmdSuggestions)-1 {
				t.cmdSuggestionIdx++
			}
			return t, nil
		}

	case spinner.TickMsg:
		if t.loading {
			var cmd tea.Cmd
			t.sp, cmd = t.sp.Update(msg)
			t.syncContent()
			return t, cmd
		}
		return t, nil

	case ipcNotification:
		t.handleIPCNotification(m)
		t.syncContent()
		if t.loading {
			return t, func() tea.Msg { return t.sp.Tick() }
		}
		return t, nil

	case tea.MouseMsg:
		var cmd tea.Cmd
		t.vp, cmd = t.vp.Update(msg)
		return t, cmd
	}

	var cmd tea.Cmd
	t.ta, cmd = t.ta.Update(msg)

	val := t.ta.Value()
	if strings.HasPrefix(val, "/") && !strings.Contains(strings.TrimPrefix(val, "/"), " ") {
		t.updateCmdSuggestions(val)
	} else {
		t.cmdSuggestions = nil
		t.cmdSuggestionIdx = 0
	}
	t.layoutChat()

	return t, cmd
}

func (t *TUI) resetPhase() {
	t.loading = false
	t.phase = phaseIdle
	t.phaseStart = time.Time{}
	t.activeTools = make(map[string]*toolEntry)
	t.toolStartTimes = make(map[string]time.Time)
}

func (t *TUI) allCommands() []commandSpec {
	return []commandSpec{
		{"/new", "tui.command.new.desc"},
		{"/compact", "tui.command.compact.desc"},
		{"/config", "tui.command.config.desc"},
		{"/model", "tui.command.model.desc"},
		{"/memory search", "tui.command.memory.desc"},
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

func (t *TUI) acceptCommandSuggestion() {
	if len(t.cmdSuggestions) == 0 || t.cmdSuggestionIdx >= len(t.cmdSuggestions) {
		return
	}
	t.ta.SetValue(t.cmdSuggestions[t.cmdSuggestionIdx].cmd + " ")
	t.ta.CursorEnd()
	t.cmdSuggestions = nil
	t.cmdSuggestionIdx = 0
}

func (t *TUI) handleSend() tea.Cmd {
	input := strings.TrimSpace(t.ta.Value())
	t.ta.Reset()
	if input == "" {
		return t.ta.Focus()
	}
	t.messages = append(t.messages, chatMsg{role: "user", content: input})
	t.syncContent()

	if t.pendingAskID != "" {
		askID := t.pendingAskID
		t.pendingAskID = ""
		t.ipcCli.AskReply(askID, input)
		return t.ta.Focus()
	}

	if strings.HasPrefix(input, "/") {
		t.handleCommand(input)
		t.syncContent()
		return t.ta.Focus()
	}
	return t.runAgent(input)
}

func (t *TUI) layoutChat() {
	if t.width == 0 || t.height == 0 {
		return
	}
	inputH := t.ta.Height()
	if inputH < 1 {
		inputH = 1
	}
	suggestionH := 0
	if len(t.cmdSuggestions) > 0 {
		suggestionH = min(4, len(t.cmdSuggestions)) + 2
	}
	fixedH := 4 + inputH + suggestionH
	vpHeight := t.height - fixedH
	if vpHeight < 3 {
		vpHeight = 3
	}
	t.vp.SetWidth(t.width)
	t.vp.SetHeight(vpHeight)
	t.ta.SetWidth(t.width - 2)
}

func (t *TUI) viewChat() string {
	if t.width == 0 {
		return ""
	}

	t.layoutChat()

	var sb strings.Builder

	petState := petIdle
	if t.loading {
		if t.phase == phaseThinking {
			petState = petThinking
		} else {
			petState = petWorking
		}
	}
	miniPet := renderMiniPet(petState)
	topLeft := miniPet + " " + styleBrand.Render("suna")

	provider := t.providerName
	model := t.modelName
	if provider == "" {
		provider = "..."
	}
	if model == "" {
		model = "..."
	}
	conn := styleDim.Render("○")
	if t.ipcCli != nil && t.ipcCli.Connected() {
		conn = styleAgent.Render("●")
	}
	topRight := styleDim.Render(provider+"/"+model) + "  " + conn
	padW := t.width - lipgloss.Width(topLeft) - lipgloss.Width(topRight) - 1
	if padW < 1 {
		padW = 1
	}
	sb.WriteString(topLeft + strings.Repeat(" ", padW) + topRight)
	sb.WriteString("\n")
	sb.WriteString(styleDim.Render(strings.Repeat("─", t.width)))
	sb.WriteString("\n")

	content := t.vp.View()
	if t.showHelp {
		overlay := t.renderHelpOverlay(t.width)
		content = overlay + "\n" + content
	}
	sb.WriteString(content)

	sb.WriteString(styleDim.Render(strings.Repeat("─", t.width)))
	sb.WriteString("\n")

	sb.WriteString(t.ta.View())

	if len(t.cmdSuggestions) > 0 {
		sb.WriteString("\n")
		sb.WriteString(t.renderCommandSuggestions())
	}

	sb.WriteString("\n")
	sb.WriteString(t.renderChatStatusBar())
	sb.WriteString("\n")

	return sb.String()
}

func (t *TUI) renderChatStatusBar() string {
	var parts []string

	if t.sessionInputTok > 0 || t.sessionOutputTok > 0 {
		tokParts := []string{styleUser.Render("↑" + fmtTok(t.sessionInputTok))}
		if t.sessionCachedTok > 0 {
			tokParts = append(tokParts, styleDim.Render("⟳"+fmtTok(t.sessionCachedTok)))
		}
		tokParts = append(tokParts, styleAgent.Render("↓"+fmtTok(t.sessionOutputTok)))
		parts = append(parts, joinNonEmpty(tokParts, " "))
	}
	if t.lastOutputTok > 0 && t.lastDuration.Seconds() > 0 {
		speed := float64(t.lastOutputTok) / t.lastDuration.Seconds()
		parts = append(parts, fmt.Sprintf("%.0ft/s", speed))
	}
	if t.ipcCli != nil && t.ipcCli.Connected() {
		parts = append(parts, styleAgent.Render("●"))
	} else {
		parts = append(parts, styleDim.Render("○"))
	}

	if len(parts) == 0 {
		return styleDim.Render(" ○")
	}
	return styleDim.Render(" " + joinNonEmpty(parts, " · "))
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
