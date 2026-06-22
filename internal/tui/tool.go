package tui

import (
	"strings"
	"time"

	textutil "github.com/alanchenchen/suna/internal/tui/components/text"
	toolview "github.com/alanchenchen/suna/internal/tui/components/toolview"
)

func (t *TUI) ensureToolBlock() *toolBlock       { return t.chat.EnsureToolBlock() }
func (t *TUI) canAppendToCurrentToolBlock() bool { return t.chat.CanAppendToCurrentToolBlock() }
func (t *TUI) hasRunningTools() bool             { return t.chat.HasRunningTools() }

func (t *TUI) renderToolBlock(block *toolBlock) string {
	return textutil.IndentLines(toolview.RenderBlock(block, t.toolRenderDeps()), transcriptBlockIndent)
}

func (t *TUI) renderToolEntry(te *toolEntry, nested bool) string {
	return toolview.RenderEntry(te, nested, t.toolRenderDeps())
}

func (t *TUI) toolRenderDeps() toolview.RenderDeps {
	return toolview.RenderDeps{
		Width:   t.width,
		Spinner: t.chat.Spinner.View(),
		Labels: toolview.RenderLabels{
			Tools:                t.tr("tui.tool.tools"),
			Subtask:              t.tr("tui.tool.subtask"),
			GuardBadge:           t.tr("tui.tool.guard.badge"),
			GuardUnknown:         t.tr("tui.tool.guard.unknown"),
			FileBadge:            t.tr("tui.tool.file.badge"),
			Actions:              t.tr("tui.tool.actions"),
			FilesChanged:         t.tr("tui.tool.files_changed"),
			FSChanges:            t.tr("tui.tool.fs_changes"),
			Guarded:              t.tr("tui.tool.guarded"),
			FSBadge:              t.tr("tui.tool.fs.badge"),
			FSDeleted:            t.tr("tui.tool.fs.deleted"),
			FSCreatedDir:         t.tr("tui.tool.fs.created_dir"),
			FSMoved:              t.tr("tui.tool.fs.moved"),
			FSCopied:             t.tr("tui.tool.fs.copied"),
			Recursive:            t.tr("tui.tool.fs.recursive"),
			Overwrote:            t.tr("tui.tool.fs.overwrote"),
			Entries:              t.tr("tui.tool.fs.entries"),
			SearchMatchesInFiles: t.tr("tui.tool.search.matches_in_files"),
			SearchScanned:        t.tr("tui.tool.search.scanned"),
			SearchTruncated:      t.tr("tui.tool.search.truncated"),
		},
		Styles:             toolviewStyles(),
		GuardDecisionLabel: t.guardDecisionLabel,
		RiskLabel:          t.renderRiskBadge,
	}
}

func (t *TUI) toolDetailDeps() toolview.DetailDeps {
	idx, total := t.selectedToolPosition()
	return toolview.DetailDeps{
		Width:            t.width,
		OverlayMaxHeight: t.overlayMaxHeight(),
		SelectedIndex:    idx,
		SelectedTotal:    total,
		ShowPosition:     true,
		Labels: toolview.DetailLabels{
			DetailTitle:        t.tr("tui.tool.detail_title"),
			SubtaskDetailTitle: t.tr("tui.tool.subtask_detail_title"),
			SubtaskToolTitle:   t.tr("tui.tool.subtask_tool_detail_title"),
			Tool:               t.tr("tui.tool.tool"),
			Intent:             t.tr("tui.tool.intent"),
			Params:             t.tr("tui.tool.params"),
			Guard:              t.tr("tui.tool.guard"),
			GuardDecision:      t.tr("tui.tool.guard.decision"),
			GuardRisk:          t.tr("tui.tool.guard.risk"),
			GuardSource:        t.tr("tui.tool.guard.source"),
			GuardReason:        t.tr("tui.tool.guard.reason"),
			GuardSuggestion:    t.tr("tui.tool.guard.suggestion"),
			Result:             t.tr("tui.tool.result"),
			Bytes:              t.tr("tui.tool.bytes"),
			Truncated:          t.tr("tui.tool.truncated"),
			Model:              t.tr("tui.tool.model"),
			Tools:              t.tr("tui.tool.tools"),
			Task:               t.tr("tui.tool.task"),
			Scroll:             t.tr("tui.overlay.scroll"),
			Prev:               t.tr("tui.tool.prev"),
			Next:               t.tr("tui.tool.next"),
			Close:              t.tr("tui.tool.close"),
		},
		Styles:             toolviewStyles(),
		Box:                boxStyle,
		GuardDecisionBadge: t.renderGuardDecisionBadge,
		RiskBadge:          t.renderRiskBadge,
	}
}

