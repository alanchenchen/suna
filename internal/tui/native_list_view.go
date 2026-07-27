package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/alanchenchen/suna/internal/tui/components/overlaylist"
)

// nativeListEmptyHint 补齐 Bubbles list 未提供本地化接口的空状态文案。
// 列表的导航、筛选和分页仍完全由 Bubbles 负责。
func (t *TUI) nativeListEmptyHint(model overlaylist.Model, emptyKey string) string {
	if model.ItemCount() == 0 {
		return styleDim.Render(t.tr(emptyKey))
	}
	if model.VisibleCount() == 0 && strings.TrimSpace(model.List().FilterValue()) != "" {
		return styleDim.Render(t.tr("tui.list.no_matches"))
	}
	return ""
}

// nativeListHeader 始终将标题或筛选输入与实时数量排在同一行。Bubbles 默认
// View 会在筛选时用输入框替换标题；这里接管布局，避免数量和筛选状态丢失。
func (t *TUI) nativeListHeader(model overlaylist.Model) string {
	width := model.List().Width()
	count := styleDim.Render(model.CountText())
	countWidth := lipgloss.Width(count)
	leftWidth := max(1, width-countWidth-2)

	var left string
	if model.Filtering() {
		promptWidth := lipgloss.Width(model.List().FilterInput.Prompt)
		// textinput 会在光标后保留一个显示单元；预留该空间后由组件自身
		// 横向滚动输入内容，不能再对整行追加省略号，否则会像异常状态残留。
		model.List().FilterInput.SetWidth(max(1, leftWidth-promptWidth-1))
		left = model.List().FilterInput.View()
	} else {
		left = styleHL.Render(model.TitleText())
	}
	// 输入框本身负责长查询的横向滚动。这里仅做安全裁剪，不能显示“…”并
	// 伪装成筛选状态的一部分。
	left = ansi.Truncate(left, leftWidth, "")
	gap := max(1, width-lipgloss.Width(left)-countWidth)
	return left + strings.Repeat(" ", gap) + count
}

// nativeListFooter 显示与当前状态严格一致的少量操作。键位判断和筛选状态机
// 仍由 Bubbles list.Model 负责，这里只提供 Suna 的紧凑可发现性文案。
func (t *TUI) nativeListFooter(model overlaylist.Model, action string) string {
	text := t.nativeListText()
	parts := []string{styleCursor.Render("↑↓") + " " + styleDim.Render(t.tr("tui.list.key.move"))}
	if model.Filtering() {
		parts = append(parts,
			styleCursor.Render("Enter")+" "+styleDim.Render(action),
			styleCursor.Render("Esc")+" "+styleDim.Render(text.ClearFilter),
		)
	} else {
		parts = append(parts,
			styleCursor.Render("/")+" "+styleDim.Render(text.FilterHelp),
			styleCursor.Render(actionKey(action))+" "+styleDim.Render(action),
			styleCursor.Render("Esc")+" "+styleDim.Render(text.Close),
		)
	}
	return strings.Join(parts, styleDim.Render("  ·  "))
}

func actionKey(action string) string {
	if action == "toggle" || action == "切换" {
		return "Enter/Space"
	}
	return "Enter"
}

// renderNativeListOverlay 仅接管视觉骨架：紧凑标题、筛选栏、当前分页行和 footer。
// Bubbles list.Model 继续作为筛选、游标、自动滚动与分页的唯一状态来源。
func (t *TUI) renderNativeListOverlay(owner string, model *overlaylist.Model, width int, action, emptyKey, loading, backendError string) string {
	panelWidth := nativeListWidth(width, 82)
	// boxStyle 的 Width 包含边框与 Padding；list 的实际内容宽度必须扣除这些
	// 空间，否则标题/筛选行会被 box 再次折行，造成截图中标题与数量分离。
	innerWidth := max(1, panelWidth-6)
	maxHeight := max(8, t.overlayMaxHeight())

	// 外层边框与上下 padding 占四行；内部 header、分隔线、footer 与 footer
	// 前的两行留白共同占五行。小列表按内容收缩，大列表最多显示十行，
	// 由 Bubbles 分页继续滚动。
	reserved := 9
	if strings.TrimSpace(backendError) != "" {
		reserved++
	}
	bodyCap := max(1, min(10, maxHeight-reserved))
	bodyRows := model.VisibleCount()
	if bodyRows == 0 {
		bodyRows = 1
	}
	bodyRows = min(bodyRows, bodyCap)
	model.List().SetSize(innerWidth, bodyRows)

	header := t.nativeListHeader(*model)
	divider := styleDim.Render(strings.Repeat("─", innerWidth))
	hint := t.nativeListEmptyHint(*model, emptyKey)
	if loading != "" && model.ItemCount() == 0 {
		hint = styleDim.Render(loading)
	}

	var rows []string
	if hint != "" {
		rows = []string{hint}
	} else {
		rows = t.chat.NativeListRows(owner, t.nativeListStyles(), t.nativeListText(), innerWidth)
	}
	if len(rows) == 0 {
		rows = []string{""}
	}

	lines := []string{header, divider}
	lines = append(lines, rows...)
	if detail := nativeListError(backendError, innerWidth); detail != "" {
		lines = append(lines, detail)
	}
	// 操作提示不能紧贴最后一项，留出稳定的两行呼吸空间，让内容区和
	// 可执行操作形成清晰层级；同时预留高度避免大列表挤出浮层。
	lines = append(lines, "", "", t.nativeListFooter(*model, action))
	return boxStyle.Width(panelWidth).Padding(1, 2).Render(strings.Join(lines, "\n"))
}

// nativeListError 将后端错误限制为浮层中的一行，避免长错误撑破列表布局。
func nativeListError(value string, width int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return styleError.Render(truncateDisplay(value, max(1, width)))
}
