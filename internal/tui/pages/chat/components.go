package chat

import (
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	"github.com/alanchenchen/suna/internal/tui/components/toolview"
)

type ComponentDeps struct {
	Placeholder    string
	TextareaStyles textarea.Styles
	SpinnerStyle   lipgloss.Style
}

// InitComponents 初始化 Chat 页面拥有的 Bubble components；root 只注入样式和文案。
func (m *Model) InitComponents(deps ComponentDeps) {
	m.Viewport = viewport.New()
	m.Viewport.SoftWrap = false
	m.Viewport.MouseWheelEnabled = true
	m.Viewport.MouseWheelDelta = MouseWheelDelta

	ta := textarea.New()
	ta.Prompt = ""
	ta.Placeholder = deps.Placeholder
	ta.DynamicHeight = true
	ta.MaxHeight = InputMaxHeight
	ta.MinHeight = 1
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetStyles(deps.TextareaStyles)
	m.Textarea = ta

	m.Spinner = spinner.New(spinner.WithSpinner(spinner.Dot))
	m.Spinner.Style = deps.SpinnerStyle

	m.Phase = PhaseIdle
	m.InputHistoryIndex = -1
	m.ActiveTools = make(map[string]*toolview.Entry)
	m.ToolStartTimes = make(map[string]time.Time)
	m.CurrentToolBlock = nil
	m.SelectedToolID = ""
	m.SubtaskCursor = 0
	m.SubtaskCursorUserSet = false
	m.SubtaskToolCursor = 0
	m.SubtaskToolCursorUserSet = false
	m.SubtaskToolDetailExpanded = false
	m.SubtaskToolDetailScroll = 0
}

func (m *Model) RestorePendingInput() {
	if m.PendingInput == "" {
		return
	}
	m.Textarea.SetValue(m.PendingInput)
	m.Textarea.CursorEnd()
	m.PendingInput = ""
}
