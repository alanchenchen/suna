package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func (t *TUI) initHelpPage() {
	t.helpVP = viewport.New()
	t.helpVP.SoftWrap = false
	t.helpVP.MouseWheelEnabled = true
	t.layoutHelp()
	t.helpVP.SetContent(t.renderHelpContent())
}

func (t *TUI) updateHelp(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		t.width, t.height, t.ready = m.Width, m.Height, true
		t.layoutHelp()
		t.helpVP.SetContent(t.renderHelpContent())
		return t, nil
	case tea.KeyPressMsg:
		switch m.String() {
		case "ctrl+c":
			t.doQuit()
			return t, tea.Quit
		case "esc":
			t.mode = t.prevMode
			if t.mode == "" {
				t.mode = "welcome"
			}
			return t, nil
		case "pgup":
			t.helpVP.HalfPageUp()
			return t, nil
		case "pgdown":
			t.helpVP.HalfPageDown()
			return t, nil
		}
	case tea.MouseMsg:
		var cmd tea.Cmd
		t.helpVP, cmd = t.helpVP.Update(msg)
		return t, cmd
	}
	return t, nil
}

func (t *TUI) layoutHelp() {
	if t.width <= 0 || t.height <= 0 {
		return
	}
	t.helpVP.SetWidth(t.width)
	t.helpVP.SetHeight(max(3, t.height-3))
}

func (t *TUI) viewHelp() string {
	if t.helpVP.Width() == 0 {
		t.initHelpPage()
	}
	header := renderHeader(t.tr("tui.help.title"), "[Esc] "+t.tr("tui.key.back"), t.width)
	return header + "\n" + t.helpVP.View()
}

func (t *TUI) renderHelpOverlay(width int) string {
	common := []string{
		styleHL.Render(t.tr("tui.help.common")),
		"  " + styleBrand.Render("Enter") + styleDim.Render("  ") + t.tr("tui.key.send"),
		"  " + styleBrand.Render("Esc") + styleDim.Render("    ") + t.tr("tui.key.back"),
		"  " + styleBrand.Render("?") + styleDim.Render("      ") + t.tr("tui.key.help"),
	}
	commands := []string{
		styleHL.Render(t.tr("tui.help.commands")),
		t.commandLine("/new", "tui.command.new.desc"),
		t.commandLine("/model", "tui.command.model.desc"),
		t.commandLine("/compact", "tui.command.compact.desc"),
		t.commandLine("/config", "tui.command.config.desc"),
	}
	more := []string{
		styleHL.Render(t.tr("tui.help.more")),
		"  " + styleBrand.Render("Shift+Enter") + styleDim.Render(" ") + t.tr("tui.key.newline"),
		"  " + styleBrand.Render("Ctrl+Y") + styleDim.Render(" ") + t.tr("tui.key.copy_mode"),
		"  " + styleBrand.Render("Ctrl+T") + styleDim.Render(" ") + t.tr("tui.key.detail"),
		"  " + styleBrand.Render("PgUp/PgDn") + styleDim.Render(" ") + t.tr("tui.key.scroll_up") + "/" + t.tr("tui.key.scroll_down"),
	}
	body := strings.Join(common, "\n") + "\n\n" + strings.Join(commands, "\n") + "\n\n" + strings.Join(more, "\n")
	w := min(max(44, maxLineWidth(body)+4), max(20, width-8))
	return boxStyle.Width(w).Padding(1, 2).Render(body)
}

func maxLineWidth(s string) int {
	maxWidth := 0
	for _, line := range strings.Split(s, "\n") {
		maxWidth = max(maxWidth, lipgloss.Width(line))
	}
	return maxWidth
}

func (t *TUI) renderHelpContent() string {
	sections := []string{
		styleHL.Render(t.tr("tui.help.chat_basics")),
		t.helpLine("Enter", "tui.help.chat_send"),
		t.helpLine("Shift+Enter", "tui.help.chat_newline"),
		t.helpLine("Esc", "tui.help.chat_back"),
		t.helpLine("PgUp/PgDn", "tui.help.chat_scroll"),
		"",
		styleHL.Render(t.tr("tui.help.commands")),
		t.commandLine("/new", "tui.command.new.desc"),
		t.commandLine("/model [name]", "tui.command.model.desc"),
		t.commandLine("/compact", "tui.command.compact.desc"),
		t.commandLine("/config", "tui.command.config.desc"),
		t.commandLine("/memory search <q>", "tui.command.memory.desc"),
		"",
		styleHL.Render(t.tr("tui.help.copy_text")),
		t.helpLine("Ctrl+Y", "tui.help.copy_mode"),
		t.helpLine("Esc", "tui.help.copy_exit"),
		"",
		styleHL.Render(t.tr("tui.help.config")),
		t.helpText("tui.help.config_menu"),
		t.helpText("tui.help.config_space"),
		"",
		styleHL.Render(t.tr("tui.help.troubleshooting")),
		t.helpText("tui.help.slash_unknown"),
		t.helpText("tui.help.cancel"),
	}
	return "\n" + strings.Join(sections, "\n") + "\n"
}

func (t *TUI) helpLine(keyText, descKey string) string {
	return fmt.Sprintf("  %-14s %s", styleBrand.Render(keyText), t.tr(descKey))
}

func (t *TUI) helpText(key string) string {
	return "  " + t.tr(key)
}

func (t *TUI) commandLine(cmd, descKey string) string {
	return fmt.Sprintf("  %-18s %s", styleBrand.Render(cmd), t.tr(descKey))
}
