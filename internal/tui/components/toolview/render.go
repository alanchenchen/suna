package toolview

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	textutil "github.com/alanchenchen/suna/internal/tui/components/text"
)

// RenderStyles 是工具 transcript 组件需要的样式集合。
// 组件包不读取 root TUI 的全局样式，所有视觉依赖都由调用方注入。
type RenderStyles struct {
	Dim       lipgloss.Style
	HL        lipgloss.Style
	OK        lipgloss.Style
	Err       lipgloss.Style
	Run       lipgloss.Style
	ToolDim   lipgloss.Style
	Intent    lipgloss.Style
	MetaPill  lipgloss.Style
	GuardOK   lipgloss.Style
	GuardWarn lipgloss.Style
	GuardErr  lipgloss.Style
	FilePath  lipgloss.Style
}

// RenderLabels 是工具组件展示文案。i18n 留在 root/page，组件只消费最终字符串。
type RenderLabels struct {
	Tools                string
	Subtask              string
	GuardBadge           string
	GuardUnknown         string
	FileBadge            string
	Actions              string
	FilesChanged         string
	FSChanges            string
	Guarded              string
	FSBadge              string
	FSDeleted            string
	FSCreatedDir         string
	FSMoved              string
	FSCopied             string
	Recursive            string
	Overwrote            string
	Entries              string
	SearchMatchesInFiles string
	SearchScanned        string
	SearchTruncated      string
}

// RenderDeps 汇总工具块渲染所需依赖。
type RenderDeps struct {
	Width   int
	Spinner string
	Labels  RenderLabels
	Styles  RenderStyles

	GuardDecisionLabel func(*GuardInfo) string
	RiskLabel          func(string) string
}

func (d RenderDeps) width() int {
	if d.Width <= 0 {
		return 80
	}
	return d.Width
}

// RenderBlock 渲染一个连续工具调用块。调用方负责将结果嵌入 Chat transcript。
func RenderBlock(block *Block, deps RenderDeps) string {
	if block == nil || len(block.Order) == 0 {
		return ""
	}
	entries := VisibleMainEntries(block)
	if len(entries) == 0 {
		return ""
	}
	var lines []string
	for _, te := range entries {
		entryLines := strings.Split(strings.TrimSuffix(RenderEntry(te, false, deps), "\n"), "\n")
		for _, line := range entryLines {
			lines = append(lines, trimToolBlockIndent(line))
		}
	}
	return renderToolTitledBox(deps.width(), toolBlockTitleWithStatus(entries, deps), lines, deps.Styles)
}

func trimToolBlockIndent(line string) string {
	// 工具条目原本为了独立渲染带有左缩进；放进圆角容器后去掉整层缩进，避免边框内留白过大。
	line = strings.TrimPrefix(line, "      ")
	return strings.TrimPrefix(line, "    ")
}

func toolBlockTitleWithStatus(entries []*Entry, deps RenderDeps) string {
	return strings.TrimSpace(toolBlockStatusIcon(entries, deps) + " " + BlockTitle(entries, deps.Labels))
}

func toolBlockStatusIcon(entries []*Entry, deps RenderDeps) string {
	if len(entries) == 0 {
		return ""
	}
	running := 0
	failed := 0
	for _, te := range entries {
		if te == nil {
			continue
		}
		switch te.Status {
		case StatusRunning:
			running++
		case StatusError:
			failed++
		}
	}
	if running > 0 {
		if strings.TrimSpace(deps.Spinner) != "" {
			return deps.Spinner
		}
		return "◐"
	}
	if failed > 0 {
		return "✗"
	}
	return "✓"
}

func runningStatusIcon(deps RenderDeps) string {
	if strings.TrimSpace(deps.Spinner) != "" {
		return deps.Spinner
	}
	return "◐"
}

