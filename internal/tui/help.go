package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Help 页面和快捷键定义集中在一起。
// 这样全局帮助、Chat overlay 帮助与快捷键绑定保持单一来源，避免页面间文案漂移。
type tuiKeyMap struct {
	Send       key.Binding
	Newline    key.Binding
	Back       key.Binding
	NewSession key.Binding
	Detail     key.Binding
	Config     key.Binding
	ScrollUp   key.Binding
	ScrollDown key.Binding
	Help       key.Binding
	Quit       key.Binding
}

func (t *TUI) keys() tuiKeyMap {
	return tuiKeyMap{
		Send:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", t.tr("tui.key.send"))),
		Newline:    key.NewBinding(key.WithKeys("shift+enter", "alt+enter"), key.WithHelp("shift+enter", t.tr("tui.key.newline"))),
		Back:       key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", t.tr("tui.key.back"))),
		NewSession: key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("ctrl+n", t.tr("tui.key.new_session"))),
		Detail:     key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", t.tr("tui.key.detail"))),
		Config:     key.NewBinding(key.WithKeys("ctrl+o"), key.WithHelp("ctrl+o", t.tr("tui.key.config"))),
		ScrollUp:   key.NewBinding(key.WithKeys("ctrl+u", "pgup"), key.WithHelp("ctrl+u", t.tr("tui.key.scroll_up"))),
		ScrollDown: key.NewBinding(key.WithKeys("ctrl+d", "pgdown"), key.WithHelp("ctrl+d", t.tr("tui.key.scroll_down"))),
		Help:       key.NewBinding(key.WithKeys("?", "f1"), key.WithHelp("?", t.tr("tui.key.help"))),
		Quit:       key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", t.tr("tui.key.quit"))),
	}
}

func (k tuiKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Send, k.Back, k.NewSession, k.Detail, k.Config, k.Help}
}

func (k tuiKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Send, k.Newline, k.Back, k.NewSession}, {k.Detail, k.Config, k.ScrollUp, k.ScrollDown, k.Help, k.Quit}}
}

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
	commands := []string{t.tr("tui.help.commands"), t.commandLine("/new", "tui.command.new.desc"), t.commandLine("/model <name>", "tui.command.model.desc"), t.commandLine("/compact", "tui.command.compact.desc"), t.commandLine("/config", "tui.command.config.desc"), t.commandLine("/memory search <q>", "tui.command.memory.desc"), t.commandLine("/help", "tui.command.help.desc")}
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
		t.commandLine("/config", "tui.command.config.desc"),
		t.commandLine("/memory search <q>", "tui.command.memory.desc"),
		t.commandLine("/help", "tui.command.help.desc"),
	}
	return "\n" + strings.Join(sections, "\n") + "\n"
}

func (t *TUI) commandLine(cmd, descKey string) string {
	return fmt.Sprintf("  %-18s %s", styleBrand.Render(cmd), t.tr(descKey))
}
