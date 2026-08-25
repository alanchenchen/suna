package help

import (
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

type Action int

const (
	ActionNone Action = iota
	ActionBack
	ActionQuit
)

type Model struct {
	vp viewport.Model
}

func New() Model {
	vp := viewport.New()
	vp.SoftWrap = false
	vp.MouseWheelEnabled = true
	return Model{vp: vp}
}

func (m *Model) SetSize(width, height int) {
	m.ensure()
	m.vp.SetWidth(width)
	m.vp.SetHeight(max(3, height-3))
}

func (m *Model) SetContent(content string) {
	m.ensure()
	m.vp.SetContent(content)
}

func (m *Model) Initialized() bool {
	return m.vp.Width() > 0
}

func (m *Model) View() string {
	m.ensure()
	return m.vp.View()
}

func (m *Model) Update(msg tea.Msg) (Action, tea.Cmd) {
	m.ensure()
	switch v := msg.(type) {
	case tea.KeyPressMsg:
		switch v.String() {
		case "ctrl+c":
			return ActionQuit, tea.Quit
		case "esc":
			return ActionBack, nil
		case "pgup":
			m.vp.HalfPageUp()
			return ActionNone, nil
		case "pgdown":
			m.vp.HalfPageDown()
			return ActionNone, nil
		}
	case tea.MouseMsg:
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return ActionNone, cmd
	}
	return ActionNone, nil
}

func (m *Model) ensure() {
	if m.vp.Width() != 0 || m.vp.Height() != 0 {
		return
	}
	vp := viewport.New()
	vp.SoftWrap = false
	vp.MouseWheelEnabled = true
	m.vp = vp
}