func renderToolTitledBox(width int, title string, lines []string, styles RenderStyles) string {
	maxOuterWidth := maxInt(20, width-4)
	maxContentWidth := maxInt(8, maxOuterWidth-2)
	title = textutil.TruncateRunes(strings.TrimSpace(title), maxInt(4, maxContentWidth-3))
	titlePrefix := "─ "
	titleSuffix := " "
	titleWidth := lipgloss.Width(titlePrefix) + lipgloss.Width(title) + lipgloss.Width(titleSuffix)
	contentWidth := maxInt(8, titleWidth)
	for _, line := range lines {
		contentWidth = maxInt(contentWidth, lipgloss.Width(line)+2)
	}
	contentWidth = minInt(maxContentWidth, contentWidth)
	topRest := strings.Repeat("─", maxInt(0, contentWidth-titleWidth))
	top := styles.Dim.Render("╭"+titlePrefix) + styles.HL.Render(title) + styles.Dim.Render(titleSuffix+topRest+"╮")
	bottom := styles.Dim.Render("╰" + strings.Repeat("─", contentWidth) + "╯")
	out := []string{top}
	for _, line := range lines {
		content := ansi.Truncate(" "+line+" ", contentWidth, "…")
		pad := strings.Repeat(" ", maxInt(0, contentWidth-lipgloss.Width(content)))
		out = append(out, styles.Dim.Render("│")+content+pad+styles.Dim.Render("│"))
	}
	out = append(out, bottom)
	return strings.Join(out, "\n") + "\n"
}

// RenderEntry 渲染单个工具调用摘要。纯组件渲染只能依赖 Entry 和 RenderDeps。
func RenderEntry(te *Entry, nested bool, deps RenderDeps) string {
	if te == nil {
		return ""
	}
	var statusIcon string
	var dur string
	if te.Duration > 0 {
		dur = fmt.Sprintf(" %.1fs", te.Duration.Seconds())
	}
	s := deps.Styles
	switch te.Status {
	case StatusDone:
		statusIcon = s.OK.Render("✓")
	case StatusError:
		statusIcon = s.Err.Render("✗")
	default:
		statusIcon = s.Run.Render(runningStatusIcon(deps))
	}
	prefix := "      "
	if nested {
		prefix = "      " + s.Dim.Render("└─ ")
	}
	maxWidth := maxInt(20, deps.width()-lipgloss.Width(stripANSI(prefix))-8)
	mainLabel, detailLabel := entryLabels(te, maxWidth, deps)
	// 首行必须为耗时预留空间，避免长命令或长路径把右侧 duration 挤出可视区域。
	durWidth := lipgloss.Width(dur)
	statusWidth := lipgloss.Width(statusIcon)
	headerBudget := maxInt(8, maxWidth-statusWidth-durWidth-2)
	mainLabel = ansi.Truncate(mainLabel, headerBudget, "…")
	line := fmt.Sprintf("%s%s %s%s", prefix, statusIcon, s.HL.Render(mainLabel), s.Dim.Render(dur))
	if detailLabel != "" {
		for _, detail := range splitWrappedStyle(detailLabel, maxWidth, 2, s.Intent, s) {
			line += "\n" + prefix + "  " + detail
		}
	}
	if te.Status == StatusError {
		if err := ShortToolError(te.Result); err != "" {
			line += "\n" + prefix + "  " + s.Err.Render(textutil.TruncateRunes(err, maxInt(24, deps.width()-12)))
		}
	}
	if te.Status == StatusDone {
		if summary := renderMetadataSummary(te, prefix, deps); summary != "" {
			line += "\n" + summary
		}
	}
	if summary := renderGuardSummary(te.Guard, prefix, deps); summary != "" {
		line += "\n" + summary
	}
	return line + "\n"
}

func entryLabels(te *Entry, maxWidth int, deps RenderDeps) (string, string) {
	label := DisplayIntentLabel(te, deps.Labels.Subtask)
	if HasFileChange(te) {
		if path, _ := te.Metadata["path"].(string); path != "" {
			main := te.Name + " " + CompactPath(path, maxInt(12, maxWidth-lipgloss.Width(te.Name)-1))
			if strings.TrimSpace(label) != "" && strings.TrimSpace(label) != main {
				return main, label
			}
			return main, ""
		}
	}
	if summary := SemanticSummary(te, maxWidth, deps.Labels); summary != "" {
		main := te.Name + " " + summary
		if strings.TrimSpace(label) != "" && strings.TrimSpace(label) != main {
			return main, label
		}
		return main, ""
	}
	return label, ""
}

