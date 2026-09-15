package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/alanchenchen/suna/internal/tui/components/scroll"
	textutil "github.com/alanchenchen/suna/internal/tui/components/text"
	toolview "github.com/alanchenchen/suna/internal/tui/components/toolview"
)

func (t *TUI) ensureToolBlock() *toolBlock       { return t.chat.EnsureToolBlock() }
func (t *TUI) canAppendToCurrentToolBlock() bool { return t.chat.CanAppendToCurrentToolBlock() }
func (t *TUI) hasRunningTools() bool             { return t.chat.HasRunningTools() }

func (t *TUI) renderToolBlock(block *toolBlock) string {
	deps := t.toolRenderDeps()
	// 展开态是块级状态：提示与详情只依赖"这块是否展开"，不依赖视窗位置，
	// 保证块渲染静态可缓存（滚动时 transcript 窗口签名仍可复用）。
	// 纯 subtask 块没有主条目，RenderBlock 会返回空（面板由 renderSubtaskBlock 负责），
	// 因此不为它构建详情，避免白算一次长结果的行索引。
	hasMain := block != nil && len(toolview.VisibleMainEntries(block)) > 0
	if block != nil && block == t.chat.ExpandedBlock && t.chat.ExpandedBoxKind == "tool" && hasMain {
		deps.Expanded = true
		deps.ExpandedHint = t.tr("tui.tool.detail_expanded")
		deps.EntryCursor = t.chat.ExpandedBlockCursor
		deps.DetailLines, deps.DetailFooter = t.expandedBlockDetailLines()
	} else if hasMain {
		deps.ExpandedHint = t.tr("tui.tool.detail_hint")
	}
	return textutil.IndentLines(toolview.RenderBlock(block, deps), transcriptBlockIndent)
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
			DetailSection:        t.tr("tui.tool.detail_section"),
		},
		Styles:             toolviewStyles(),
		GuardDecisionLabel: t.guardDecisionLabel,
		ReadOnlyLabel:      t.renderReadOnlyBadge,
	}
}

func (t *TUI) toolDetailDeps() toolview.DetailDeps {
	return toolview.DetailDeps{
		Width: t.width,
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
			Context:            t.tr("tui.tool.context"),
			SideEffects:        t.tr("tui.tool.side_effects"),
			Scroll:             t.tr("tui.overlay.scroll"),
			Prev:               t.tr("tui.tool.prev"),
			Next:               t.tr("tui.tool.next"),
			Close:              t.tr("tui.tool.close"),
		},
		Styles:             toolviewStyles(),
		GuardDecisionBadge: t.renderGuardDecisionBadge,
		ReadOnlyBadge:      t.renderReadOnlyBadge,
	}
}

// expandedBlockDetailHeight 是展开块内详情窗口的高度上限。
// 详情就地嵌入 transcript，过高会把块撑成整屏，因此按终端高度取比例并设上下限。
func (t *TUI) expandedBlockDetailHeight() int {
	return min(16, max(6, t.height/3))
}

// expandedBlockDetailLines 渲染展开块当前选中条目的详情窗口。
// 复用 DetailLineSource（虚拟数据源），只取可见窗口，长结果不会 materialize 全文。
func (t *TUI) expandedBlockDetailLines() (lines []string, footer string) {
	te := t.chat.SelectedExpandedEntry()
	if te == nil {
		return nil, ""
	}
	deps := t.toolDetailDeps()
	height := t.expandedBlockDetailHeight()
	source := toolview.DetailLineSource(te, deps)
	body, start, total := scroll.Window(source, height, &t.chat.ExpandedBlockDetailScroll)
	lines = append([]string(nil), body...)
	if total > height {
		footer = fmt.Sprintf("PgUp/PgDn %s %d-%d/%d", t.tr("tui.overlay.scroll"), start+1, min(total, start+height), total)
	}
	return lines, footer
}

// scrollExpandedBlockDetail 滚动展开块详情窗口。
// 返回是否真正消费了本次滚动：已到边界时返回 false，让调用方把滚动
// 透传给 transcript（滚动链），否则视窗会被内层窗口"卡住"。
func (t *TUI) scrollExpandedBlockDetail(delta int) bool {
	te := t.chat.SelectedExpandedEntry()
	if te == nil {
		t.chat.ExpandedBlockDetailScroll = 0
		return false
	}
	deps := t.toolDetailDeps()
	maxOffset := max(0, toolview.DetailLineSource(te, deps).Len()-t.expandedBlockDetailHeight())
	next := clampInt(t.chat.ExpandedBlockDetailScroll+delta, 0, maxOffset)
	if next == t.chat.ExpandedBlockDetailScroll {
		return false
	}
	t.chat.ExpandedBlockDetailScroll = next
	return true
}

// toggleBlockDetail 切换视窗内最相关工具块的展开态（Ctrl+T），
// 与 Ctrl+R 一样在高度变化后恢复滚动锚点，避免展开导致视口跳动。
func (t *TUI) toggleBlockDetail() {
	if t.chat.Compacting {
		// 压缩中 transcript 正在重建，展开锚点会失效。
		return
	}
	anchor, changed := t.chat.ToggleVisibleBlockDetail()
	if !changed {
		return
	}
	t.syncContent()
	t.chat.RestoreTranscriptAnchor(anchor)
	t.updateTranscriptFollowAfterNavigation()
	t.layoutChat()
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

func (t *TUI) moveExpandedBlockCursor(delta int) { t.chat.MoveExpandedBlockCursor(delta) }

// expandedBlockHasMainEntries 判断展开的是否是含主条目的 tool 盒子。
// 纯 subtask 块的 tool 盒子不渲染详情窗口，导航由 subtask 面板自身承担。
func (t *TUI) expandedBlockHasMainEntries() bool {
	block := t.chat.ExpandedBlock
	return block != nil && t.chat.ExpandedBoxKind == "tool" && len(toolview.VisibleMainEntries(block)) > 0
}

// expandedBlockVisible 报告展开块当前是否在 transcript 视窗内。
// 就地展开的详情窗口只在块可见时接管滚动：块滚出视窗后，
// 用户滚动意图是查看其它内容，继续消耗会让视窗"卡住"。
func (t *TUI) expandedBlockVisible() bool {
	block := t.chat.ExpandedBlock
	if block == nil {
		return false
	}
	for i, msg := range t.chat.Messages {
		if msg.Content == block {
			return t.chat.BlockVisible(i)
		}
	}
	return false
}

// wheelDelta 把滚轮方向转成滚动增量（与 viewport 的步长一致）。
func wheelDelta(up, down bool) int {
	switch {
	case up:
		return -3
	case down:
		return 3
	}
	return 0
}

func isSubtask(te *toolEntry) bool {
	return toolview.IsSubtask(te)
}
func isSubtaskChild(te *toolEntry) bool {
	return toolview.IsSubtaskChild(te)
}
func (t *TUI) findTool(id string) *toolEntry { return t.chat.FindTool(id) }
func (t *TUI) visibleSubtaskIDs() []string   { return t.chat.VisibleSubtaskIDs() }
func (t *TUI) runningToolCount() int         { return t.chat.RunningToolCount() }
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
			out = append(out, styleToolMuted.Render(wrapped))
			if maxLines > 0 && len(out) >= maxLines {
				return append(out, styleDim.Render("..."))
			}
		}
	}
	return out
}

// clampInt 将 v 限制在 [low, high] 区间内；high < low 时交换边界，避免调用方传入反向区间。
func clampInt(v, low, high int) int {
	if high < low {
		low, high = high, low
	}
	return min(high, max(low, v))
}
