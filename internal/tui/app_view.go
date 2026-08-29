package tui

import (
	tea "charm.land/bubbletea/v2"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
)

func (t *TUI) View() tea.View {
	v := tea.NewView("")
	v.WindowTitle = t.windowTitle()
	v.AltScreen = true
	// 鼠标模式固定为 cell motion：内置拖选复制（选区状态机）依赖按下后上报 motion，
	// 不再切换到 MouseModeNone（原 ctrl+s 终端原生选择模式已移除）。
	v.MouseMode = tea.MouseModeCellMotion
	if !t.ready {
		v.SetContent(t.viewWelcome())
		return v
	}
	switch t.mode {
	case uipage.Welcome:
		v.SetContent(t.viewWelcome())
	case uipage.Config:
		v.SetContent(t.viewConfig())
	case uipage.Chat:
		v.SetContent(t.viewChat())
	case uipage.Help:
		v.SetContent(t.viewHelp())
	}
	return v
}
