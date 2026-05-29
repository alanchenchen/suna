package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

func (b *toolBlock) add(te *toolEntry) {
	if b.entries == nil {
		b.entries = make(map[string]*toolEntry)
	}
	if _, exists := b.entries[te.id]; !exists {
		b.order = append(b.order, te.id)
	}
	b.entries[te.id] = te
}

func (t *TUI) ensureToolBlock() *toolBlock {
	if t.canAppendToCurrentToolBlock() {
		return t.currentToolBlock
	}
	block := &toolBlock{entries: make(map[string]*toolEntry)}
	t.currentToolBlock = block
	t.messages = append(t.messages, chatMsg{role: "tool", content: block})
	return block
}

func (t *TUI) canAppendToCurrentToolBlock() bool {
	if t.currentToolBlock == nil || len(t.messages) == 0 {
		return false
	}
	last := t.messages[len(t.messages)-1]
	if last.role != "tool" {
		return false
	}
	block, ok := last.content.(*toolBlock)
	return ok && block == t.currentToolBlock
}

func parseSubtaskToolID(id string) (string, string) {
	const prefix = "spawn:"
	if !strings.HasPrefix(id, prefix) {
		return "", id
	}
	rest := strings.TrimPrefix(id, prefix)
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) != 2 {
		return "", id
	}
	return parts[0], parts[1]
}

func (t *TUI) hasRunningTools() bool {
	for _, te := range t.activeTools {
		if te.status == toolRunning {
			return true
		}
	}
	return false
}

func (t *TUI) renderToolBlock(block *toolBlock) string {
	if block == nil || len(block.order) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("    " + styleDim.Render(t.tr("tui.tool.tools")) + "\n")
	for _, id := range block.order {
		te := block.entries[id]
		if te == nil || te.parentID != "" {
			continue
		}
		sb.WriteString(t.renderToolEntry(te, false))
		for _, childID := range block.order {
			child := block.entries[childID]
			if child == nil || child.parentID != te.id {
				continue
			}
			sb.WriteString(t.renderToolEntry(child, true))
		}
	}
	for _, id := range block.order {
		te := block.entries[id]
		if te == nil || te.parentID == "" || block.entries[te.parentID] != nil {
			continue
		}
		sb.WriteString(t.renderToolEntry(te, true))
	}
	return sb.String()
}

func (t *TUI) renderToolEntry(te *toolEntry, nested bool) string {
	var statusIcon string
	var dur string
	if te.duration > 0 {
		dur = fmt.Sprintf(" %.1fs", te.duration.Seconds())
	}
	switch te.status {
	case toolDone:
		statusIcon = styleToolOk.Render("✓")
	case toolError:
		statusIcon = styleToolErr.Render("✗")
	default:
		statusIcon = styleToolRun.Render("◐")
	}
	prefix := "      "
	if nested {
		prefix = "      " + styleDim.Render("└─ ")
	}
	label := t.displayToolIntentLabel(te)
	maxWidth := max(20, t.width-lipgloss.Width(stripANSI(prefix))-8)
	maxLines := 2
	if isSubtask(te) {
		maxLines = 3
	}
	wrapped := wrapLineLimit(label, maxWidth, maxLines)
	if len(wrapped) > maxLines {
		wrapped = wrapped[:maxLines]
	}
	line := fmt.Sprintf("%s%s %s%s", prefix, statusIcon, styleToolDim.Render(wrapped[0]), styleDim.Render(dur))
	for _, extra := range wrapped[1:] {
		line += "\n" + prefix + "  " + styleToolDim.Render(extra)
	}
	if te.status == toolError {
		err := shortToolError(te.result)
		if err != "" {
			line += "\n" + prefix + "  " + styleToolErr.Render(truncateRunes(err, max(24, t.width-12)))
		}
	}
	if summary := t.renderGuardSummary(te.guard, prefix); summary != "" {
		line += "\n" + summary
	}
	if te.status == toolDone {
		if summary := t.renderToolMetadataSummary(te, prefix); summary != "" {
			line += "\n" + summary
		}
	}
	return line + "\n"
}

func (t *TUI) renderGuardSummary(info *guardInfo, prefix string) string {
	if info == nil {
		return ""
	}
	parts := []string{styleMetaPill.Render(t.tr("tui.tool.guard.badge")), t.renderGuardDecisionBadge(info), t.renderRiskBadge(info.risk)}
	if reason := shortGuardReason(info.reason); reason != "" {
		parts = append(parts, styleToolDim.Render(reason))
	}
	return prefix + "  " + styleDim.Render("↳ ") + strings.Join(parts, "  ")
}

