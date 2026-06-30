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

func (t *TUI) renderSubtaskBlock(block *toolBlock) string {
	if block == nil {
		return ""
	}
	ids := subtaskIDsInBlock(block)
	if len(ids) == 0 {
		return ""
	}
	active := block == t.chat.CurrentToolBlock
	if active {
		t.ensureSubtaskSelection()
	}
	width := max(40, t.width-8)
	innerWidth := max(24, width-8)
	sectionWidth := max(24, width-4)
	done, running, failed := t.subtaskStatusCounts(ids)
	title := fmt.Sprintf("%s "+t.tr("tui.subtask_panel.title"), t.subtaskBlockStatusIcon(done, running, failed, len(ids)), len(ids), running, done, failed)
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
			lines = append(lines, t.subtaskSectionTitle(t.tr("tui.subtask_panel.tools"), sectionWidth))
			lines = append(lines, t.renderSelectedSubtaskTools(innerWidth)...)
			if t.chat.SubtaskToolDetailExpanded {
				lines = append(lines, t.subtaskSectionTitle(t.tr("tui.subtask_panel.tool_detail"), sectionWidth))
				lines = append(lines, t.renderSelectedSubtaskToolDetail(innerWidth)...)
			}
		}
		lines = append(lines, styleDim.Render(t.tr(t.subtaskPanelHelpKey())))
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
			line += styleDim.Render(" · " + activity)
		}
		if dur != "" {
			line += styleDim.Render(subtaskDurationSep(dur) + dur)
		}
		rows = append(rows, line)
	}
	return rows
}

func (t *TUI) renderSelectedSubtaskSummary(te *toolEntry, innerWidth int) []string {
	label := textutil.TruncateRunes(toolview.PlainIntentLabel(te), max(12, innerWidth))
	parts := []string{styleHL.Render(label)}
	if te != nil && te.Status == toolview.StatusError {
		if reason := t.subtaskFailureReason(te); reason != "" {
			parts = append(parts, styleDim.Render(t.tr("tui.subtask_panel.error")+": ")+styleToolErr.Render(textutil.TruncateRunes(reason, max(12, innerWidth-8))))
		}
	}
	if model := subtaskParamLabel(te, "model"); model != "" {
		parts = append(parts, styleDim.Render(t.tr("tui.tool.model")+": ")+styleToolDim.Render(textutil.TruncateRunes(model, max(10, innerWidth-8))))
	}
	if tools := subtaskParamLabel(te, "tools"); tools != "" {
		parts = append(parts, styleDim.Render(t.tr("tui.tool.tools")+": ")+styleToolDim.Render(textutil.TruncateRunes(tools, max(10, innerWidth-8))))
	}
	if task := subtaskParamLabel(te, "task"); task != "" {
		parts = append(parts, styleDim.Render(t.tr("tui.tool.task")+": ")+styleToolDim.Render(textutil.TruncateRunes(task, max(12, innerWidth-8))))
	}
	return parts
}

