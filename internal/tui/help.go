package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
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
		case "ctrl+u", "pgup":
			t.helpVP.HalfPageUp()
			return t, nil
		case "ctrl+d", "pgdown":
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
	h := help.New()
	short := h.ShortHelpView(t.keys().ShortHelp())
	commands := []string{t.tr("tui.help.commands"), t.commandLine("/new", "tui.command.new.desc"), t.commandLine("/model <name>", "tui.command.model.desc"), t.commandLine("/compact", "tui.command.compact.desc"), t.commandLine("/memory search <q>", "tui.command.memory.desc"), t.commandLine("/help", "tui.command.help.desc")}
	body := short + "\n\n" + strings.Join(commands, "\n")
	w := min(max(44, lipgloss.Width(short)+4), max(20, width-8))
	return boxStyle.Width(w).Padding(1, 2).Render(body)
}

func (t *TUI) renderHelpContent() string {
	h := help.New()
	h.ShowAll = true
	sections := []string{
		styleHL.Render(t.tr("tui.help.shortcuts")),
		h.FullHelpView(t.keys().FullHelp()),
		"",
		styleHL.Render(t.tr("tui.help.commands")),
		t.commandLine("/new", "tui.command.new.desc"),
		t.commandLine("/model <name>", "tui.command.model.desc"),
		t.commandLine("/compact", "tui.command.compact.desc"),
		t.commandLine("/memory search <q>", "tui.command.memory.desc"),
		t.commandLine("/help", "tui.command.help.desc"),
	}
	return "\n" + strings.Join(sections, "\n") + "\n"
}

func (t *TUI) commandLine(cmd, descKey string) string {
	return fmt.Sprintf("  %-18s %s", styleBrand.Render(cmd), t.tr(descKey))
}