func (t *TUI) renderGuardDecisionBadge(info *guardInfo) string {
	label := t.guardDecisionLabel(info)
	switch info.decision {
	case "approve":
		return styleGuardOK.Render(label)
	case "reject":
		return styleGuardErr.Render(label)
	case "confirm", "modify":
		return styleGuardWarn.Render(label)
	default:
		if info.source == "fallback" {
			return styleGuardWarn.Render(label)
		}
		return styleMetaPill.Render(label)
	}
}

func (t *TUI) guardDecisionLabel(info *guardInfo) string {
	if info == nil {
		return t.tr("tui.tool.guard.unknown")
	}
	switch info.source {
	case "llm":
		switch info.decision {
		case "approve":
			return t.tr("tui.tool.guard.llm_approved")
		case "reject":
			return t.tr("tui.tool.guard.llm_blocked")
		case "modify":
			return t.tr("tui.tool.guard.llm_suggested")
		case "confirm":
			return t.tr("tui.tool.guard.llm_confirm")
		}
	case "user":
		if info.decision == "reject" {
			return t.tr("tui.tool.guard.user_rejected")
		}
		return t.tr("tui.tool.guard.user_approved")
	case "rule":
		if info.decision == "reject" {
			return t.tr("tui.tool.guard.rule_blocked")
		}
		return t.tr("tui.tool.guard.rule_approved")
	case "static":
		if info.decision == "reject" {
			return t.tr("tui.tool.guard.policy_blocked")
		}
		return t.tr("tui.tool.guard.auto_approved")
	case "fallback":
		return t.tr("tui.tool.guard.review_unavailable")
	}
	return info.decision
}

func (t *TUI) renderRiskBadge(risk string) string {
	switch risk {
	case "high":
		return styleGuardErr.Render(t.tr("tui.tool.guard.risk.high"))
	case "medium":
		return styleGuardWarn.Render(t.tr("tui.tool.guard.risk.medium"))
	case "low":
		return styleGuardOK.Render(t.tr("tui.tool.guard.risk.low"))
	default:
		return styleMetaPill.Render(risk)
	}
}

func shortGuardReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ""
	}
	return truncateRunes(reason, 64)
}

func (t *TUI) renderToolMetadataSummary(te *toolEntry, prefix string) string {
	if te == nil || te.metadata == nil {
		return ""
	}
	if kind, _ := te.metadata["kind"].(string); kind == "file_change" {
		return t.renderFileChangeSummary(te.metadata, prefix)
	}
	return ""
}

// file_change metadata 用于主工具块的一行结果条；不解析 tool result 文本，避免 UI 绑定 LLM 文案。
func (t *TUI) renderFileChangeSummary(metadata map[string]any, prefix string) string {
	path, _ := metadata["path"].(string)
	operation, _ := metadata["operation"].(string)
	if path == "" || operation == "" {
		return ""
	}
	added := metadataInt(metadata["added_lines"])
	removed := metadataInt(metadata["removed_lines"])
	replacements := metadataInt(metadata["replacements"])
	sizeBefore, hasBefore := metadataIntOK(metadata["size_before"])
	sizeAfter, hasAfter := metadataIntOK(metadata["size_after"])

	arrow := styleDim.Render("↳ ")
	status := renderFileChangeStatus(operation)
	add := renderLineDelta("+", added, true)
	del := renderLineDelta("-", removed, false)
	parts := []string{status, add, del}
	plainParts := []string{operation, fmt.Sprintf("+%d", added), fmt.Sprintf("-%d", removed)}
	if replacements > 0 {
		parts = append(parts, styleToolDim.Render(fmt.Sprintf("%d repl", replacements)))
		plainParts = append(plainParts, fmt.Sprintf("%d repl", replacements))
	}
	if hasAfter {
		if hasBefore && sizeBefore != sizeAfter {
			size := fmt.Sprintf("%s → %s", formatTinyBytes(sizeBefore), formatTinyBytes(sizeAfter))
			parts = append(parts, styleToolDim.Render(size))
			plainParts = append(plainParts, size)
		} else if !hasBefore || operation == "created" {
			size := formatTinyBytes(sizeAfter)
			parts = append(parts, styleToolDim.Render(size))
			plainParts = append(plainParts, size)
		}
	}

	maxWidth := max(24, t.width-lipgloss.Width(stripANSI(prefix))-8)
	metaWidth := lipgloss.Width(strings.Join(plainParts, "  "))
	pathWidth := max(10, maxWidth-metaWidth-4)
	pathText := styleFilePath.Render(compactPath(path, pathWidth))
	return prefix + "  " + arrow + styleMetaPill.Render(t.tr("tui.tool.file.badge")) + "  " + pathText + "  " + strings.Join(parts, "  ")
}

func renderFileChangeStatus(operation string) string {
	switch operation {
	case "created":
		return styleGuardOK.Render("created")
	case "updated":
		return styleMetaPill.Render("updated")
	case "unchanged":
		return styleToolDim.Render("unchanged")
	default:
		return styleToolDim.Render(operation)
	}
}

