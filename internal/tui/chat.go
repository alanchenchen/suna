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
	styleUserLine  = lipgloss.NewStyle().Foreground(ColorUser).Bold(true)
	styleAgentLine = lipgloss.NewStyle().Foreground(ColorAgent).Bold(true)
	styleToolPill  = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(ColorTool).Padding(0, 1).Bold(true)
	styleToolOk    = lipgloss.NewStyle().Foreground(ColorAgent).Bold(true)
	styleToolErr   = lipgloss.NewStyle().Foreground(ColorError).Bold(true)
	styleToolRun   = lipgloss.NewStyle().Foreground(ColorBrand).Bold(true)
	styleToolDim   = lipgloss.NewStyle().Foreground(ColorDim)
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
	id       string
	name     string
	rawName  string
	intent   string
	params   string
	summary  string
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
	ta.SetStyles(textareaStyles())
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
	followBottom := t.vp.AtBottom()
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

	for _, msg := range t.messages {
		switch msg.role {
		case "user":
			content, _ := msg.content.(string)
			sb.WriteString("\n" + renderInlineUserMessage(content, max(20, t.width-8)) + "\n")
			inSunaBlock = false
		case "assistant":
			content, _ := msg.content.(string)
			renderSunaHeader()
			sb.WriteString(indentLines(RenderMarkdown(content, t.width-6), "  ") + "\n")
		case "reasoning":
			content, _ := msg.content.(string)
			renderSunaHeader()
			sb.WriteString(t.renderThinkingBox(content, false))
		case "tool":
			if te, ok := msg.content.(*toolEntry); ok {
				renderSunaHeader()
				sb.WriteString(t.renderToolEntry(te))
			}
		case "error":
			content, _ := msg.content.(string)
			sb.WriteString("\n" + styleErrLine.Render("  ✗ "+content) + "\n")
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
		sb.WriteString(styleDim.Render("  "+t.tr("tui.ask.help")) + "\n\n")
	}
	if t.modelPickerOpen {
		sb.WriteString(t.renderModelPicker())
	}

	if t.loading && t.phaseStart.After(time.Time{}) {
		renderSunaHeader()
		sb.WriteString(t.renderCurrentStatusLine())
	}

	t.vp.SetContent(sb.String())
	if followBottom {
		t.vp.GotoBottom()
	}
}