// DisplayIntentLabel 返回工具主标签。subtask 文案由调用方注入，避免组件包持有 i18n。
func DisplayIntentLabel(te *Entry, subtaskLabel string) string {
	label := PlainIntentLabel(te)
	if IsSubtask(te) {
		if model := SubtaskModelLabel(te); model != "" {
			return fmt.Sprintf("%s [%s] · %s", subtaskLabel, model, label)
		}
		return subtaskLabel + " · " + label
	}
	return label
}

func SubtaskModelLabel(te *Entry) string {
	if te == nil || te.ParamsRaw == nil {
		return ""
	}
	model, ok := te.ParamsRaw["model"]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", model))
}

func PlainIntentLabel(te *Entry) string {
	if te == nil {
		return ""
	}
	if intent := strings.TrimSpace(te.Intent); intent != "" {
		return intent
	}
	if strings.TrimSpace(te.Summary) != "" {
		return te.Name + " " + strings.TrimSpace(te.Summary)
	}
	return te.Name
}

func ShortToolError(result string) string {
	result = strings.TrimSpace(result)
	if result == "" {
		return "tool failed"
	}
	for _, line := range strings.Split(result, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return "tool failed"
}

func renderGuardSummary(info *GuardInfo, prefix string, deps RenderDeps) string {
	if !ShouldShowGuardSummary(info) {
		return ""
	}
	s := deps.Styles
	decision := deps.Labels.GuardUnknown
	if deps.GuardDecisionLabel != nil {
		decision = deps.GuardDecisionLabel(info)
	}
	risk := ""
	if deps.RiskLabel != nil && info != nil {
		risk = deps.RiskLabel(info.Risk)
	}
	parts := []string{s.Dim.Render(deps.Labels.GuardBadge), renderGuardDecisionBadge(info, decision, s)}
	if risk != "" {
		parts = append(parts, risk)
	}
	if reason := shortGuardReason(info.Reason); reason != "" {
		parts = append(parts, s.ToolDim.Render(reason))
	}
	return prefix + "  " + s.Dim.Render("↳ ") + strings.Join(parts, "  ")
}

func renderGuardDecisionBadge(info *GuardInfo, label string, s RenderStyles) string {
	if info == nil {
		return s.MetaPill.Render(label)
	}
	source := strings.ToLower(info.Source)
	decision := strings.ToLower(info.Decision)
	if decision == "reject" || strings.Contains(label, "blocked") || strings.Contains(label, "拒绝") || strings.Contains(label, "阻止") {
		return s.GuardErr.Render(label)
	}
	if decision == "confirm" || decision == "modify" || source == "fallback" || (decision == "approve" && strings.ToLower(info.Risk) != "low" && source == "static") {
		return s.GuardWarn.Render(label)
	}
	if decision == "approve" {
		return s.GuardOK.Render(label)
	}
	return s.MetaPill.Render(label)
}

func shortGuardReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ""
	}
	return textutil.TruncateRunes(reason, 64)
}

func renderMetadataSummary(te *Entry, prefix string, deps RenderDeps) string {
	if te == nil || te.Metadata == nil {
		return ""
	}
	if kind, _ := te.Metadata["kind"].(string); kind == "file_change" {
		return RenderFileChangeSummary(te.Metadata, prefix, deps)
	}
	if kind, _ := te.Metadata["kind"].(string); kind == "fs_change" {
		return RenderFSChangeSummary(te.Metadata, prefix, deps)
	}
	if kind, _ := te.Metadata["kind"].(string); kind == "search_result" {
		return RenderSearchSummary(te.Metadata, prefix, deps)
	}
	if kind, _ := te.Metadata["kind"].(string); kind == "http_response" {
		return RenderHTTPSummary(te.Metadata, prefix, deps)
	}
	return ""
}

