package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/alanchenchen/suna/internal/tui/components/scroll"
	textutil "github.com/alanchenchen/suna/internal/tui/components/text"
	"github.com/alanchenchen/suna/internal/tui/components/toolview"
)

// subtaskContextMaxLines 限制 subtask block 中 context 字段的展示行数：
// context 是 main 显式传入的补充上下文，可能很长，全量展示会挤掉工具 timeline。
const subtaskContextMaxLines = 3

func (t *TUI) renderSubtaskBlock(block *toolBlock) string {
	if block == nil {
		return ""
	}
	ids := subtaskIDsInBlock(block)
	if len(ids) == 0 {
		return ""
	}
	// 面板活跃判定与数据来源共用 ActiveSubtaskBlock：
	// 正在执行的块，或用户用 Ctrl+T 展开的块。子任务结果恰好在根调用结束时到达
	// （此时 CurrentToolBlock 已置空），因此展开态必须能激活面板。
	active := block == t.chat.ActiveSubtaskBlock()
	if active {
		t.ensureSubtaskSelection()
	}
	maxWidth := max(40, t.width-8)
	innerWidth := max(24, maxWidth-8)
	sectionWidth := max(24, maxWidth-4)
	done, running, failed := t.subtaskStatusCounts(ids)
	title := fmt.Sprintf("%s "+t.tr("tui.subtask_panel.title"), t.subtaskBlockStatusIcon(done, running, failed, len(ids)), len(ids), running, done, failed)
	if !active {
		// 折叠态提示：与思考链的 "Ctrl+R 展开" 同款，提示只依赖块状态，不依赖视窗位置。
		title += " · " + t.tr("tui.tool.detail_hint")
	} else if block == t.chat.ExpandedBlock && t.chat.ExpandedBoxKind == "subtask" {
		// 展开态提示：纯 subtask 块没有 tool 盒子，收起提示只能由面板提供，
		// 否则用户展开后看不到任何退出提示（与 tool 盒子的展开态保持一致）。
		title += " · " + t.tr("tui.tool.detail_expanded")
	}
	width := maxWidth
	if !active {
		// 非 active（含完成后的历史 block）是纯展示：盒子宽度按行内容与标题收窄，
		// 与 tool block 的按需宽度一致；active 时保持固定宽度，避免选中态跳动。
		width = t.subtaskContentWidth(ids, title, maxWidth)
		innerWidth = max(24, width-8)
		sectionWidth = max(24, width-4)
	}
	lines := make([]string, 0)
	selected := -1
	if active {
		selected = t.chat.SubtaskCursor
	}
	lines = append(lines, t.renderSubtaskRows(ids, innerWidth, selected)...)
	if active {
		if current := t.selectedSubtask(); current != nil {
			lines = append(lines, t.subtaskSectionTitle(t.tr("tui.subtask_panel.current"), sectionWidth))
			lines = append(lines, t.renderSelectedSubtaskSummary(current, innerWidth)...)
			lines = append(lines, t.renderSelectedSubtaskResult(current, innerWidth)...)
			lines = append(lines, t.subtaskSectionTitle(t.tr("tui.subtask_panel.tools"), sectionWidth))
			lines = append(lines, t.renderSelectedSubtaskTools(innerWidth)...)
			if t.chat.SubtaskToolDetailExpanded {
				lines = append(lines, t.subtaskSectionTitle(t.tr("tui.subtask_panel.tool_detail"), sectionWidth))
				lines = append(lines, t.renderSelectedSubtaskToolDetail(innerWidth)...)
			}
		}
		if t.canToggleSubtaskDetailWithEnter() {
			lines = append(lines, styleMuted.Render(t.tr(t.subtaskPanelHelpKey())))
		}
	}
	return textutil.IndentLines(renderTitledRoundBox(width, title, lines), transcriptBlockIndent)
}

func subtaskIDsInBlock(block *toolBlock) []string {
	if block == nil {
		return nil
	}
	ids := make([]string, 0, len(block.Order))
	for _, id := range block.Order {
		te := block.Entries[id]
		if toolview.IsSubtask(te) {
			ids = append(ids, id)
		}
	}
	return ids
}