func (t *TUI) renderThinkingBox(content string, running bool) string {
	width := max(24, min(t.width-8, 62))
	inner := width - 4
	title := " ◎ " + t.tr("tui.chat.thinking") + " "
	if running {
		elapsed := 0.0
		if !t.phaseStart.IsZero() {
			elapsed = time.Since(t.phaseStart).Seconds()
		}
		title = fmt.Sprintf(" ◎ %s %s %.1fs ", t.tr("tui.chat.thinking"), t.sp.View(), elapsed)
	}
	display := strings.TrimSpace(content)
	if running && display == "" {
		display = t.tr("status.thinking")
	}
	if !t.showToolDetail && !running {
		display = extractLastSentence(display)
		if display == "" {
			display = t.tr("tui.chat.thought_done")
		}
		display += "    [Ctrl+T " + t.tr("tui.key.detail") + "]"
	}
	if t.showToolDetail {
		display = RenderMarkdown(strings.TrimSpace(content), inner)
	}
	lines := strings.Split(strings.TrimRight(display, "\n"), "\n")
	if running && !t.showToolDetail && len(lines) > 8 {
		lines = append([]string{"..."}, lines[len(lines)-8:]...)
	}
	if t.showToolDetail && len(lines) > 15 {
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
	if te, ok := t.currentRunningTool(); ok {
		return t.tr("status.exec_tool") + " " + plainToolCallLabel(te)
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
	default:
		return ""
	}
}

func (t *TUI) currentRunningTool() (*toolEntry, bool) {
	var selected *toolEntry
	var selectedStart time.Time
	for _, te := range t.activeTools {
		if te.status != toolRunning {
			continue
		}
		start := t.toolStartTimes[te.id]
		if selected == nil || start.Before(selectedStart) || (start.Equal(selectedStart) && te.name < selected.name) {
			selected = te
			selectedStart = start
		}
	}
	if selected == nil {
		return nil, false
	}
	return selected, true
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
	line := fmt.Sprintf("    %s %s%s", statusIcon, styleToolCallLabel(te), dur)
	if t.showToolDetail {
		if strings.TrimSpace(te.intent) != "" {
			line += "\n      " + styleDim.Render(t.tr("tui.tool.intent"))
			for _, wrapped := range wrapLine(te.intent, max(20, t.width-10)) {
				line += "\n      " + styleToolDim.Render(wrapped)
			}
		}
		if te.params != "" {
			line += "\n      " + styleDim.Render(t.tr("tui.tool.params"))
			for _, l := range strings.Split(te.params, "\n") {
				for _, wrapped := range wrapLine(l, max(20, t.width-10)) {
					line += "\n      " + styleToolDim.Render(wrapped)
				}
			}
		}
		if te.result != "" {
			line += "\n      " + styleDim.Render(t.tr("tui.tool.result"))
			resultLines := strings.Split(te.result, "\n")
			if len(resultLines) > 10 {
				resultLines = append(resultLines[:10], "...")
			}
			for _, l := range resultLines {
				for _, wrapped := range wrapLine(l, max(20, t.width-10)) {
					line += "\n      " + styleToolDim.Render(wrapped)
				}
			}
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
		if t.pendingGuard != nil {
			return t.updateGuardConfirm(ks)
		}
		if t.modelPickerOpen {
			return t.updateModelPicker(ks)
		}
		switch {
		case ks == "ctrl+c":
			t.doQuit()
			return t, tea.Quit
		case ks == "?" || ks == "f1":
			t.showHelp = !t.showHelp
			return t, nil
		case ks == "enter":
			if len(t.cmdSuggestions) > 0 {
				cmd := t.acceptCommandSuggestion()
				if cmd != nil {
					return t, cmd
				}
				return t, t.ta.Focus()
			}
			if t.pendingAskID != "" && len(t.pendingAskOptions) > 0 && t.ta.Value() == "" {
				idx := t.pendingAskCursor
				if idx >= 0 && idx < len(t.pendingAskOptions) {
					answer := t.pendingAskOptions[idx]
					askID := t.pendingAskID
					t.pendingAskID = ""
					t.pendingAskOptions = nil
					t.messages = append(t.messages, chatMsg{role: "user", content: answer})
					t.startLLMWait()
					t.syncContent()
					return t, tea.Batch(t.askReplyCmd(askID, answer), t.sp.Tick)
				}
			}
			if !t.loading {
				return t, t.handleSend()
			}
			return t, nil
		case ks == "shift+enter", ks == "alt+enter":
			t.ta.InsertString("\n")
			t.layoutChat()
			return t, nil
		case ks == "esc":
			if t.showHelp {
				t.showHelp = false
				return t, nil
			}
			if t.loading {
				t.resetPhase()
				t.messages = append(t.messages, chatMsg{role: "system", content: t.i18n.T("status.cancelled")})
				t.syncContent()
				return t, tea.Batch(t.cancelCmd(), t.ta.Focus())
			}
			if t.ta.Value() == "" {
				t.mode = "welcome"
				t.refreshDaemonStatus()
				t.initWelcomeList()
				return t, nil
			}
			t.ta.Reset()
			return t, t.ta.Focus()
		case ks == "ctrl+t":
			t.showToolDetail = !t.showToolDetail
			t.syncContent()
			return t, nil
		case ks == "pgup":
			t.vp.HalfPageUp()
			return t, nil
		case ks == "pgdown":
			t.vp.HalfPageDown()
			return t, nil
		case ks == "up":
			if len(t.cmdSuggestions) > 0 {
				if t.cmdSuggestionIdx > 0 {
					t.cmdSuggestionIdx--
				}
			} else if t.pendingAskID != "" && len(t.pendingAskOptions) > 0 {
				if t.pendingAskCursor > 0 {
					t.pendingAskCursor--
				}
				t.syncContent()
			}
			return t, nil
		case ks == "down":
			if len(t.cmdSuggestions) > 0 {
				if t.cmdSuggestionIdx < len(t.cmdSuggestions)-1 {
					t.cmdSuggestionIdx++
				}
			} else if t.pendingAskID != "" && len(t.pendingAskOptions) > 0 {
				if t.pendingAskCursor < len(t.pendingAskOptions)-1 {
					t.pendingAskCursor++
				}
				t.syncContent()
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

func (t *TUI) updateGuardConfirm(ks string) (tea.Model, tea.Cmd) {
	switch ks {
	case "ctrl+c":
		t.doQuit()
		return t, tea.Quit
	case "left", "h", "up", "k", "tab", "shift+tab":
		if t.guardCursor == 0 {
			t.guardCursor = 1
		} else {
			t.guardCursor = 0
		}
		t.syncContent()
		return t, nil
	case "right", "l", "down", "j":
		if t.guardCursor == 0 {
			t.guardCursor = 1
		} else {
			t.guardCursor = 0
		}
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
	if decision == "reject" {
		t.messages = append(t.messages, chatMsg{role: "system", content: t.tr("tui.guard.rejected")})
	}
	t.pendingGuard = nil
	t.guardCursor = 0
	t.loading = true
	t.phase = phaseTool
	t.phaseStart = time.Now()
	return t.guardReplyCmd(id, decision)
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
		options := t.pendingAskOptions
		t.pendingAskOptions = nil
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
		return t.ta.Focus()
	}
	return t.runAgent(input)
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
