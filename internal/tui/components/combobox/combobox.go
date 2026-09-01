// Package combobox 提供"输入即过滤"的同步选择器。
//
// 与 Bubbles list 的异步过滤链路（FilterMatchesMsg 经命令包装送回）不同，
// 这里在按键处理内同步过滤：无异步消息、无跨页路由，状态单一不会丢失。
// 模型表单的 model 字段用它实现"要么选、要么输入"：
// 列表可拉取时输入即筛选，↑↓ 选择后 Enter 确认；
// 拉取失败或列表为空时直接输入自定义名称，Enter 同样生效。
package combobox

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Styles 由页面注入主题样式，组件自身不感知全局主题。
type Styles struct {
	Cursor lipgloss.Style // 光标条样式
	Value  lipgloss.Style // 光标所在行内容
	Dim    lipgloss.Style // 未选中行与提示文案
}

// Model 是输入即过滤的同步选择器：输入框即筛选框，候选与光标全部同步更新。
type Model struct {
	input     textinput.Model
	items     []string
	filtered  []string
	cursor    int
	scroll    int
	maxRows   int
	width     int
	emptyHint string
	current   string
	focused   bool
}

// New 创建选择器；placeholder 是输入框空态提示。
func New(placeholder string) Model {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = placeholder
	in.CharLimit = 200
	in.SetWidth(36)
	m := Model{input: in, maxRows: 8, width: 40}
	m.refilter()
	return m
}

// SetPrompt 设置输入行提示前缀（如 "Filter: "），由页面注入本地化文案。
func (m *Model) SetPrompt(prompt string) { m.input.Prompt = prompt }

// SetSize 设置内容宽度与最大可见行数；候选超出时组件内部滚动。
func (m *Model) SetSize(width, maxRows int) {
	m.width = max(1, width)
	m.maxRows = max(1, maxRows)
	m.input.SetWidth(max(1, m.width-2))
}

// SetEmptyHint 设置无匹配时的提示文案（页面注入 i18n）。
func (m *Model) SetEmptyHint(hint string) { m.emptyHint = hint }

// SetCurrent 记录当前已填写的模型名，列表中对应项显示 ✓ 标记。
func (m *Model) SetCurrent(current string) { m.current = strings.TrimSpace(current) }

// Focus 聚焦输入框；Blur 收起光标。
func (m *Model) Focus() tea.Cmd {
	m.focused = true
	return m.input.Focus()
}

// Blur 取消聚焦。
func (m *Model) Blur() {
	m.focused = false
	m.input.Blur()
}

// SetItems 全量替换候选并按当前输入重新过滤；光标回到首行。
// 异步拉取结果到达时调用：保留用户已输入的筛选文本。
func (m *Model) SetItems(items []string) {
	m.items = append([]string(nil), items...)
	m.refilter()
}

// UpdateInput 把非导航按键转给内部输入框（字符、退格、粘贴、光标移动），
// 输入值变化时同步重算过滤结果。导航键（up/down/enter/esc 等）由页面拦截，
// 不进入这里。输入值未变化时不重算：光标闪烁等周期性消息若触发 refilter
// 会把光标拽回首行，表现为“上下移动后又被弹回第一项”。
func (m *Model) UpdateInput(msg tea.Msg) tea.Cmd {
	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != before {
		m.refilter()
	}
	return cmd
}

// refilter 按当前输入同步过滤（大小写不敏感子串匹配）。
// 每次输入变化后光标回到首行：输入即筛选，用户预期第一项就是最佳匹配。
func (m *Model) refilter() {
	query := strings.ToLower(strings.TrimSpace(m.input.Value()))
	m.filtered = m.filtered[:0]
	for _, item := range m.items {
		if query == "" || strings.Contains(strings.ToLower(item), query) {
			m.filtered = append(m.filtered, item)
		}
	}
	m.cursor = 0
	m.scroll = 0
}

// MoveCursor 移动光标，到边界后环绕（无缝循环）：到底继续向下回到顶部，
// 到顶继续向上回到底部。浮层是独立小窗口，环绕只影响内部滚动窗口，
// 不会引起页面级跳变，因此这里循环是安全的。
func (m *Model) MoveCursor(delta int) {
	if len(m.filtered) == 0 {
		return
	}
	n := len(m.filtered)
	// 双重取模兼容负数偏移（Go 的 % 对负数返回负值）。
	m.cursor = ((m.cursor+delta)%n + n) % n
	m.followCursor()
}

// PageUp/PageDown 按可见行数翻动光标，同样不循环。
func (m *Model) PageUp()   { m.MoveCursor(-m.maxRows) }
func (m *Model) PageDown() { m.MoveCursor(m.maxRows) }

// followCursor 保证光标行在可见窗口内（内部滚动窗口跟随）。
func (m *Model) followCursor() {
	if m.cursor < m.scroll {
		m.scroll = m.cursor
	}
	if m.cursor >= m.scroll+m.maxRows {
		m.scroll = m.cursor - m.maxRows + 1
	}
}

// Selected 返回光标所在候选；无候选时返回 false。
func (m Model) Selected() (string, bool) {
	if m.cursor < 0 || m.cursor >= len(m.filtered) {
		return "", false
	}
	return m.filtered[m.cursor], true
}

// InputValue 返回输入框文本（已去空白）：即筛选词，也是自定义名的入口。
func (m Model) InputValue() string { return strings.TrimSpace(m.input.Value()) }

// Count 返回过滤后的候选数量。
func (m Model) Count() int { return len(m.filtered) }

// View 渲染输入行与可见候选行；无匹配时显示空态提示。
func (m Model) View(s Styles) string {
	lines := []string{m.input.View()}
	if len(m.filtered) == 0 {
		if m.emptyHint != "" {
			lines = append(lines, "", "  "+s.Dim.Render(m.emptyHint))
		}
		return strings.Join(lines, "\n")
	}
	end := min(len(m.filtered), m.scroll+m.maxRows)
	for i := m.scroll; i < end; i++ {
		name := m.filtered[i]
		if i == m.cursor {
			mark := ""
			if m.current != "" && name == m.current {
				mark = s.Dim.Render(" ✓")
			}
			lines = append(lines, s.Cursor.Render("▎ ")+s.Value.Render(name)+mark)
			continue
		}
		lines = append(lines, "  "+s.Dim.Render(name))
	}
	return strings.Join(lines, "\n")
}