func renderLineDelta(prefix string, n int, added bool) string {
	text := fmt.Sprintf("%s%d", prefix, n)
	if n == 0 {
		return styleToolDim.Render(text)
	}
	if added {
		return styleGuardOK.Render(text)
	}
	return styleGuardErr.Render(text)
}

func compactPath(path string, maxWidth int) string {
	if maxWidth <= 0 || lipgloss.Width(path) <= maxWidth {
		return path
	}
	base := path
	if idx := strings.LastIndexAny(path, "/\\"); idx >= 0 && idx < len(path)-1 {
		base = path[idx+1:]
	}
	if lipgloss.Width(base)+1 <= maxWidth {
		return "…" + base
	}
	return truncateRunes(base, maxWidth)
}

func formatTinyBytes(n int) string {
	if n >= 1024*1024 {
		return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
	}
	if n >= 1024 {
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	}
	return fmt.Sprintf("%dB", n)
}

func metadataInt(value any) int {
	n, _ := metadataIntOK(value)
	return n
}

func metadataIntOK(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case float32:
		return int(v), true
	case json.Number:
		i, err := v.Int64()
		if err == nil {
			return int(i), true
		}
	}
	return 0, false
}

func stripANSI(s string) string { return s }

func isSubtask(te *toolEntry) bool {
	return te != nil && te.rawName == "spawn" && te.parentID == ""
}

func isSubtaskChild(te *toolEntry) bool {
	return te != nil && te.parentID != ""
}

func (t *TUI) displayToolIntentLabel(te *toolEntry) string {
	label := plainToolIntentLabel(te)
	if isSubtask(te) {
		return t.tr("tui.tool.subtask") + " · " + label
	}
	return label
}

func plainToolIntentLabel(te *toolEntry) string {
	intent := strings.TrimSpace(te.intent)
	if intent != "" {
		return intent
	}
	if strings.TrimSpace(te.summary) != "" {
		return te.name + " " + strings.TrimSpace(te.summary)
	}
	return te.name
}

