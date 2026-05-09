package tui

import "charm.land/bubbles/v2/key"

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