func toolviewStyles() toolview.RenderStyles {
	return toolview.RenderStyles{
		Dim:       styleDim,
		HL:        styleHL,
		OK:        styleToolOk,
		Err:       styleToolErr,
		Run:       styleToolRun,
		ToolDim:   styleToolDim,
		Intent:    styleToolIntent,
		MetaPill:  styleMetaPill,
		GuardOK:   styleGuardOK,
		GuardWarn: styleGuardWarn,
		GuardErr:  styleGuardErr,
		FilePath:  styleFilePath,
	}
}

func (t *TUI) visibleToolEntries(block *toolBlock) []*toolEntry {
	return toolview.VisibleEntries(block)
}

func (t *TUI) toolBlockTitle(entries []*toolEntry) string {
	return toolview.BlockTitle(entries, t.toolRenderDeps().Labels)
}

func (t *TUI) moveSelectedTool(delta int) { t.chat.MoveSelectedTool(delta) }

func isSubtask(te *toolEntry) bool {
	return toolview.IsSubtask(te)
}
func isSubtaskChild(te *toolEntry) bool {
	return toolview.IsSubtaskChild(te)
}
func (t *TUI) findTool(id string) *toolEntry    { return t.chat.FindTool(id) }
func (t *TUI) visibleToolIDs() []string         { return t.chat.VisibleToolIDs() }
func (t *TUI) visibleSubtaskIDs() []string      { return t.chat.VisibleSubtaskIDs() }
func (t *TUI) selectedToolPosition() (int, int) { return t.chat.SelectedToolPosition() }
func (t *TUI) runningToolCount() int            { return t.chat.RunningToolCount() }
func (t *TUI) markToolRejected(id string) {
	t.chat.MarkToolRejected(id, t.tr("tui.guard.rejected"), time.Now())
}

func (t *TUI) renderGuardDecisionBadge(info *guardInfo) string {
	label := t.guardDecisionLabel(info)
	if info == nil {
		return styleMetaPill.Render(label)
	}
	source := strings.ToLower(info.Source)
	decision := strings.ToLower(info.Decision)
	if decision == "reject" || strings.Contains(label, "blocked") || strings.Contains(label, "拒绝") || strings.Contains(label, "阻止") {
		return styleGuardErr.Render(label)
	}
	if decision == "confirm" || decision == "modify" || source == "fallback" || (decision == "approve" && strings.ToLower(info.Risk) != "low" && source == "static") {
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
	switch info.Source {
	case "llm":
		switch info.Decision {
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
		if info.Decision == "reject" {
			return t.tr("tui.tool.guard.user_rejected")
		}
		return t.tr("tui.tool.guard.user_approved")
	case "rule":
		if info.Decision == "reject" {
			return t.tr("tui.tool.guard.rule_blocked")
		}
		return t.tr("tui.tool.guard.rule_approved")
	case "static":
		if info.Decision == "reject" {
			return t.tr("tui.tool.guard.policy_blocked")
		}
		return t.tr("tui.tool.guard.auto_approved")
	case "fallback":
		return t.tr("tui.tool.guard.review_unavailable")
	}
	return info.Decision
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

func (t *TUI) renderToolDetailOverlay(width int) string {
	te := t.findTool(t.chat.SelectedToolID)
	if te == nil {
		return ""
	}
	deps := t.toolDetailDeps()
	deps.Width = width
	return toolview.RenderDetailOverlay(te, &t.chat.ToolDetailScroll, deps)
}

func (t *TUI) toolDetailPageStep() int {
	return toolview.DetailPageStep(t.toolDetailDeps())
}

func (t *TUI) scrollToolDetailOverlay(delta int) {
	te := t.findTool(t.chat.SelectedToolID)
	toolview.ScrollDetail(te, &t.chat.ToolDetailScroll, delta, t.toolDetailDeps())
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
		for _, wrapped := range textutil.WrapLineLimit(line, width, remaining) {
			out = append(out, styleToolDim.Render(wrapped))
			if maxLines > 0 && len(out) >= maxLines {
				return append(out, styleDim.Render("..."))
			}
		}
	}
	return out
}
