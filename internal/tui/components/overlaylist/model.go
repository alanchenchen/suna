// Package overlaylist 为 TUI 浮层提供 Bubbles list 的最小封装。
package overlaylist

import (
	"fmt"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

// Item 是拥有稳定业务标识的列表项。列表刷新后用 Key 恢复选择，避免按旧索引误操作。
type Item interface {
	list.Item
	Key() string
}

// Message 包装由某个 list 产生的异步消息。Bubbles 的 FilterMatchesMsg 没有来源字段，
// 因此必须在命令边界保留 owner，不能把结果广播给当前任意浮层。
type Message struct {
	Owner string
	Inner tea.Msg
}

// BatchMessage 延续 Bubbles 的批量命令语义，同时保持每个后续消息的 owner。
type BatchMessage struct {
	Commands []tea.Cmd
}

// Model 以 Bubbles list.Model 作为唯一的导航、筛选和渲染状态来源。
type Model struct {
	owner       string
	title       string
	titleSuffix string
	list        list.Model
}

// New 创建一个原生 Bubbles 列表。调用方可通过 List 配置原生样式、标题和帮助。
func New(owner string, items []Item, delegate list.ItemDelegate, width, height int) Model {
	m := Model{
		owner: owner,
		list:  list.New(asItems(items), delegate, max(1, width), max(1, height)),
	}
	// Suna 自己处理关闭浮层，不能让 list 的 q/esc 退出整个程序。
	m.list.DisableQuitKeybindings()
	return m
}

// Owner 返回该列表的稳定归属标识。
func (m Model) Owner() string { return m.owner }

// Initialized 表示原生列表已经通过 New 创建，可以安全接收项目更新。
// daemon 的初始通知可能早于 Chat 组件初始化，因此调用方需要先保存原始数据。
func (m Model) Initialized() bool { return m.owner != "" }

// List 返回官方列表，以便页面使用其原生配置和 View。
func (m *Model) List() *list.Model { return &m.list }

// Update 将消息交给官方列表，并为其异步后续消息保留 owner。
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	m.updateTitle()
	return Wrap(m.owner, cmd)
}

// Reset 清空项目以及与当前页面绑定的选择、分页和筛选状态。列表实例会跨会话
// 复用，因此只调用 SetItems(nil) 会让下一个会话继承旧查询，导致新数据被错误隐藏。
func (m *Model) Reset() {
	if !m.Initialized() {
		return
	}
	m.list.ResetFilter()
	m.list.SetItems(nil)
	m.list.ResetSelected()
	m.updateTitle()
}

// SetItems 刷新项目并按稳定 Key 恢复选中项。筛选状态下同步重建结果，
// 避免配置刷新依赖一条脱离 owner 的异步消息。
func (m *Model) SetItems(items []Item) {
	selectedKey := m.SelectedKey()
	filterValue := m.list.FilterValue()
	wasFiltering := m.list.FilterState() == list.Filtering
	m.list.SetItems(asItems(items))
	if m.list.FilterState() != list.Unfiltered {
		m.list.SetFilterText(filterValue)
		if wasFiltering {
			// 刷新数据不能让正在输入的实时筛选退化为“已应用”状态。
			m.list.SetFilterState(list.Filtering)
		}
	}
	m.updateTitle()
	if !m.SelectKey(selectedKey) {
		m.list.ResetSelected()
	}
}

// SetTitleCount 配置标题及项目总数显示。项目刷新后会自动更新总数。
func (m *Model) SetTitleCount(title, suffix string) {
	m.title = title
	m.titleSuffix = suffix
	m.updateTitle()
}

// TitleText 返回由页面负责排版的本地化标题。
func (m Model) TitleText() string { return m.title }

// CountText 返回当前可见项目数与原始总数。筛选中的实时结果也必须可见，
// 不能依赖 Bubbles 在 Filtering 状态下会被替换掉的内置标题行。
func (m Model) CountText() string {
	total := len(m.list.Items())
	visible := len(m.list.VisibleItems())
	if m.list.FilterValue() != "" {
		return fmt.Sprintf("%d / %d%s", visible, total, m.titleSuffix)
	}
	return fmt.Sprintf("%d%s", total, m.titleSuffix)
}