// RenderFileChangeSummary 渲染 file_change metadata；不解析 tool result 文本，避免 UI 绑定 LLM 文案。
func RenderFileChangeSummary(metadata map[string]any, prefix string, deps RenderDeps) string {
	operation, _ := metadata["operation"].(string)
	if operation == "" {
		return ""
	}
	added := MetadataInt(metadata["added_lines"])
	removed := MetadataInt(metadata["removed_lines"])
	replacements := MetadataInt(metadata["replacements"])
	sizeBefore, hasBefore := MetadataIntOK(metadata["size_before"])
	sizeAfter, hasAfter := MetadataIntOK(metadata["size_after"])

	s := deps.Styles
	arrow := s.Dim.Render("↳ ")
	parts := []string{s.Dim.Render(deps.Labels.FileBadge), renderFileChangeStatus(operation, s), renderLineDelta("+", added, true, s), renderLineDelta("-", removed, false, s)}
	if replacements > 0 {
		parts = append(parts, s.GuardWarn.Render(fmt.Sprintf("%d repl", replacements)))
	}
	if hasAfter {
		if hasBefore && sizeBefore != sizeAfter {
			parts = append(parts, s.ToolDim.Render(fmt.Sprintf("%s → %s", FormatTinyBytes(sizeBefore), FormatTinyBytes(sizeAfter))))
		} else if !hasBefore || operation == "created" {
			parts = append(parts, s.ToolDim.Render(FormatTinyBytes(sizeAfter)))
		}
	}

	return prefix + "  " + arrow + strings.Join(parts, "  ")
}

func renderFileChangeStatus(operation string, s RenderStyles) string {
	label := strings.ToUpper(operation)
	switch operation {
	case "created":
		return s.GuardOK.Render(label)
	case "updated":
		return s.MetaPill.Render(label)
	case "deleted":
		return s.GuardErr.Render(label)
	case "unchanged":
		return s.ToolDim.Render(operation)
	default:
		return s.ToolDim.Render(operation)
	}
}

func renderLineDelta(prefix string, n int, added bool, s RenderStyles) string {
	text := fmt.Sprintf("%s%d", prefix, n)
	if n == 0 {
		return s.ToolDim.Render(text)
	}
	if added {
		return s.GuardOK.Render(text)
	}
	return s.GuardErr.Render(text)
}

func splitWrapped(content string, width int, maxLines int, s RenderStyles) []string {
	return splitWrappedStyle(content, width, maxLines, s.ToolDim, s)
}

func splitWrappedStyle(content string, width int, maxLines int, style lipgloss.Style, s RenderStyles) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimRight(content, "\n"), "\n") {
		remaining := 0
		if maxLines > 0 {
			remaining = maxLines - len(out)
			if remaining <= 0 {
				return append(out, s.Dim.Render("..."))
			}
		}
		for _, wrapped := range textutil.WrapLineLimit(line, width, remaining) {
			out = append(out, style.Render(wrapped))
			if maxLines > 0 && len(out) >= maxLines {
				return append(out, s.Dim.Render("..."))
			}
		}
	}
	return out
}

func stripANSI(s string) string { return s }

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func topLevelEntries(block *Block) []*Entry {
	if block == nil {
		return nil
	}
	entries := make([]*Entry, 0, len(block.Order))
	for _, id := range block.Order {
		te := block.Entries[id]
		if te == nil || te.ParentID != "" {
			continue
		}
		entries = append(entries, te)
	}
	for _, id := range block.Order {
		te := block.Entries[id]
		if te == nil || te.ParentID == "" || block.Entries[te.ParentID] != nil {
			continue
		}
		entries = append(entries, te)
	}
	return entries
}

func childEntries(block *Block, parentID string) []*Entry {
	return SubtaskChildren(block, parentID)
}
