package tui

import (
	tea "charm.land/bubbletea/v2"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
)

func (t *TUI) View() tea.View {
	v := tea.NewView("")
	v.WindowTitle = t.windowTitleWithFrame(t.liveSpinnerFramePlain())
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
		// 终端光标精确跟随 textarea 光标：IME 组合文本（拼音 preedit）由终端绘制在
		// 终端光标位置，锚定到 textarea 光标后组合文本始终显示在输入框内，
		// 不会跟随 renderer 增量渲染的光标移动残留到 pet 等其他区域。
		if x, y, ok := t.textareaCursorScreenPos(); ok {
			// CursorBar 细光标 + Blink=true（闪烁）：终端光标与 textarea 虚拟光标（反色字符）
			// 精确重叠，闪烁由终端光标提供，视觉上就是一个自然闪烁的光标。
			cur := tea.NewCursor(x, y)
			cur.Shape = tea.CursorBar
			cur.Blink = true
			v.Cursor = cur
		}
	case uipage.Help:
		v.SetContent(t.viewHelp())
	}
	return v
}
