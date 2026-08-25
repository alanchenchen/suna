package chat

import (
	"fmt"
	"io"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/tui/components/overlaylist"
	"github.com/alanchenchen/suna/internal/tui/components/selection"
)

const (
	overlayListSkills = "skills"
	overlayListMCP    = "mcp"
	overlayListModels = "models"

	// NativeList* 是 root TUI 渲染原生列表浮层时使用的稳定归属标识。
	NativeListSkills = overlayListSkills
	NativeListMCP    = overlayListMCP
	NativeListModels = overlayListModels
)

// ListStyles 由 root 注入主题，页面只保留列表结构和业务数据。
type ListStyles struct {
	Cursor lipgloss.Style
	Title  lipgloss.Style
	Text   lipgloss.Style
	Dim    lipgloss.Style
	OK     lipgloss.Style
	Error  lipgloss.Style
	Run    lipgloss.Style
}

// ListText 由 root 注入本地化文案，避免通用列表组件依赖 TUI 的翻译器。
type ListText struct {
	SkillsTitle  string
	MCPTitle     string
	ModelsTitle  string
	CountSuffix  string
	Filter       string
	Skill        string
	Skills       string
	Server       string
	Servers      string
	Model        string
	Models       string
	Toggle       string
	Reload       string
	GlobalScope  string
	ProjectScope string
	Select       string
	Close        string
	Tools        string
	Up           string
	Down         string
	FilterHelp   string
	ClearFilter  string
	Cancel       string
}

type skillItem struct{ skill protocol.SkillInfo }

func (i skillItem) Key() string {
	return strings.Join([]string{strings.TrimSpace(i.skill.Scope), i.skill.Path, i.skill.Name}, "\x00")
}
func (i skillItem) FilterValue() string {
	// Skill 名称和说明是用户可理解的检索入口；校验原因、路径和错误只用于展示，不能污染结果。
	return strings.TrimSpace(i.skill.Name + " " + i.skill.Description)
}

type mcpItem struct{ server protocol.MCPServerInfo }

func (i mcpItem) Key() string { return i.server.Name }
func (i mcpItem) FilterValue() string {
	// MCP 搜索只覆盖用户可识别的服务名与传输类型，不匹配命令参数或运行错误。
	return strings.TrimSpace(i.server.Name + " " + i.server.Transport)
}

type modelItem struct{ row ModelPickerRow }

func (i modelItem) Key() string         { return i.row.Ref }
func (i modelItem) FilterValue() string { return strings.TrimSpace(i.row.Ref + " " + i.row.Summary) }

type nativeDelegate struct {
	styles  ListStyles
	text    ListText
	loading func(string) bool
}

// 原生列表保持单行高密度展示；详情用短摘要补充，避免长列表被多行条目挤压。
func (d nativeDelegate) Height() int  { return 1 }
func (d nativeDelegate) Spacing() int { return 0 }
func (d nativeDelegate) Update(tea.Msg, *list.Model) tea.Cmd {
	return nil
}
func (d nativeDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	keyed, ok := item.(overlaylist.Item)
	if !ok {
		return
	}
	fmt.Fprint(w, d.renderItem(m.Width(), index == m.Index(), keyed))
}

// renderItem 是默认 delegate 与 Suna 自定义浮层共同使用的单行渲染逻辑。
// 交互仍由 Bubbles list.Model 驱动，外层只替换默认 chrome 与页面排版。
func (d nativeDelegate) renderItem(width int, selected bool, item overlaylist.Item) string {
	rail := selection.Rail(selected, 0, d.styles.Cursor)
	nameStyle := d.styles.Text
	if selected {
		nameStyle = d.styles.Title
	}

	mark, markStyle := "○", d.styles.Dim
	name, detail, badge := "", "", ""
	switch row := item.(type) {
	case skillItem:
		name = row.skill.Name
		scope := d.text.GlobalScope
		if SkillIsProject(row.skill) {
			scope = d.text.ProjectScope
		}
		if strings.TrimSpace(scope) != "" {
			badge = d.styles.Dim.Render("[" + strings.TrimSpace(scope) + "]")
		}
		detail = strings.TrimSpace(row.skill.Description)
		if SkillIsProject(row.skill) && strings.TrimSpace(row.skill.Path) != "" {
			path := strings.TrimSpace(row.skill.Path)
			if detail == "" {
				detail = path
			} else {
				detail = path + " · " + detail
			}
		}
		if detail == "" && SkillHasIssue(row.skill) {
			detail = strings.Join(append(row.skill.Reasons, row.skill.Error), " · ")
		}
		switch {
		case SkillHasIssue(row.skill):
			mark, markStyle = "!", d.styles.Error
		case SkillIsActive(row.skill):
			// 启用状态用成功色勾选表达，避免再追加冗余文字。
			mark, markStyle = "✓", d.styles.OK
		}
	case mcpItem:
		name = row.server.Name
		transport := strings.TrimSpace(row.server.Transport)
		if transport == "" {
			transport = "stdio"
		}
		detail = fmt.Sprintf("%s · %d %s", transport, row.server.ToolCount, d.text.Tools)
		switch {
		case d.loading != nil && d.loading(row.server.Name):
			mark, markStyle = "◌", d.styles.Run
		case row.server.State == protocol.MCPServerStarting:
			mark, markStyle = "◌", d.styles.Run
		case row.server.State == protocol.MCPServerError || row.server.Error != "":
			mark, markStyle = "!", d.styles.Error
		case row.server.State == protocol.MCPServerActive:
			mark, markStyle = "✓", d.styles.OK
		}
	case modelItem:
		name = row.row.Ref
		detail = row.row.Summary
		switch row.row.Mark {
		case "!":
			mark, markStyle = "!", d.styles.Error
		case "◉":
			mark, markStyle = "✓", d.styles.OK
		}
	}

	return d.renderLineWithBadge(width, rail, mark, markStyle, badge, name, nameStyle, detail)
}