func renderTitledRoundBox(width int, title string, lines []string) string {
	return renderTitledRoundBoxWithStyles(width, title, lines, styleHL, styleDim)
}

func renderThinkingRoundBox(width int, title string, lines []string) string {
	return renderTitledRoundBoxWithStyles(width, title, lines, styleBrand, styleBrand)
}

func renderTitledRoundBoxWithStyles(width int, title string, lines []string, titleStyle, borderStyle lipgloss.Style) string {
	// 手工绘制带标题边框时，width 表示整块外宽；边框之间的可用宽度是 width-2。
	// 内容行在进入这里前已经按内宽截断，这里只负责补齐，避免 ANSI 样式导致右边框漂移。
	width = max(12, width)
	contentWidth := max(8, width-2)
	titleText := strings.TrimSpace(title)
	if lipgloss.Width(titleText) > contentWidth-3 {
		titleText = textutil.TruncateRunes(titleText, max(1, contentWidth-3))
	}
	titlePrefix := "─ "
	titleSuffix := " "
	titleWidth := lipgloss.Width(titlePrefix) + lipgloss.Width(titleText) + lipgloss.Width(titleSuffix)
	topRest := strings.Repeat("─", max(0, contentWidth-titleWidth))
	top := borderStyle.Render("╭"+titlePrefix) + titleStyle.Render(titleText) + borderStyle.Render(titleSuffix+topRest+"╮")
	body := make([]string, 0, len(lines)+2)
	body = append(body, top)
	for _, line := range lines {
		content := ansi.Truncate(" "+line+" ", contentWidth, "…")
		pad := strings.Repeat(" ", max(0, contentWidth-lipgloss.Width(content)))
		body = append(body, borderStyle.Render("│")+content+pad+borderStyle.Render("│"))
	}
	body = append(body, borderStyle.Render("╰"+strings.Repeat("─", contentWidth)+"╯"))
	return strings.Join(body, "\n")
}

// subtaskContentWidth 计算非 active subtask block 的按需宽度：
// 取行列表内容（cursor + icon + label + activity + duration）与标题的较大者，
// 下限 40、上限 maxWidth，与 tool block 的按需宽度语义一致。
// 标题必须纳入计算，否则折叠态标题（含 "Ctrl+T 详情" 提示）会被边框截断，
// 提示一旦被截就等于没有（与 tool 盒子为提示预留空间同策略）。
func (t *TUI) subtaskContentWidth(ids []string, title string, maxWidth int) int {
	// 标题预算与 renderTitledRoundBox 的截断公式对齐：contentWidth-3 = (width-2)-3，
	// 因此宽度需满足 width >= 标题宽 + 5，否则标题会被边框截断（提示一旦被截等于没有）。
	w := lipgloss.Width(title) + 5
	for _, id := range ids {
		te := t.findTool(id)
		if te == nil {
			continue
		}
		label := toolview.PlainIntentLabel(te)
		activity := t.subtaskActivity(te, maxWidth)
		line := lipgloss.Width(label)
		if activity != "" {
			line += lipgloss.Width(" · " + activity)
		}
		if dur := t.subtaskDuration(te); dur != "" {
			line += lipgloss.Width(subtaskDurationSep(dur) + dur)
		}
		w = max(w, line+4) // cursor(2) + icon(1) + space(1)
	}
	return min(maxWidth, max(40, w))
}

func (t *TUI) renderSubtaskRows(ids []string, innerWidth int, selected int) []string {
	rows := make([]string, 0, len(ids))
	for i, id := range ids {
		te := t.findTool(id)
		if te == nil {
			continue
		}
		cursor := "  "
		labelStyle := lipgloss.NewStyle()
		if i == selected {
			cursor = styleCursor.Render("▶ ")
			labelStyle = styleHL
		}
		icon := t.subtaskStatusIcon(te)
		prefixWidth := 4 // cursor(2) + icon(1) + space(1)
		dur := t.subtaskDuration(te)
		durWidth := 0
		if dur != "" {
			durWidth = lipgloss.Width(subtaskDurationSep(dur) + dur)
		}
		rawLabel := toolview.PlainIntentLabel(te)
		rawActivity := t.subtaskActivity(te, innerWidth)
		label, activity := fitSubtaskRowParts(rawLabel, rawActivity, innerWidth-prefixWidth-durWidth)
		line := fmt.Sprintf("%s%s %s", cursor, icon, labelStyle.Render(label))
		if activity != "" {
			line += styleDim.Render(" · ") + styleMuted.Render(activity)
		}
		if dur != "" {
			line += styleMuted.Render(subtaskDurationSep(dur) + dur)
		}
		rows = append(rows, line)
	}
	return rows
}

