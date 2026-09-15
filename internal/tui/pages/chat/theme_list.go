package chat

import (
	tea "charm.land/bubbletea/v2"

	"github.com/alanchenchen/suna/internal/tui/components/overlaylist"
)

// 主题列表是纯展示层数据：条目由 root TUI 注入，页面只维护光标与选中状态。
// 内置 default 始终排在首位，用户因此能随时切回它。

// ThemeItem 是主题列表项。Err 非空表示该主题文件解析失败，
// 仍在列表中展示（让用户看到原因），但不可应用。
//
// Display 是展示名（内置 default 需要本地化，用户主题直接用文件名）；
// 为空时回退到 Name。Builtin 标记内置主题，供 UI 区分展示。
//
// Active 标记“已保存生效”的主题：光标移动只做预览（不落库），
// 因此它与光标位置是两件事，标记的是 Esc 取消后会回到的那一个。
//
// Swatch 是已渲染好的 ANSI 色块（由 root TUI 用该主题自身的颜色生成）。
// 页面只负责拼接，不依赖主题子系统，避免 chat 包与主题实现耦合。
type ThemeItem struct {
	Name    string
	Display string
	Builtin bool
	Active  bool
	Err     string
	Swatch  string
}

func (i ThemeItem) Key() string { return i.Name }

func (i ThemeItem) FilterValue() string {
	if i.Display != "" {
		return i.Display
	}
	return i.Name
}

// Label 返回用于展示的名称。
func (i ThemeItem) Label() string {
	if i.Display != "" {
		return i.Display
	}
	return i.Name
}

// SetThemeItems 注入主题列表项并把光标定位到当前主题。
func (m *Model) SetThemeItems(items []ThemeItem, current string) {
	listItems := make([]overlaylist.Item, 0, len(items))
	for _, it := range items {
		listItems = append(listItems, it)
	}
	m.ThemeList.SetItems(listItems)
	if current != "" {
		m.ThemeList.SelectKey(current)
	}
}

// SelectedTheme 返回光标所在主题名与其错误摘要（空表示可用）。
func (m *Model) SelectedTheme() (string, string) {
	item, ok := m.ThemeList.Selected()
	if !ok {
		return "", ""
	}
	if it, ok := item.(ThemeItem); ok {
		return it.Name, it.Err
	}
	return item.Key(), ""
}

// UpdateThemeList 把消息转发给主题列表组件。
func (m *Model) UpdateThemeList(msg tea.Msg) tea.Cmd {
	return m.ThemeList.Update(msg)
}