// renderLine 为名称优先保留空间，摘要仅在有余量时展示；状态标记与名称同行表达，不额外附加文字。
func (d nativeDelegate) renderLine(width int, rail, mark string, markStyle lipgloss.Style, name string, nameStyle lipgloss.Style, detail string) string {
	return d.renderLineWithBadge(width, rail, mark, markStyle, "", name, nameStyle, detail)
}

func (d nativeDelegate) renderLineWithBadge(width int, rail, mark string, markStyle lipgloss.Style, badge, name string, nameStyle lipgloss.Style, detail string) string {
	prefix := rail + markStyle.Render(mark) + "  "
	if badge != "" {
		prefix += badge + " "
	}
	available := width - lipgloss.Width(prefix)
	if available < 1 {
		return truncateNative(prefix+name, width)
	}

	nameLimit := available
	if detail != "" && available >= 10 {
		// 为摘要保留一部分空间；名称仍是主信息，窄终端则只显示名称。
		nameLimit = max(4, available*3/5)
	}
	name = truncateNative(strings.TrimSpace(name), nameLimit)
	contentWidth := lipgloss.Width(name)
	remaining := available - contentWidth
	if detail != "" && remaining > 3 {
		detail = truncateNative(strings.TrimSpace(detail), remaining-3)
	}
	line := prefix + nameStyle.Render(name)
	if detail != "" {
		line += d.styles.Dim.Render(" · " + detail)
	}
	// ANSI 样式宽度不可依赖裸字符串长度；最终以可见宽度裁剪，保证 delegate 不会撑破 viewport。
	return truncateNative(line, width)
}