func (t *TUI) renderSelectedSubtaskSummary(te *toolEntry, innerWidth int) []string {
	var parts []string
	if te != nil && te.Status == toolview.StatusError {
		if reason := t.subtaskFailureReason(te); reason != "" {
			parts = append(parts, styleMuted.Render(t.tr("tui.subtask_panel.error")+": ")+styleToolErr.Render(textutil.TruncateRunes(reason, max(12, innerWidth-8))))
		}
	}
	if model := subtaskParamLabel(te, "model"); model != "" {
		parts = append(parts, styleMuted.Render(t.tr("tui.tool.model")+": ")+styleToolMuted.Render(textutil.TruncateRunes(model, max(10, innerWidth-8))))
	}
	if tools := subtaskParamLabel(te, "tools"); tools != "" {
		parts = append(parts, styleMuted.Render(t.tr("tui.tool.tools")+": ")+styleToolMuted.Render(textutil.TruncateRunes(tools, max(10, innerWidth-8))))
	}
	if task := subtaskParamText(te, "task"); task != "" {
		parts = append(parts, styleMuted.Render(t.tr("tui.tool.task")+":"))
		for _, line := range strings.Split(strings.TrimRight(task, "\n"), "\n") {
			for _, wrapped := range textutil.WrapLine(textutil.ExpandTabs(line, 4), max(12, innerWidth)) {
				parts = append(parts, styleToolMuted.Render(wrapped))
			}
		}
	}
	// context 是 main 显式传给 subtask 的补充上下文（可能含图片 source 等关键引用），
	// 与 task 并列展示但限制行数，避免长 context 撑爆 subtask block 挤掉工具 timeline。
	if ctx := subtaskParamText(te, "context"); ctx != "" {
		parts = append(parts, styleMuted.Render(t.tr("tui.tool.context")+":"))
		lines := strings.Split(strings.TrimRight(ctx, "\n"), "\n")
		for i, line := range lines {
			if i >= subtaskContextMaxLines {
				parts = append(parts, styleToolDim.Render("…"))
				break
			}
			for _, wrapped := range textutil.WrapLine(textutil.ExpandTabs(line, 4), max(12, innerWidth)) {
				parts = append(parts, styleToolMuted.Render(wrapped))
			}
		}
	}
	return parts
}

// subtaskResultMaxRows 限制结果小节的可视行数：结果可能很长，全量展示会挤掉工具 timeline。
// 与思考链一致按终端高度自适应，超出部分用 PgUp/PgDn 滚动查看全文。
func (t *TUI) subtaskResultMaxRows() int {
	return min(8, max(4, t.height/10))
}

// subtaskResultSource 构建结果正文的虚拟行数据源（含 wrap 计数），
// 供渲染与滚动共用，避免两处各自计算行数不一致。
func (t *TUI) subtaskResultSource(te *toolEntry, innerWidth int) (toolview.SubtaskResult, scroll.LineSource) {
	if te == nil || te.Status == toolview.StatusRunning || strings.TrimSpace(te.Result) == "" {
		return toolview.SubtaskResult{}, nil
	}
	result := toolview.ParseSubtaskResult(te.Result)
	if result.Text == "" {
		return result, nil
	}
	return result, scroll.NewWrappedLineSection(result.Text, max(12, innerWidth), styleToolMuted)
}

