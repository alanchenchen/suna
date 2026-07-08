package welcome

import (
	"fmt"
	"io"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type Action int

const (
	ActionNone Action = iota
	ActionNew
	ActionResume
	ActionJoin
	ActionJoinPicker
	ActionBack
	ActionConfig
	ActionHelp
)

type Item struct {
	LabelKey  string
	Key       string
	Action    Action
	Disabled  bool
	SessionID string
}

func (i Item) FilterValue() string { return i.LabelKey }

type Styles struct {
	Cursor lipgloss.Style
	Dim    lipgloss.Style
	HL     lipgloss.Style
	Brand  lipgloss.Style
}

type Deps struct {
	Tr     func(string) string
	Styles Styles
}

type Model struct {
	deps        Deps
	cursor      int
	initialized bool
	menu        list.Model
	selected    Item
}

func New(deps Deps) Model {
	return Model{deps: deps}
}

func (m *Model) SetItems(items []Item, width int) {
	listItems := make([]list.Item, 0, len(items))
	for _, item := range items {
		listItems = append(listItems, item)
	}
	w := max(20, min(max(54, width-14), 84)-6)
	h := max(3, len(items))
	m.menu = list.New(listItems, delegate{m: m}, w, h)
	m.initialized = true
	m.menu.SetShowTitle(false)
	m.menu.SetShowStatusBar(false)
	m.menu.SetShowPagination(false)
	m.menu.SetFilteringEnabled(false)
	m.menu.SetShowHelp(false)
	if m.cursor >= len(items) {
		m.cursor = max(0, len(items)-1)
	}
	m.menu.Select(m.cursor)
}

func (m *Model) HasItems() bool {
	return m.initialized && len(m.menu.Items()) > 0
}

func (m *Model) View() string {
	return m.menu.View()
}

func (m *Model) UpdateKey(key string, items []Item) (Action, bool) {
	switch key {
	case "up":
		if m.cursor > 0 {
			m.cursor--
		}
		m.menu.Select(m.cursor)
		return ActionNone, true
	case "down":
		if m.cursor < len(items)-1 {
			m.cursor++
		}
		m.menu.Select(m.cursor)
		return ActionNone, true
	case "enter":
		if m.cursor >= 0 && m.cursor < len(items) && !items[m.cursor].Disabled {
			m.selected = items[m.cursor]
			return items[m.cursor].Action, true
		}
		return ActionNone, true
	default:
		return ActionNone, false
	}
}

func (m *Model) SelectedItem() Item { return m.selected }

type delegate struct{ m *Model }

func (d delegate) Height() int                         { return 1 }
func (d delegate) Spacing() int                        { return 0 }
func (d delegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }
func (d delegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	wi, ok := item.(Item)
	if !ok {
		return
	}
	cursor := "  "
	st := lipgloss.NewStyle()
	if wi.Disabled {
		st = d.m.deps.Styles.Dim
	}
	if index == m.Index() {
		cursor = d.m.deps.Styles.Cursor.Render("▶ ")
		if !wi.Disabled {
			st = d.m.deps.Styles.Brand
		}
	}
	line := cursor + st.Render(d.m.deps.Tr(wi.LabelKey))
	if wi.Key != "" {
		line += d.m.deps.Styles.Dim.Render("  [" + wi.Key + "]")
	}
	fmt.Fprint(w, line)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
