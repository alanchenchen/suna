package welcome

import (
	"fmt"
	"io"
	"strings"

	"github.com/alanchenchen/suna/internal/tui/components/selection"
	textutil "github.com/alanchenchen/suna/internal/tui/components/text"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type Action int

const (
	ActionNone Action = iota
	ActionNew
	ActionConfirmNew
	ActionCancelNew
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
	// CWD 显示在活跃会话选择器的标题下方。
	CWD string
	// DetailKey 是双行菜单使用的可选本地化次行。
	DetailKey string
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
	joinPicker := false
	for _, item := range items {
		listItems = append(listItems, item)
		joinPicker = joinPicker || item.Action == ActionJoin
	}
	w := max(1, welcomeContentWidth(width)-6)
	if joinPicker {
		// Welcome 外框在窄终端会与视口一起收缩，菜单必须使用同一套宽度计算。
		w = max(1, w)
	}
	itemHeight := 1
	if joinPicker {
		itemHeight = 2
	}
	h := max(3, len(items)*itemHeight)
	m.menu = list.New(listItems, delegate{m: m, twoLine: joinPicker}, w, h)
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

type delegate struct {
	m       *Model
	twoLine bool
}

func (d delegate) Height() int {
	if d.twoLine {
		return 2
	}
	return 1
}
func (d delegate) Spacing() int                        { return 0 }
func (d delegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }
func (d delegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	wi, ok := item.(Item)
	if !ok {
		return
	}
	cursor := selection.Rail(index == m.Index(), 0, d.m.deps.Styles.Cursor)
	st := lipgloss.NewStyle()
	if wi.Disabled {
		st = d.m.deps.Styles.Dim
	}
	if index == m.Index() && !wi.Disabled {
		st = d.m.deps.Styles.Brand
	}

	if wi.Action == ActionJoin {
		contentWidth := max(1, m.Width()-lipgloss.Width(cursor))
		title := textutil.TruncateRunes(wi.Key, contentWidth)
		cwd := textutil.TruncateRunes(wi.CWD, contentWidth)
		indent := strings.Repeat(" ", lipgloss.Width(cursor))
		fmt.Fprint(w, cursor+st.Render(title)+"\n"+indent+d.m.deps.Styles.Dim.Render(cwd))
		return
	}

	line := cursor + st.Render(d.m.deps.Tr(wi.LabelKey))
	if wi.Key != "" {
		line += d.m.deps.Styles.Dim.Render("  [" + wi.Key + "]")
	}
	if d.twoLine {
		indent := strings.Repeat(" ", lipgloss.Width(cursor))
		fmt.Fprint(w, line+"\n"+indent+d.m.deps.Styles.Dim.Render(d.m.deps.Tr(wi.DetailKey)))
		return
	}
	fmt.Fprint(w, line)
}

func welcomeContentWidth(viewportWidth int) int {
	preferred := min(max(54, viewportWidth-14), 84)
	// Border and horizontal padding consume six cells outside Style.Width.
	return min(preferred, max(1, viewportWidth-6))
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