// renderSelectedSubtaskResult 渲染选中子任务的结果小节。
// spawn 结果是 JSON 载荷，这里解析出可读正文与副作用披露，避免把原始 JSON 丢给用户。
// 正文按窗口滚动展示，长结果不会丢失（PgUp/PgDn 查看剩余内容）。
func (t *TUI) renderSelectedSubtaskResult(te *toolEntry, innerWidth int) []string {
	result, source := t.subtaskResultSource(te, innerWidth)
	if result.Text == "" && result.SideEffects == "" {
		return nil
	}
	var parts []string
	if source != nil {
		parts = append(parts, styleMuted.Render(t.tr("tui.subtask_panel.result")+":"))
		height := t.subtaskResultMaxRows()
		body, start, total := scroll.Window(source, height, &t.chat.SubtaskResultScroll)
		parts = append(parts, body...)
		if total > height {
			parts = append(parts, styleToolDim.Render(fmt.Sprintf("PgUp/PgDn %s %d-%d/%d", t.tr("tui.overlay.scroll"), start+1, min(total, start+height), total)))
		}
	}
	if result.SideEffects != "" {
		parts = append(parts, styleMuted.Render(t.tr("tui.subtask_panel.side_effects")+": ")+styleToolMuted.Render(textutil.TruncateRunes(result.SideEffects, max(12, innerWidth-10))))
	}
	return parts
}

// subtaskResultScrollable 判断当前子任务结果是否超出可视行数。
// 只有确实有剩余内容时才消耗 PgUp/PgDn 与滚轮，避免短结果下按键"无反应"。
func (t *TUI) subtaskResultScrollable() bool {
	if !t.hasActiveSubtaskPanel() {
		return false
	}
	te := t.selectedSubtask()
	_, source := t.subtaskResultSource(te, t.subtaskResultInnerWidth())
	return source != nil && source.Len() > t.subtaskResultMaxRows()
}

// scrollSubtaskResult 滚动结果小节窗口。
// 返回是否真正消费了本次滚动：已到边界时返回 false，让调用方透传给
// transcript（滚动链），否则视窗会被结果窗口"卡住"。
func (t *TUI) scrollSubtaskResult(delta int) bool {
	te := t.selectedSubtask()
	_, source := t.subtaskResultSource(te, t.subtaskResultInnerWidth())
	if source == nil {
		t.chat.SubtaskResultScroll = 0
		return false
	}
	maxOffset := max(0, source.Len()-t.subtaskResultMaxRows())
	next := clampInt(t.chat.SubtaskResultScroll+delta, 0, maxOffset)
	if next == t.chat.SubtaskResultScroll {
		return false
	}
	t.chat.SubtaskResultScroll = next
	return true
}

// subtaskResultInnerWidth 返回结果小节可用的内容宽度。
// 必须与 renderSubtaskBlock 中 active 面板的 innerWidth 完全一致：active 面板
// 使用固定宽度（不按内容收窄），否则滚动会按更窄宽度 wrap 出更多行，
// maxOffset 偏大导致滚动无法揭示最后一行。
func (t *TUI) subtaskResultInnerWidth() int {
	return max(24, max(40, t.width-8)-8)
}

func (t *TUI) renderSelectedSubtaskTools(innerWidth int) []string {
	children := t.selectedSubtaskTools()
	if len(children) == 0 {
		if t.selectedSubtaskWaitingForTool() {
			return []string{styleToolRun.Render(spinnerPlaceholder+" ") + styleMuted.Render(t.tr("tui.subtask_panel.waiting_tool"))}
		}
		return []string{styleMuted.Render(t.tr("tui.subtask_panel.no_tools"))}
	}
	t.ensureSubtaskSelection()
	height := min(len(children), t.subtaskTimelineHeight())
	start := t.chat.SubtaskToolCursor - height + 1
	if start < 0 {
		start = 0
	}
	if start+height > len(children) {
		start = max(0, len(children)-height)
	}
	end := min(len(children), start+height)
	rows := make([]string, 0, height+2)
	if start > 0 {
		rows = append(rows, styleMuted.Render(fmt.Sprintf(t.tr("tui.subtask_panel.more_above"), start)))
	}
	for i := start; i < end; i++ {
		child := children[i]
		cursor := "  "
		labelStyle := lipgloss.NewStyle()
		if i == t.chat.SubtaskToolCursor {
			cursor = styleCursor.Render("▎ ")
			labelStyle = styleHL
		}
		icon := t.subtaskStatusIcon(child)
		prefixWidth := 4 // cursor(2) + icon(1) + space(1)
		dur := ""
		if child.Status == toolview.StatusRunning && !child.StartedAt.IsZero() {
			dur = liveElapsedPlaceholder(child.StartedAt)
		} else if fixed := fixedToolDuration(child); fixed > 0 {
			dur = toolview.FormatCompactDuration(fixed)
		}
		durWidth := 0
		if dur != "" {
			durWidth = lipgloss.Width(subtaskDurationSep(dur) + dur)
		}
		remaining := max(0, innerWidth-prefixWidth-durWidth)
		label := t.subtaskToolTimelineLabel(child, max(4, remaining))
		line := fmt.Sprintf("%s%s %s", cursor, icon, labelStyle.Render(label))
		if dur != "" {
			line += styleMuted.Render(subtaskDurationSep(dur) + dur)
		}
		rows = append(rows, line)
	}
	if end < len(children) {
		rows = append(rows, styleMuted.Render(fmt.Sprintf(t.tr("tui.subtask_panel.more_below"), len(children)-end)))
	} else if t.selectedSubtaskWaitingForTool() {
		rows = append(rows, styleToolRun.Render(spinnerPlaceholder)+styleMuted.Render(" "+t.tr("tui.subtask_panel.waiting_tool")))
	}
	return rows
}

