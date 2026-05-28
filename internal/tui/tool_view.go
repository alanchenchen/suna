package tui

import (
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
	return line + "\n"
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
	inner := max(24, w-8)
	maxBodyLines := max(8, t.vp.Height()-8)
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
		lines = append(lines, splitWrapped(te.intent, inner, 4)...)
	}
	if isSubtask(te) {
		t.appendSubtaskParams(&lines, te, inner)
	} else if te.params != "" {
		lines = append(lines, "", styleDim.Render(t.tr("tui.tool.params")))
		lines = append(lines, splitWrapped(te.params, inner, 18)...)
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
		remaining := max(4, maxBodyLines-len(lines)-2)
		lines = append(lines, splitWrapped(te.result, inner, remaining)...)
	}
	if len(lines) > maxBodyLines {
		lines = append(lines[:maxBodyLines], styleDim.Render("..."))
	}
	lines = append(lines, "", styleDim.Render(t.toolDetailHelpText()))
	return boxStyle.Width(w).Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func (t *TUI) toolDetailHelpText() string {
	idx, total := t.selectedToolPosition()
	var parts []string
	if total > 1 {
		if idx > 0 {
			parts = append(parts, "↑ "+t.tr("tui.tool.prev"))
		}
		if idx < total-1 {
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
		*lines = append(*lines, splitWrapped(fmt.Sprintf("%v", tools), width, 3)...)
	}
	if task, ok := te.paramsRaw["task"]; ok {
		*lines = append(*lines, "", styleDim.Render(t.tr("tui.tool.task")))
		*lines = append(*lines, splitWrapped(fmt.Sprintf("%v", task), width, 10)...)
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
