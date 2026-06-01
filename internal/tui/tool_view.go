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
	entries := t.visibleToolEntries(block)
	var sb strings.Builder
	sb.WriteString("    " + styleDim.Render(t.toolBlockTitle(entries)) + "\n")
	for _, te := range entries {
		sb.WriteString(t.renderToolEntry(te, te.parentID != ""))
	}
	return sb.String()
}

func (t *TUI) visibleToolEntries(block *toolBlock) []*toolEntry {
	var entries []*toolEntry
	for _, id := range block.order {
		te := block.entries[id]
		if te == nil || te.parentID != "" {
			continue
		}
		entries = append(entries, te)
		for _, childID := range block.order {
			child := block.entries[childID]
			if child == nil || child.parentID != te.id {
				continue
			}
			entries = append(entries, child)
		}
	}
	for _, id := range block.order {
		te := block.entries[id]
		if te == nil || te.parentID == "" || block.entries[te.parentID] != nil {
			continue
		}
		entries = append(entries, te)
	}
	return entries
}

func (t *TUI) toolBlockTitle(entries []*toolEntry) string {
	parts := []string{t.tr("tui.tool.tools")}
	if len(entries) > 0 {
		parts = append(parts, fmt.Sprintf("%d actions", len(entries)))
	}
	changedFiles := make(map[string]struct{})
	guards := 0
	for _, te := range entries {
		if path := changedFilePath(te); path != "" {
			changedFiles[path] = struct{}{}
		}
		if shouldShowGuardSummary(te.guard) {
			guards++
		}
	}
	if len(changedFiles) > 0 {
		parts = append(parts, fmt.Sprintf("%d files changed", len(changedFiles)))
	}
	if guards > 0 {
		parts = append(parts, fmt.Sprintf("%d guarded", guards))
	}
	return strings.Join(parts, " · ")
}

func changedFilePath(te *toolEntry) string {
	if !hasFileChange(te) {
		return ""
	}
	path, _ := te.metadata["path"].(string)
	return strings.TrimSpace(path)
}

func hasFileChange(te *toolEntry) bool {
	if te == nil || te.metadata == nil {
		return false
	}
	kind, _ := te.metadata["kind"].(string)
	return kind == "file_change"
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
	maxWidth := max(20, t.width-lipgloss.Width(stripANSI(prefix))-8)
	mainLabel, detailLabel := t.toolEntryLabels(te, maxWidth)
	maxLines := 2
	if isSubtask(te) {
		maxLines = 3
	}
	wrapped := wrapLineLimit(mainLabel, maxWidth, maxLines)
	if len(wrapped) > maxLines {
		wrapped = wrapped[:maxLines]
	}
	line := fmt.Sprintf("%s%s %s%s", prefix, statusIcon, styleHL.Render(wrapped[0]), styleDim.Render(dur))
	for _, extra := range wrapped[1:] {
		line += "\n" + prefix + "  " + styleHL.Render(extra)
	}
	if detailLabel != "" {
		for _, detail := range splitWrapped(detailLabel, maxWidth, 2) {
			line += "\n" + prefix + "  " + detail
		}
	}
	if te.status == toolError {
		err := shortToolError(te.result)
		if err != "" {
			line += "\n" + prefix + "  " + styleToolErr.Render(truncateRunes(err, max(24, t.width-12)))
		}
	}
	if te.status == toolDone {
		if summary := t.renderToolMetadataSummary(te, prefix); summary != "" {
			line += "\n" + summary
		}
	}
	if summary := t.renderGuardSummary(te.guard, prefix); summary != "" {
		line += "\n" + summary
	}
	return line + "\n"
}

func (t *TUI) toolEntryLabels(te *toolEntry, maxWidth int) (string, string) {
	label := t.displayToolIntentLabel(te)
	if hasFileChange(te) {
		if path, _ := te.metadata["path"].(string); path != "" {
			main := te.name + " " + compactPath(path, max(12, maxWidth-lipgloss.Width(te.name)-1))
			if strings.TrimSpace(label) != "" && strings.TrimSpace(label) != main {
				return main, label
			}
			return main, ""
		}
	}
	return label, ""
}

func (t *TUI) renderGuardSummary(info *guardInfo, prefix string) string {
	if !shouldShowGuardSummary(info) {
		return ""
	}
	parts := []string{styleDim.Render(t.tr("tui.tool.guard.badge")), t.renderGuardDecisionBadge(info), t.renderRiskBadge(info.risk)}
	if reason := shortGuardReason(info.reason); reason != "" {
		parts = append(parts, styleToolDim.Render(reason))
	}
	return prefix + "  " + styleDim.Render("↳ ") + strings.Join(parts, "  ")
}