func (t *TUI) subtaskTimelineHeight() int {
	// 详情展开时优先保留顶部子任务信息和下方详情区，小终端下进一步压缩工具列表高度。
	if t.chat.SubtaskToolDetailExpanded {
		return min(5, max(3, t.height/10))
	}
	return min(7, max(subtaskTimelineMaxRows, t.height/8))
}

func (t *TUI) subtaskToolTimelineLabel(child *toolEntry, width int) string {
	if child == nil {
		return ""
	}
	if intent := strings.TrimSpace(toolview.PlainIntentLabel(child)); intent != "" {
		return textutil.TruncateRunes(intent, width)
	}
	if semantic := strings.TrimSpace(toolview.SemanticSummary(child, width, t.toolRenderDeps().Labels)); semantic != "" {
		return textutil.TruncateRunes(semantic, width)
	}
	name := strings.TrimSpace(child.Name)
	if name == "" {
		name = toolview.DisplayName(child.RawName)
	}
	return textutil.TruncateRunes(name, width)
}

func (t *TUI) subtaskActiveToolLabel(child *toolEntry, width int) string {
	if child == nil {
		return ""
	}
	name := strings.TrimSpace(child.Name)
	if name == "" {
		name = toolview.DisplayName(child.RawName)
	}
	semantic := strings.TrimSpace(toolview.SemanticSummary(child, max(8, width-lipgloss.Width(name)-1), t.toolRenderDeps().Labels))
	if semantic == "" || semantic == name {
		return textutil.TruncateRunes(name, width)
	}
	return textutil.TruncateRunes(strings.TrimSpace(name+" "+semantic), width)
}

func (t *TUI) selectedSubtaskWaitingForTool() bool {
	parent := t.selectedSubtask()
	if parent == nil || parent.Status != toolview.StatusRunning {
		return false
	}
	for _, child := range t.selectedSubtaskTools() {
		if child.Status == toolview.StatusRunning {
			return false
		}
	}
	return true
}

func (t *TUI) renderSelectedSubtaskToolDetail(innerWidth int) []string {
	te := t.selectedSubtaskTool()
	if te == nil {
		return []string{styleMuted.Render(t.tr("tui.subtask_panel.no_tools"))}
	}
	deps := t.toolDetailDeps()
	deps.Width = max(44, innerWidth)
	source := toolview.DetailLineSource(te, deps)
	body, start, total := scroll.Window(source, t.subtaskToolDetailHeight(), &t.chat.SubtaskToolDetailScroll)
	if total == 0 {
		return []string{styleMuted.Render(t.tr("tui.subtask_panel.no_detail"))}
	}
	lines := append([]string(nil), body...)
	end := min(total, start+t.subtaskToolDetailHeight())
	lines = append(lines, styleMuted.Render(fmt.Sprintf("PgUp/PgDn/%s %s %d-%d/%d", t.tr("tui.subtask_panel.wheel"), t.tr("tui.overlay.scroll"), start+1, end, total)))
	return lines
}

func (t *TUI) subtaskSectionTitle(title string, width int) string {
	text := "─ " + title + " "
	return styleDim.Render(text + strings.Repeat("─", max(0, width-lipgloss.Width(text))))
}

