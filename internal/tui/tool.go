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
		Width: t.width,
		// 使用占位符代替实时 spinner 字符，渲染结果缓存在 transcript 里；
		// viewChat() 最终输出时统一替换为当前帧，避免 spinner tick 触发全量重建。
		Spinner: spinnerPlaceholder,
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
			Cancelling:           t.tr("tui.tool.cancelling"),
			Cancelled:            t.tr("tui.tool.cancelled"),
			ExecBadge:            t.tr("tui.tool.exec.badge"),
			ExecRunCommand:       t.tr("tui.tool.exec.run_command"),
			ExecStartTask:        t.tr("tui.tool.exec.start_task"),
			ExecCheckTask:        t.tr("tui.tool.exec.check_task"),
			ExecStopTask:         t.tr("tui.tool.exec.stop_task"),
			ExecRunning:          t.tr("tui.tool.exec.running"),
			ExecCompleted:        t.tr("tui.tool.exec.completed"),
			ExecFailed:           t.tr("tui.tool.exec.failed"),
			ExecTimedOut:         t.tr("tui.tool.exec.timed_out"),
			ExecCancelled:        t.tr("tui.tool.exec.cancelled"),
			ExecStopped:          t.tr("tui.tool.exec.stopped"),
			ExecStartFailed:      t.tr("tui.tool.exec.start_failed"),
			ExecNotFound:         t.tr("tui.tool.exec.not_found"),
			ExecAccessDenied:     t.tr("tui.tool.exec.access_denied"),
			ExecAlreadyCompleted: t.tr("tui.tool.exec.already_completed"),
			ExecAlreadyFailed:    t.tr("tui.tool.exec.already_failed"),
			ExecRunLifetime:      t.tr("tui.tool.exec.run_lifetime"),
			ExecSessionLifetime:  t.tr("tui.tool.exec.session_lifetime"),
			ExecElapsed:          t.tr("tui.tool.exec.elapsed"),
			ExecTotal:            t.tr("tui.tool.exec.total"),
			ExecExitCode:         t.tr("tui.tool.exec.exit_code"),
			ExecCleanupPartial:   t.tr("tui.tool.exec.cleanup_partial"),
			ExecStopIncomplete:   t.tr("tui.tool.exec.stop_incomplete"),
			ExecSeeDetails:       t.tr("tui.tool.exec.see_details"),
		},
		Styles:             toolviewStyles(),
		GuardDecisionLabel: t.guardDecisionLabel,
		ReadOnlyLabel:      t.renderReadOnlyBadge,
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
			GuardReadOnly:      t.tr("tui.tool.guard.readonly"),
			GuardSource:        t.tr("tui.tool.guard.source"),
			GuardReason:        t.tr("tui.tool.guard.reason"),
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
		ReadOnlyBadge:      t.renderReadOnlyBadge,
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
	if decision == "confirm" || source == "fallback" || (decision == "approve" && !info.ReadOnly && source == "static") {
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

// renderReadOnlyBadge 展示只读/行动徽章：只读放行绿色，非只读黄色。
// 非只读不代表写入（可能是执行/网络/子进程），统一用“行动”表达有副作用。
func (t *TUI) renderReadOnlyBadge(readOnly bool) string {
	if readOnly {
		return styleGuardOK.Render(t.tr("tui.tool.guard.readonly_badge"))
	}
	return styleGuardWarn.Render(t.tr("tui.tool.guard.write_badge"))
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