func (t *TUI) renderSelectedSubtaskTools(innerWidth int) []string {
	children := t.selectedSubtaskTools()
	if len(children) == 0 {
		if t.selectedSubtaskWaitingForTool() {
			return []string{styleToolRun.Render(spinnerPlaceholder+" ") + styleDim.Render(t.tr("tui.subtask_panel.waiting_tool"))}
		}
		return []string{styleDim.Render(t.tr("tui.subtask_panel.no_tools"))}
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
		rows = append(rows, styleDim.Render(fmt.Sprintf(t.tr("tui.subtask_panel.more_above"), start)))
	}
	for i := start; i < end; i++ {
		child := children[i]
		cursor := "  "
		labelStyle := lipgloss.NewStyle()
		if i == t.chat.SubtaskToolCursor {
			cursor = styleCursor.Render("▶ ")
			labelStyle = styleHL
		}
		icon := t.subtaskStatusIcon(child)
		prefixWidth := 4 // cursor(2) + icon(1) + space(1)
		dur := ""
		if child.Status == toolview.StatusRunning && !child.StartedAt.IsZero() {
			dur = liveElapsedPlaceholder(child.StartedAt)
		} else if fixed := fixedToolDuration(child); fixed > 0 {
			dur = fmt.Sprintf("%.1fs", fixed.Seconds())
		}
		durWidth := 0
		if dur != "" {
			durWidth = lipgloss.Width(subtaskDurationSep(dur) + dur)
		}
		remaining := max(0, innerWidth-prefixWidth-durWidth)
		label := t.subtaskToolTimelineLabel(child, max(4, remaining))
		line := fmt.Sprintf("%s%s %s", cursor, icon, labelStyle.Render(label))
		if dur != "" {
			line += styleDim.Render(subtaskDurationSep(dur) + dur)
		}
		rows = append(rows, line)
	}
	if end < len(children) {
		rows = append(rows, styleDim.Render(fmt.Sprintf(t.tr("tui.subtask_panel.more_below"), len(children)-end)))
	} else if t.selectedSubtaskWaitingForTool() {
		rows = append(rows, styleToolRun.Render(spinnerPlaceholder)+styleDim.Render(" "+t.tr("tui.subtask_panel.waiting_tool")))
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
		return []string{styleDim.Render(t.tr("tui.subtask_panel.no_tools"))}
	}
	deps := t.toolDetailDeps()
	deps.Width = max(44, innerWidth)
	source := toolview.DetailLineSource(te, deps)
	body, start, total := scroll.Window(source, t.subtaskToolDetailHeight(), &t.chat.SubtaskToolDetailScroll)
	if total == 0 {
		return []string{styleDim.Render(t.tr("tui.subtask_panel.no_detail"))}
	}
	lines := append([]string(nil), body...)
	end := min(total, start+t.subtaskToolDetailHeight())
	lines = append(lines, styleDim.Render(fmt.Sprintf("PgUp/PgDn/%s %s %d-%d/%d", t.tr("tui.subtask_panel.wheel"), t.tr("tui.overlay.scroll"), start+1, end, total)))
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
		case te.Status == toolview.StatusError:
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
	default:
		return styleToolRun.Render(spinnerPlaceholder)
	}
}

func (t *TUI) subtaskActivity(te *toolEntry, width int) string {
	if te == nil || t.chat.CurrentToolBlock == nil {
		return ""
	}
	if te.Status == toolview.StatusError {
		return t.subtaskFailureReason(te)
	}
	children := toolview.SubtaskChildren(t.chat.CurrentToolBlock, te.ID)
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
		return fmt.Sprintf("%.1fs", fixed.Seconds())
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

func (t *TUI) toggleToolDetail() {
	t.chat.ToggleToolDetail(t.visibleToolIDs())
}

func (t *TUI) hasActiveSubtaskPanel() bool {
	return len(t.visibleSubtaskIDs()) > 0
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
	if parent == nil || t.chat.CurrentToolBlock == nil {
		return nil
	}
	return toolview.SubtaskChildren(t.chat.CurrentToolBlock, parent.ID)
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
}

func (t *TUI) moveSubtaskToolCursor(delta int) {
	children := t.selectedSubtaskTools()
	if len(children) == 0 {
		t.chat.SubtaskToolCursor = 0
		t.chat.SubtaskToolCursorUserSet = false
		t.chat.SubtaskToolDetailScroll = 0
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
		if child.Status == toolview.StatusRunning {
			return i
		}
		if child.Status == toolview.StatusDone || child.Status == toolview.StatusError {
			lastDone = i
		}
	}
	return lastDone
}

func (t *TUI) scrollSubtaskToolDetail(delta int) {
	te := t.selectedSubtaskTool()
	if te == nil {
		t.chat.SubtaskToolDetailScroll = 0
		return
	}
	deps := t.toolDetailDeps()
	source := toolview.DetailLineSource(te, deps)
	maxOffset := max(0, source.Len()-t.subtaskToolDetailHeight())
	t.chat.SubtaskToolDetailScroll += delta
	if t.chat.SubtaskToolDetailScroll < 0 {
		t.chat.SubtaskToolDetailScroll = 0
	}
	if t.chat.SubtaskToolDetailScroll > maxOffset {
		t.chat.SubtaskToolDetailScroll = maxOffset
	}
}