func shortToolError(result string) string {
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

func (t *TUI) renderToolDetailOverlay(width int) string {
	te := t.findTool(t.selectedToolID)
	if te == nil {
		return ""
	}
	w := max(44, min(104, width-4))
	body, bodyHeight := t.toolDetailBodyLines()
	body, start, total := scrollWindow(body, bodyHeight, &t.toolDetailScroll)
	lines := append([]string(nil), body...)
	lines = append(lines, "", styleDim.Render(t.toolDetailHelpText(start, bodyHeight, total)))
	return boxStyle.Width(w).Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func (t *TUI) toolDetailBodyLines() ([]string, int) {
	te := t.findTool(t.selectedToolID)
	if te == nil {
		return nil, 1
	}
	w := max(44, min(104, t.width-4))
	inner := max(24, w-8)
	bodyHeight := max(1, t.overlayMaxHeight()-7)
	var lines []string
	title := t.tr("tui.tool.detail_title")
	if isSubtask(te) {
		title = t.tr("tui.tool.subtask_detail_title")
	} else if isSubtaskChild(te) {
		title = t.tr("tui.tool.subtask_tool_detail_title")
	}
	idx, total := t.selectedToolPosition()
	if total > 0 {
		title += fmt.Sprintf(" · %d/%d", idx+1, total)
	}
	lines = append(lines, styleHL.Render(title))
	lines = append(lines, styleDim.Render(t.tr("tui.tool.tool")+": ")+styleTool.Render(te.rawName))
	if strings.TrimSpace(te.intent) != "" {
		lines = append(lines, styleDim.Render(t.tr("tui.tool.intent")))
		lines = append(lines, splitWrapped(te.intent, inner, 0)...)
	}
	if isSubtask(te) {
		t.appendSubtaskParams(&lines, te, inner)
	} else if te.params != "" {
		lines = append(lines, "", styleDim.Render(t.tr("tui.tool.params")))
		lines = append(lines, splitWrapped(te.params, inner, 0)...)
	}
	if te.guard != nil {
		lines = append(lines, "", styleDim.Render(t.tr("tui.tool.guard")))
		lines = append(lines, styleDim.Render(t.tr("tui.tool.guard.decision"))+" "+t.renderGuardDecisionBadge(te.guard))
		lines = append(lines, styleDim.Render(t.tr("tui.tool.guard.risk"))+" "+t.renderRiskBadge(te.guard.risk))
		if te.guard.source != "" {
			lines = append(lines, styleDim.Render(t.tr("tui.tool.guard.source"))+" "+styleToolDim.Render(te.guard.source))
		}
		if strings.TrimSpace(te.guard.reason) != "" {
			lines = append(lines, styleDim.Render(t.tr("tui.tool.guard.reason")))
			lines = append(lines, splitWrapped(te.guard.reason, inner, 0)...)
		}
		if strings.TrimSpace(te.guard.suggestion) != "" {
			lines = append(lines, styleDim.Render(t.tr("tui.tool.guard.suggestion")))
			lines = append(lines, splitWrapped(te.guard.suggestion, inner, 0)...)
		}
	}
	if te.result != "" {
		meta := t.tr("tui.tool.result")
		if te.resultBytes > 0 {
			meta += fmt.Sprintf(" · %d %s", te.resultBytes, t.tr("tui.tool.bytes"))
		}
		if te.resultTruncated {
			meta += " · " + t.tr("tui.tool.truncated")
		}
		lines = append(lines, "", styleDim.Render(meta))
		lines = append(lines, splitWrapped(te.result, inner, 0)...)
	}
	return lines, bodyHeight
}

func (t *TUI) toolDetailHelpText(start, height, total int) string {
	idx, toolTotal := t.selectedToolPosition()
	var parts []string
	if total > height {
		parts = append(parts, fmt.Sprintf("PgUp/PgDn %s %d-%d/%d", t.tr("tui.overlay.scroll"), start+1, min(total, start+height), total))
	}
	if toolTotal > 1 {
		if idx > 0 {
			parts = append(parts, "↑ "+t.tr("tui.tool.prev"))
		}
		if idx < toolTotal-1 {
			parts = append(parts, "↓ "+t.tr("tui.tool.next"))
		}
	}
	parts = append(parts, "Ctrl+T/Esc "+t.tr("tui.tool.close"))
	return strings.Join(parts, " · ")
}

func (t *TUI) appendSubtaskParams(lines *[]string, te *toolEntry, width int) {
	if te == nil || len(te.paramsRaw) == 0 {
		return
	}
	if model, ok := te.paramsRaw["model"]; ok {
		*lines = append(*lines, "", styleDim.Render(t.tr("tui.tool.model")))
		*lines = append(*lines, styleToolDim.Render(fmt.Sprintf("%v", model)))
	}
	if tools, ok := te.paramsRaw["tools"]; ok {
		*lines = append(*lines, "", styleDim.Render(t.tr("tui.tool.tools")))
		*lines = append(*lines, splitWrapped(fmt.Sprintf("%v", tools), width, 0)...)
	}
	if task, ok := te.paramsRaw["task"]; ok {
		*lines = append(*lines, "", styleDim.Render(t.tr("tui.tool.task")))
		*lines = append(*lines, splitWrapped(fmt.Sprintf("%v", task), width, 0)...)
	}
}

func splitWrapped(content string, width int, maxLines int) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimRight(content, "\n"), "\n") {
		remaining := 0
		if maxLines > 0 {
			remaining = maxLines - len(out)
			if remaining <= 0 {
				return append(out, styleDim.Render("..."))
			}
		}
		for _, wrapped := range wrapLineLimit(line, width, remaining) {
			out = append(out, styleToolDim.Render(wrapped))
			if maxLines > 0 && len(out) >= maxLines {
				return append(out, styleDim.Render("..."))
			}
		}
	}
	return out
}

func (t *TUI) findTool(id string) *toolEntry {
	if id == "" {
		return nil
	}
	if t.currentToolBlock != nil {
		if te := t.currentToolBlock.entries[id]; te != nil {
			return te
		}
	}
	for _, msg := range t.messages {
		if block, ok := msg.content.(*toolBlock); ok && block != nil {
			if te := block.entries[id]; te != nil {
				return te
			}
		}
	}
	return nil
}

func (t *TUI) visibleToolIDs() []string {
	block := t.currentToolBlock
	if block == nil {
		for i := len(t.messages) - 1; i >= 0; i-- {
			if b, ok := t.messages[i].content.(*toolBlock); ok {
				block = b
				break
			}
		}
	}
	if block == nil {
		return nil
	}
	return append([]string(nil), block.order...)
}

func (t *TUI) selectedToolPosition() (int, int) {
	ids := t.visibleToolIDs()
	for i, id := range ids {
		if id == t.selectedToolID {
			return i, len(ids)
		}
	}
	return 0, len(ids)
}

func (t *TUI) runningToolCount() int {
	count := 0
	for _, te := range t.activeTools {
		if te.status == toolRunning {
			count++
		}
	}
	return count
}

func (t *TUI) markToolRejected(id string) {
	if id == "" {
		return
	}
	te := t.findTool(id)
	if te == nil {
		return
	}
	te.status = toolError
	te.result = t.tr("tui.guard.rejected")
	te.endedAt = time.Now()
	if start, ok := t.toolStartTimes[id]; ok {
		te.duration = time.Since(start)
		delete(t.toolStartTimes, id)
	}
	delete(t.activeTools, id)
	if t.selectedToolID == "" {
		t.selectedToolID = id
	}
}