func (m *Model) updateTitle() {
	if m.title == "" {
		return
	}
	m.list.Title = m.title + " · " + m.CountText()
}

// ItemCount 返回未筛选的项目总数。
func (m Model) ItemCount() int { return len(m.list.Items()) }

// VisibleCount 返回当前筛选结果中的项目数。
func (m Model) VisibleCount() int { return len(m.list.VisibleItems()) }

// SelectedKey 返回当前可见选中项的稳定标识。
func (m Model) SelectedKey() string {
	item, ok := m.list.SelectedItem().(Item)
	if !ok {
		return ""
	}
	return item.Key()
}

// Selected 返回当前可见选中项。
func (m Model) Selected() (Item, bool) {
	item, ok := m.list.SelectedItem().(Item)
	return item, ok
}

// SelectKey 选择可见项目中的稳定标识。返回 false 表示目标已不可见或不存在。
func (m *Model) SelectKey(key string) bool {
	if key == "" {
		return false
	}
	for index, item := range m.list.VisibleItems() {
		keyed, ok := item.(Item)
		if ok && keyed.Key() == key {
			m.list.Select(index)
			return true
		}
	}
	return false
}

// Filtering 表示用户正在编辑官方筛选输入框。
func (m Model) Filtering() bool { return m.list.FilterState() == list.Filtering }

// MoveCursor 允许用户在实时筛选时继续浏览匹配项；筛选输入仍保持焦点。
func (m *Model) MoveCursor(delta int) {
	if delta < 0 {
		m.list.CursorUp()
	} else if delta > 0 {
		m.list.CursorDown()
	}
}

// PageUp/PageDown 在实时筛选时翻页。Bubbles 在 Filtering 态会禁用翻页键，
// 但浮层打开即进入 Filtering 态，需要手动翻页浏览匹配项。
func (m *Model) PageUp()   { m.list.PrevPage() }
func (m *Model) PageDown() { m.list.NextPage() }

// PageItem 是当前 Bubbles 分页中的一项。分页、筛选、游标仍完全由 list.Model
// 持有；调用方只接管可见布局，不能再维护第二套滚动状态。
type PageItem struct {
	Item     Item
	Index    int
	Selected bool
}

// PageItems 返回当前分页内的筛选结果。list.Paginator 的页码和每页容量是唯一来源，
// 因此自定义浮层可以替换默认 View 外观，同时保持官方导航与自动滚动行为。
func (m Model) PageItems() []PageItem {
	items := m.list.VisibleItems()
	perPage := max(1, m.list.Paginator.PerPage)
	start := m.list.Paginator.Page * perPage
	if start >= len(items) {
		return nil
	}
	end := min(len(items), start+perPage)
	result := make([]PageItem, 0, end-start)
	selected := m.list.Index()
	for index := start; index < end; index++ {
		item, ok := items[index].(Item)
		if !ok {
			continue
		}
		result = append(result, PageItem{Item: item, Index: index, Selected: index == selected})
	}
	return result
}

// Wrap 为 list 命令产生的所有消息加上 owner。tea.BatchMsg 必须展开后逐项包装，
// 否则 FilterMatchesMsg 会失去路由机会。
func Wrap(owner string, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			commands := make([]tea.Cmd, 0, len(batch))
			for _, child := range batch {
				if wrapped := Wrap(owner, child); wrapped != nil {
					commands = append(commands, wrapped)
				}
			}
			return BatchMessage{Commands: commands}
		}
		if msg == nil {
			return nil
		}
		return Message{Owner: owner, Inner: msg}
	}
}

func asItems(items []Item) []list.Item {
	result := make([]list.Item, 0, len(items))
	for _, item := range items {
		result = append(result, item)
	}
	return result
}