func (m *Model) InitNativeLists(dark bool, styles ListStyles, text ListText) {
	configure := func(model *overlaylist.Model, title, singular, plural string, delegate nativeDelegate) {
		if model.Owner() == "" {
			return
		}
		listModel := model.List()
		model.SetTitleCount(title, text.CountSuffix)
		listModel.FilterInput.Prompt = text.Filter
		listModel.SetStatusBarItemName(singular, plural)
		listModel.SetShowTitle(false)
		listModel.SetShowFilter(false)
		listModel.SetShowStatusBar(false)
		listModel.SetShowPagination(false)
		listModel.SetShowHelp(false)
		listModel.KeyMap = nativeListKeyMap(text)
		// KeyMap 会覆盖 New 时禁用的默认退出键，必须再次禁用，避免 q 退出整个 TUI。
		listModel.DisableQuitKeybindings()
		listModel.Styles = nativeListStyles(dark, styles)
		listModel.AdditionalShortHelpKeys = func() []key.Binding {
			if listModel.FilterState() != list.Unfiltered {
				return nil
			}
			return []key.Binding{key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", text.Close))}
		}
		listModel.SetDelegate(delegate)
	}
	if m.SkillsList.Owner() == "" {
		m.SkillsList = overlaylist.New(overlayListSkills, skillItems(m.Skills), nativeDelegate{styles: styles, text: text}, 1, 1)
	}
	if m.MCPList.Owner() == "" {
		m.MCPList = overlaylist.New(overlayListMCP, mcpItems(m.MCPServers), nativeDelegate{styles: styles, text: text, loading: func(name string) bool { return m.MCPActionServer == name }}, 1, 1)
	}
	if m.ModelList.Owner() == "" {
		m.ModelList = overlaylist.New(overlayListModels, nil, nativeDelegate{styles: styles, text: text}, 1, 1)
	}
	configure(&m.SkillsList, text.SkillsTitle, text.Skill, text.Skills, nativeDelegate{styles: styles, text: text})
	configure(&m.MCPList, text.MCPTitle, text.Server, text.Servers, nativeDelegate{styles: styles, text: text, loading: func(name string) bool { return m.MCPActionServer == name }})
	configure(&m.ModelList, text.ModelsTitle, text.Model, text.Models, nativeDelegate{styles: styles, text: text})
}

func nativeListKeyMap(text ListText) list.KeyMap {
	// 保留 Bubbles list 的筛选状态机，但只暴露上下箭头导航，减少长帮助和多套快捷键。
	return list.KeyMap{
		CursorUp:             key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", text.Up)),
		CursorDown:           key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", text.Down)),
		PrevPage:             key.NewBinding(),
		NextPage:             key.NewBinding(),
		GoToStart:            key.NewBinding(),
		GoToEnd:              key.NewBinding(),
		Filter:               key.NewBinding(key.WithKeys("/"), key.WithHelp("/", text.FilterHelp)),
		ClearFilter:          key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", text.ClearFilter)),
		CancelWhileFiltering: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", text.Cancel)),
		AcceptWhileFiltering: key.NewBinding(),
		ShowFullHelp:         key.NewBinding(),
		CloseFullHelp:        key.NewBinding(),
		Quit:                 key.NewBinding(),
		ForceQuit:            key.NewBinding(),
	}
}

func nativeListStyles(dark bool, styles ListStyles) list.Styles {
	result := list.DefaultStyles(dark)
	result.TitleBar = result.TitleBar.Padding(0)
	result.Title = styles.Title
	result.Filter.Blurred.Prompt = styles.Cursor
	result.Filter.Focused.Prompt = styles.Cursor
	result.Filter.Cursor.Color = styles.Cursor.GetForeground()
	result.DefaultFilterCharacterMatch = styles.Cursor.Underline(true)
	// 由 Suna 在外层渲染本地化空状态，避免 Bubbles 内部硬编码英文。
	result.NoItems = lipgloss.NewStyle()
	result.PaginationStyle = styles.Dim
	result.HelpStyle = styles.Dim.Padding(1, 0, 0, 0)
	result.ActivePaginationDot = styles.Cursor.SetString("•")
	result.InactivePaginationDot = styles.Dim.SetString("•")
	return result
}

func skillItems(skills []protocol.SkillInfo) []overlaylist.Item {
	items := make([]overlaylist.Item, 0, len(skills))
	for _, skill := range skills {
		items = append(items, skillItem{skill: skill})
	}
	return items
}
func mcpItems(servers []protocol.MCPServerInfo) []overlaylist.Item {
	items := make([]overlaylist.Item, 0, len(servers))
	for _, server := range servers {
		items = append(items, mcpItem{server: server})
	}
	return items
}
func modelItems(rows []ModelPickerRow) []overlaylist.Item {
	items := make([]overlaylist.Item, 0, len(rows))
	for _, row := range rows {
		items = append(items, modelItem{row: row})
	}
	return items
}

// NativeListRows 以 Bubbles 当前分页状态渲染 Suna 的紧凑行。它不维护游标或滚动，
// 只读取 OverlayList 的当前页，确保自定义外观与官方 list 状态始终一致。
func (m *Model) NativeListRows(owner string, styles ListStyles, text ListText, width int) []string {
	var (
		model    *overlaylist.Model
		delegate nativeDelegate
	)
	switch owner {
	case overlayListSkills:
		model = &m.SkillsList
		delegate = nativeDelegate{styles: styles, text: text}
	case overlayListMCP:
		model = &m.MCPList
		delegate = nativeDelegate{styles: styles, text: text, loading: func(name string) bool { return m.MCPActionServer == name }}
	case overlayListModels:
		model = &m.ModelList
		delegate = nativeDelegate{styles: styles, text: text}
	default:
		return nil
	}
	if model.Owner() == "" {
		return nil
	}
	page := model.PageItems()
	rows := make([]string, 0, len(page))
	for _, item := range page {
		rows = append(rows, delegate.renderItem(width, item.Selected, item.Item))
	}
	return rows
}

func (m *Model) UpdateSkillsList(msg tea.Msg) tea.Cmd { return m.SkillsList.Update(msg) }
func (m *Model) UpdateMCPList(msg tea.Msg) tea.Cmd    { return m.MCPList.Update(msg) }
func (m *Model) UpdateModelList(msg tea.Msg) tea.Cmd  { return m.ModelList.Update(msg) }

func truncateNative(value string, width int) string {
	if width <= 1 {
		return ansi.Truncate(value, max(0, width), "")
	}
	return ansi.Truncate(value, width, "…")
}
