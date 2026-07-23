package chat

import "charm.land/bubbles/v2/key"

// KeyMap 集中声明 Chat 中会接管输入的交互快捷键，渲染提示和按键匹配共用同一来源。
type KeyMap struct {
	Quit                    key.Binding
	ToggleTerminalSelection key.Binding
	ExitTerminalSelection   key.Binding
	GuardPrevious           key.Binding
	GuardNext               key.Binding
	GuardConfirm            key.Binding
	GuardReject             key.Binding
	GuardScrollUp           key.Binding
	GuardScrollDown         key.Binding
}

var DefaultKeyMap = KeyMap{
	Quit: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "quit"),
	),
	ToggleTerminalSelection: key.NewBinding(
		key.WithKeys("ctrl+s"),
		key.WithHelp("ctrl+s", "select terminal text"),
	),
	ExitTerminalSelection: key.NewBinding(
		key.WithKeys("esc", "ctrl+s"),
		key.WithHelp("esc/ctrl+s", "return to chat"),
	),
	GuardPrevious: key.NewBinding(
		key.WithKeys("left"),
		key.WithHelp("←", "choose"),
	),
	GuardNext: key.NewBinding(
		key.WithKeys("right"),
		key.WithHelp("→", "choose"),
	),
	GuardConfirm: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "confirm selected"),
	),
	GuardReject: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "reject"),
	),
	GuardScrollUp: key.NewBinding(
		key.WithKeys("up", "pgup"),
		key.WithHelp("↑/pgup", "scroll"),
	),
	GuardScrollDown: key.NewBinding(
		key.WithKeys("down", "pgdown"),
		key.WithHelp("↓/pgdown", "scroll"),
	),
}