func (t *TUI) subtaskPanelHelpKey() string {
	if t.chat.SubtaskToolDetailExpanded {
		return "tui.subtask_panel.help_expanded"
	}
	return "tui.subtask_panel.help"
}

func (t *TUI) subtaskToolDetailHeight() int {
	// 工具详情是可展开区域，不能在矮终端里挤掉上方的子任务列表和工具 timeline。
	return max(4, min(6, t.height/6))
}

func subtaskParamLabel(te *toolEntry, key string) string {
	return strings.Join(strings.Fields(subtaskParamText(te, key)), " ")
}

func subtaskParamText(te *toolEntry, key string) string {
	if te == nil || te.ParamsRaw == nil {
		return ""
	}
	value, ok := te.ParamsRaw[key]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

func (t *TUI) subtaskStatusCounts(ids []string) (done, running, failed int) {
	for _, id := range ids {
		switch te := t.findTool(id); {
		case te == nil:
		case te.Status == toolview.StatusDone:
			done++
		case te.Status == toolview.StatusError || te.Status == toolview.StatusCancelled:
			failed++
		default:
			running++
		}
	}
	return done, running, failed
}

func (t *TUI) subtaskBlockStatusIcon(done, running, failed, total int) string {
	if running > 0 {
		// 使用占位符，避免 spinner tick 触发全量 transcript 重建；viewChat() 统一替换。
		return spinnerPlaceholder
	}
	if failed > 0 {
		return "✗"
	}
	if total > 0 && done == total {
		return "✓"
	}
	return "◷"
}

func (t *TUI) subtaskStatusIcon(te *toolEntry) string {
	if te == nil {
		return styleDim.Render("◷")
	}
	switch te.Status {
	case toolview.StatusDone:
		return styleToolOk.Render("✓")
	case toolview.StatusError:
		return styleToolErr.Render("✗")
	case toolview.StatusCancelled:
		return styleDim.Render("⊘")
	default:
		return styleToolRun.Render(spinnerPlaceholder)
	}
}

func (t *TUI) subtaskActivity(te *toolEntry, width int) string {
	// 与 selectedSubtaskTools 同源：展开态（CurrentToolBlock 已置空）也要能显示工具活动。
	block := t.chat.ActiveSubtaskBlock()
	if te == nil || block == nil {
		return ""
	}
	if te.Status == toolview.StatusError {
		return t.subtaskFailureReason(te)
	}
	children := toolview.SubtaskChildren(block, te.ID)
	var latest *toolEntry
	for _, child := range children {
		if child.Status == toolview.StatusRunning {
			return t.subtaskActiveToolLabel(child, width)
		}
		latest = child
	}
	if latest != nil {
		return t.subtaskActiveToolLabel(latest, width)
	}
	if te.Status == toolview.StatusDone {
		return t.tr("tui.subtask_panel.done")
	}
	return t.tr("tui.subtask_panel.waiting")
}

func fitSubtaskRowParts(label, activity string, width int) (string, string) {
	label = strings.TrimSpace(label)
	activity = strings.TrimSpace(activity)
	if width <= 0 {
		return "", ""
	}
	if activity == "" {
		return textutil.TruncateRunes(label, width), ""
	}
	sepWidth := lipgloss.Width(" · ")
	if width <= sepWidth+4 {
		return textutil.TruncateRunes(label, width), ""
	}
	labelWidth := lipgloss.Width(label)
	activityWidth := lipgloss.Width(activity)
	if labelWidth+sepWidth+activityWidth <= width {
		return label, activity
	}
	// 子任务 intent 是主信息，工具活动是辅助信息；根据当前可用宽度动态分配，避免宽屏仍按固定半宽过早截断。
	minActivityWidth := min(20, max(8, width/4))
	labelMax := min(labelWidth, max(12, width-sepWidth-minActivityWidth))
	activityMax := width - sepWidth - labelMax
	if activityMax < 8 {
		return textutil.TruncateRunes(label, width), ""
	}
	return textutil.TruncateRunes(label, labelMax), textutil.TruncateRunes(activity, activityMax)
}

func (t *TUI) subtaskFailureReason(te *toolEntry) string {
	if te == nil {
		return ""
	}
	return toolview.ShortToolError(te.Result)
}

func subtaskDurationSep(dur string) string {
	if strings.HasPrefix(dur, " ") {
		return " ·"
	}
	return " · "
}

func (t *TUI) subtaskDuration(te *toolEntry) string {
	if te == nil {
		return ""
	}
	if te.Status == toolview.StatusRunning && !te.StartedAt.IsZero() {
		return liveElapsedPlaceholder(te.StartedAt)
	}
	if fixed := fixedToolDuration(te); fixed > 0 {
		return toolview.FormatCompactDuration(fixed)
	}
	return ""
}

func fixedToolDuration(te *toolEntry) time.Duration {
	if te == nil {
		return 0
	}
	if te.Duration > 0 {
		return te.Duration
	}
	if te.StartedAt.IsZero() || te.EndedAt.IsZero() || !te.EndedAt.After(te.StartedAt) {
		return 0
	}
	return te.EndedAt.Sub(te.StartedAt)
}

func (t *TUI) hasActiveSubtaskPanel() bool {
	return t.visibleSubtaskIDs() != nil
}

func (t *TUI) canToggleSubtaskDetailWithEnter() bool {
	return t.canUseSubtaskPanelKeys() &&
		len(t.chat.Attachments) == 0 &&
		len(t.chat.CmdSuggestions) == 0 &&
		!t.chat.HasBlockingInteraction()
}

// canUseSubtaskPanelKeys 判断 ↑↓/Tab 是否应交给 subtask 面板。
// 面板是随 run 自动出现的隐式模式，不能抢正在编辑的输入框：
// 草稿非空时 ↑↓ 必须留给文本光标，否则多行输入无法移动光标。
func (t *TUI) canUseSubtaskPanelKeys() bool {
	return t.hasActiveSubtaskPanel() && strings.TrimSpace(t.chat.Textarea.Value()) == ""
}

// canNavigateSubtaskTools 报告 ↑↓ 是否可以接管为子任务工具导航。
// 只有面板激活且确实有内部工具可切换时才接管，否则让位给输入历史：
// 就地展开不是模态，按键被消耗却没有任何可见反馈属于隐形操作。
func (t *TUI) canNavigateSubtaskTools() bool {
	return t.canUseSubtaskPanelKeys() && len(t.selectedSubtaskTools()) > 0
}

func (t *TUI) selectedSubtaskID() string {
	ids := t.visibleSubtaskIDs()
	if len(ids) == 0 {
		return ""
	}
	t.clampSubtaskCursor()
	return ids[t.chat.SubtaskCursor]
}

func (t *TUI) selectedSubtask() *toolEntry {
	return t.findTool(t.selectedSubtaskID())
}

func (t *TUI) selectedSubtaskTools() []*toolEntry {
	parent := t.selectedSubtask()
	// 用 ActiveSubtaskBlock 而不是 CurrentToolBlock：run 结束后块仍在 Messages 里，
	// 用户通过 Ctrl+T 展开 subtask 盒子查看结果时，内部工具列表也必须可见。
	block := t.chat.ActiveSubtaskBlock()
	if parent == nil || block == nil {
		return nil
	}
	return toolview.SubtaskChildren(block, parent.ID)
}

func (t *TUI) selectedSubtaskTool() *toolEntry {
	children := t.selectedSubtaskTools()
	if len(children) == 0 {
		return nil
	}
	if !t.chat.SubtaskToolCursorUserSet {
		t.chat.SubtaskToolCursor = t.defaultSubtaskToolCursor()
	}
	t.clampSubtaskToolCursor()
	return children[t.chat.SubtaskToolCursor]
}

func (t *TUI) moveSubtaskCursor(delta int) {
	ids := t.visibleSubtaskIDs()
	if len(ids) == 0 {
		t.chat.SubtaskCursor = 0
		t.chat.SubtaskCursorUserSet = false
		t.chat.SubtaskToolCursor = 0
		t.chat.SubtaskToolCursorUserSet = false
		t.chat.SubtaskToolDetailScroll = 0
		t.chat.SubtaskResultScroll = 0
		return
	}
	t.chat.SubtaskCursor += delta
	t.chat.SubtaskCursorUserSet = true
	if t.chat.SubtaskCursor < 0 {
		t.chat.SubtaskCursor = len(ids) - 1
	}
	if t.chat.SubtaskCursor >= len(ids) {
		t.chat.SubtaskCursor = 0
	}
	t.chat.SubtaskToolCursor = t.defaultSubtaskToolCursor()
	t.chat.SubtaskToolCursorUserSet = false
	t.chat.SubtaskToolDetailScroll = 0
	t.chat.SubtaskResultScroll = 0
}

func (t *TUI) moveSubtaskToolCursor(delta int) {
	children := t.selectedSubtaskTools()
	if len(children) == 0 {
		t.chat.SubtaskToolCursor = 0
		t.chat.SubtaskToolCursorUserSet = false
		t.chat.SubtaskToolDetailScroll = 0
		t.chat.SubtaskResultScroll = 0
		return
	}
	t.chat.SubtaskToolCursor += delta
	t.chat.SubtaskToolCursorUserSet = true
	if t.chat.SubtaskToolCursor < 0 {
		t.chat.SubtaskToolCursor = 0
	}
	if t.chat.SubtaskToolCursor >= len(children) {
		t.chat.SubtaskToolCursor = len(children) - 1
	}
	t.chat.SubtaskToolDetailScroll = 0
	t.chat.SubtaskResultScroll = 0
}

func (t *TUI) clampSubtaskCursor() {
	ids := t.visibleSubtaskIDs()
	if len(ids) == 0 {
		t.chat.SubtaskCursor = 0
		return
	}
	if t.chat.SubtaskCursor < 0 {
		t.chat.SubtaskCursor = 0
	}
	if t.chat.SubtaskCursor >= len(ids) {
		t.chat.SubtaskCursor = len(ids) - 1
	}
}

func (t *TUI) clampSubtaskToolCursor() {
	children := t.selectedSubtaskTools()
	if len(children) == 0 {
		t.chat.SubtaskToolCursor = 0
		return
	}
	if t.chat.SubtaskToolCursor < 0 {
		t.chat.SubtaskToolCursor = 0
	}
	if t.chat.SubtaskToolCursor >= len(children) {
		t.chat.SubtaskToolCursor = len(children) - 1
	}
}

func (t *TUI) ensureSubtaskSelection() {
	if !t.chat.SubtaskCursorUserSet {
		t.chat.SubtaskCursor = t.defaultSubtaskCursor()
	}
	t.clampSubtaskCursor()
	if !t.chat.SubtaskToolCursorUserSet {
		t.chat.SubtaskToolCursor = t.defaultSubtaskToolCursor()
	}
	t.clampSubtaskToolCursor()
}

func (t *TUI) defaultSubtaskCursor() int {
	ids := t.visibleSubtaskIDs()
	if len(ids) == 0 {
		return 0
	}
	for i, id := range ids {
		if te := t.findTool(id); te != nil && te.Status == toolview.StatusRunning {
			return i
		}
	}
	return 0
}

func (t *TUI) defaultSubtaskToolCursor() int {
	children := t.selectedSubtaskTools()
	if len(children) == 0 {
		return 0
	}
	lastDone := 0
	for i, child := range children {
		if child.Status == toolview.StatusRunning || child.Status == toolview.StatusCancelling {
			return i
		}
		if child.Status == toolview.StatusDone || child.Status == toolview.StatusError || child.Status == toolview.StatusCancelled {
			lastDone = i
		}
	}
	return lastDone
}

// scrollSubtaskToolDetail 滚动子任务工具详情窗口。
// 返回是否真正消费了本次滚动：已到边界时返回 false，让调用方透传给
// transcript（滚动链）。
func (t *TUI) scrollSubtaskToolDetail(delta int) bool {
	te := t.selectedSubtaskTool()
	if te == nil {
		t.chat.SubtaskToolDetailScroll = 0
		return false
	}
	deps := t.toolDetailDeps()
	source := toolview.DetailLineSource(te, deps)
	maxOffset := max(0, source.Len()-t.subtaskToolDetailHeight())
	next := clampInt(t.chat.SubtaskToolDetailScroll+delta, 0, maxOffset)
	if next == t.chat.SubtaskToolDetailScroll {
		return false
	}
	t.chat.SubtaskToolDetailScroll = next
	return true
}