func shouldShowGuardSummary(info *guardInfo) bool {
	if info == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(info.risk), "low") && strings.EqualFold(strings.TrimSpace(info.decision), "approve") {
		return false
	}
	return true
}

func (t *TUI) renderGuardDecisionBadge(info *guardInfo) string {
	label := t.guardDecisionLabel(info)
	if info == nil {
		return styleMetaPill.Render(label)
	}
	source := strings.ToLower(info.source)
	decision := strings.ToLower(info.decision)
	if decision == "reject" || strings.Contains(label, "blocked") || strings.Contains(label, "拒绝") || strings.Contains(label, "阻止") {
		return styleGuardErr.Render(label)
	}
	if decision == "confirm" || decision == "modify" || source == "fallback" || (decision == "approve" && strings.ToLower(info.risk) != "low" && source == "static") {
		return styleGuardWarn.Render(label)
	}
	if decision == "approve" {
		return styleGuardOK.Render(label)
	}
	return styleMetaPill.Render(label)
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
	parts := []string{styleMetaPill.Render(t.tr("tui.tool.file.badge"))}
	maxWidth := max(24, t.width-lipgloss.Width(stripANSI(prefix))-8)
	pathBudget := max(10, maxWidth-34)
	pathText := styleFilePath.Render(compactPath(path, pathBudget))
	parts = append(parts, pathText, renderFileChangeStatus(operation), renderLineDelta("+", added, true), renderLineDelta("-", removed, false))
	if replacements > 0 {
		repl := fmt.Sprintf("%d repl", replacements)
		parts = append(parts, styleGuardWarn.Render(repl))
	}
	if hasAfter {
		if hasBefore && sizeBefore != sizeAfter {
			size := fmt.Sprintf("%s → %s", formatTinyBytes(sizeBefore), formatTinyBytes(sizeAfter))
			parts = append(parts, styleToolDim.Render(size))
		} else if !hasBefore || operation == "created" {
			size := formatTinyBytes(sizeAfter)
			parts = append(parts, styleToolDim.Render(size))
		}
	}

	line := prefix + "  " + arrow + strings.Join(parts, "  ")
	if lipgloss.Width(stripANSI(line)) > t.width-2 {
		allowed := max(10, pathBudget-(lipgloss.Width(stripANSI(line))-(t.width-2)))
		parts[1] = styleFilePath.Render(compactPath(path, allowed))
		line = prefix + "  " + arrow + strings.Join(parts, "  ")
	}
	return line
}

func renderFileChangeStatus(operation string) string {
	label := strings.ToUpper(operation)
	switch operation {
	case "created":
		return styleGuardOK.Render(label)
	case "updated":
		return styleMetaPill.Render(label)
	case "deleted":
		return styleGuardErr.Render(label)
	case "unchanged":
		return styleToolDim.Render(operation)
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
	const ellipsis = "…"
	base := path
	sepIdx := strings.LastIndexAny(path, "/\\")
	if sepIdx >= 0 && sepIdx < len(path)-1 {
		base = path[sepIdx+1:]
	}
	if lipgloss.Width(base)+lipgloss.Width(ellipsis) <= maxWidth {
		return ellipsis + base
	}
	if sepIdx >= 0 && sepIdx < len(path)-1 {
		dir := strings.TrimRight(path[:sepIdx], "/\\")
		parent := dir
		if parentIdx := strings.LastIndexAny(dir, "/\\"); parentIdx >= 0 && parentIdx < len(dir)-1 {
			parent = dir[parentIdx+1:]
		}
		withParent := ellipsis + parent + string(path[sepIdx]) + base
		if lipgloss.Width(withParent) <= maxWidth {
			return withParent
		}
	}
	return truncateRunesKeepEnd(base, maxWidth)
}

func truncateRunesKeepEnd(s string, maxWidth int) string {
	const ellipsis = "…"
	if maxWidth <= lipgloss.Width(ellipsis) {
		return ellipsis
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(ellipsis+string(runes)) > maxWidth {
		runes = runes[1:]
	}
	return ellipsis + string(runes)
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
		if model := subtaskModelLabel(te); model != "" {
			return fmt.Sprintf("%s [%s] · %s", t.tr("tui.tool.subtask"), model, label)
		}
		return t.tr("tui.tool.subtask") + " · " + label
	}
	return label
}

func subtaskModelLabel(te *toolEntry) string {
	if te == nil || te.paramsRaw == nil {
		return ""
	}
	model, ok := te.paramsRaw["model"]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", model))
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
