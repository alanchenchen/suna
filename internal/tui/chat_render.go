package tui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/alanchenchen/suna/internal/logging"
	"github.com/alanchenchen/suna/internal/protocol"
	attachmentmodel "github.com/alanchenchen/suna/internal/tui/components/attachment"
	textutil "github.com/alanchenchen/suna/internal/tui/components/text"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
)

const maxSystemMarkdownBytes = 4000

// transcriptBlockIndent 统一主流程 block（tool/thinking/subtask）的左缩进，避免同级块左右错位。
const transcriptBlockIndent = "  "

const (
	// 思考框本身只展示少量行，流式阶段先裁剪源文本，避免每帧对完整思考链 wrap/markdown。
	reasoningDetailSourceBytes = 32 * 1024
	reasoningDetailSourceLines = 80
	reasoningSummaryTailBytes  = 8 * 1024
	reasoningRunningMaxRows    = 5
	reasoningCompletedMaxRows  = 3
	reasoningDetailMaxRows     = 10
)

func (t *TUI) renderDisplayDiscardSummary(s chatpage.DisplayDiscardSummary) string {
	if s.Empty() {
		return ""
	}
	turns := s.Turns
	if turns <= 0 {
		turns = s.Messages
	}
	text := fmt.Sprintf("已释放 %d 轮早期显示历史 · 约 %s", turns, s.ApproxMB())
	width := max(24, t.width-8)
	bodyWidth := max(20, width-4)
	wrapped := lipgloss.NewStyle().Width(bodyWidth).Render(text)
	lines := strings.Split(wrapped, "\n")
	for i := range lines {
		if i == 0 {
			lines[i] = styleSysLine.Render("  ◇ ") + styleDim.Render(lines[i])
		} else {
			lines[i] = "    " + styleDim.Render(lines[i])
		}
	}
	return "\n" + strings.Join(lines, "\n") + "\n"
}

func (t *TUI) renderSystemMessage(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	width := max(24, t.width-8)
	bodyWidth := max(20, width-4)
	body := content
	if len(content) <= maxSystemMarkdownBytes {
		if rendered := strings.TrimSpace(RenderMarkdown(content, bodyWidth)); rendered != "" {
			body = rendered
		}
	} else {
		body = lipgloss.NewStyle().Width(bodyWidth).Render(content)
	}
	lines := strings.Split(body, "\n")
	for i := range lines {
		if i == 0 {
			lines[i] = styleSysLine.Render("  ◆ ") + lines[i]
		} else {
			lines[i] = "    " + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

func (t *TUI) renderErrorMessage(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	width := max(24, t.width-8)
	bodyWidth := max(20, width-4)
	wrapped := lipgloss.NewStyle().Width(bodyWidth).Render(content)
	lines := strings.Split(wrapped, "\n")
	for i := range lines {
		if i == 0 {
			lines[i] = styleErrLine.Render("  ✗ " + lines[i])
		} else {
			lines[i] = styleErrLine.Render("    " + lines[i])
		}
	}
	return strings.Join(lines, "\n")
}
func (t *TUI) renderThinkingBox(content string, running bool, startedAt, endedAt time.Time) string {
	width := max(24, min(t.width-8, 62))
	inner := width - 4
	elapsed := reasoningElapsed(running, startedAt, endedAt)
	title := t.tr("tui.chat.thinking")
	if running {
		title = fmt.Sprintf("%s %s %.1fs", t.chat.Spinner.View(), t.tr("tui.chat.thinking"), elapsed.Seconds())
	} else if elapsed > 0 {
		title = fmt.Sprintf("✓ %s %.1fs", t.tr("tui.chat.thinking"), elapsed.Seconds())
	}
	if !running && !t.chat.ShowReasoningDetail {
		title += " · " + t.tr("tui.chat.thinking_detail_hint")
	}
	display := strings.TrimSpace(content)
	if running && display == "" {
		display = t.tr("status.thinking")
	}
	if !t.chat.ShowReasoningDetail {
		trimmed := strings.TrimSpace(clipTailBytes(display, reasoningSummaryTailBytes))
		if running {
			display = renderStreamingText(trimmed, inner)
		} else {
			display = renderStreamingText(clipHeadLinesBytes(trimmed, reasoningDetailSourceLines, reasoningSummaryTailBytes), inner)
		}
		if strings.TrimSpace(display) == "" {
			display = t.tr("tui.chat.thought_done")
		}
	} else {
		trimmed := strings.TrimSpace(content)
		if running {
			trimmed = clipTailLinesBytes(trimmed, reasoningDetailSourceLines, reasoningDetailSourceBytes)
			display = renderStreamingText(trimmed, inner)
		} else {
			trimmed = clipHeadLinesBytes(trimmed, reasoningDetailSourceLines, reasoningDetailSourceBytes)
			display = RenderMarkdown(trimmed, inner)
		}
	}
	lines := strings.Split(strings.TrimRight(display, "\n"), "\n")
	body := make([]string, 0, len(lines))
	for _, line := range lines {
		body = append(body, textutil.WrapLine(line, inner)...)
	}
	body = limitThinkingBodyRows(body, t.chat.ShowReasoningDetail, running)
	return textutil.IndentLines(renderThinkingRoundBox(width, title, body), transcriptBlockIndent) + "\n"
}
func limitThinkingBodyRows(lines []string, detail bool, running bool) []string {
	maxRows := reasoningCompletedMaxRows
	if detail {
		maxRows = reasoningDetailMaxRows
	} else if running {
		maxRows = reasoningRunningMaxRows
	}
	lines = trimEmptyThinkingRows(lines)
	if len(lines) <= maxRows {
		return lines
	}
	if running {
		return append([]string{"..."}, lines[len(lines)-maxRows+1:]...)
	}
	return append(append([]string(nil), lines[:maxRows-1]...), "...")
}

func trimEmptyThinkingRows(lines []string) []string {
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	end := len(lines)
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return append([]string(nil), lines[start:end]...)
}

func reasoningElapsed(running bool, startedAt, endedAt time.Time) time.Duration {
	if startedAt.IsZero() {
		return 0
	}
	if running {
		return time.Since(startedAt).Truncate(100 * time.Millisecond)
	}
	if endedAt.IsZero() || endedAt.Before(startedAt) {
		return 0
	}
	return endedAt.Sub(startedAt).Truncate(100 * time.Millisecond)
}
func (t *TUI) renderSkillLoadMessage(p protocol.SkillLoadParams) string {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = "unknown"
	}
	labelKey := "tui.skill.loaded"
	icon := "✓"
	accent := ColorAgent
	if strings.TrimSpace(p.Status) == "loading" {
		labelKey = "tui.skill.loading"
		icon = "◐"
		accent = ColorBrand
	}
	badge := lipgloss.NewStyle().
		Foreground(currentTheme.ToolText).
		Background(accent).
		Bold(true).
		Padding(0, 1).
		Render(icon + " " + t.tr(labelKey))
	nameBadge := lipgloss.NewStyle().
		Foreground(currentTheme.Text).
		Background(currentTheme.CodeBg).
		Bold(true).
		Padding(0, 1).
		Render(name)
	content := badge + " " + nameBadge
	box := boxStyle.
		BorderForeground(accent).
		Padding(0, 1).
		Render(content)
	return textutil.IndentLines(box, "    ")
}
func (t *TUI) renderSkillReviewMessage(p protocol.SkillReviewParams) string {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = "unknown"
	}
	status := strings.TrimSpace(p.Status)
	title := styleMetaPill.Render(t.tr("tui.skill.review")) + " " + styleHL.Render(name)
	body := strings.TrimSpace(p.Review)
	if body == "" && p.Error != "" {
		body = "Error: " + p.Error
	}
	if body == "" {
		body = status
	}
	width := max(36, min(76, t.width-6))
	inner := max(20, width-8)
	content := title
	if body != "" {
		rendered := strings.TrimSpace(RenderMarkdown(body, inner))
		if rendered == "" {
			rendered = body
		}
		content += "\n" + rendered
	}
	return textutil.IndentLines(boxStyle.BorderForeground(ColorBrand).Width(width).Padding(1, 2).Render(content), "  ")
}
func (t *TUI) hasVisibleActiveProgress() bool {
	if t.chat.CurrentToolBlock != nil && len(t.chat.CurrentToolBlock.Order) > 0 {
		return true
	}
	for i := len(t.chat.Messages) - 1; i >= 0; i-- {
		msg := t.chat.Messages[i]
		switch msg.Role {
		case "reasoning":
			return msg.Streaming
		case "assistant", "user", "error", "system", "restore_summary", "panel":
			return false
		}
	}
	return false
}
func (t *TUI) compactRunningLabel() string {
	if t.compactAuto {
		return t.tr("compact.auto_running")
	}
	return t.tr("compact.running")
}
func (t *TUI) renderCurrentStatusLine() string {
	label := t.currentStatusLabel()
	if label == "" {
		label = t.tr("status.responding")
	}
	elapsed := 0.0
	if !t.chat.PhaseStart.IsZero() {
		elapsed = time.Since(t.chat.PhaseStart).Seconds()
	}
	return fmt.Sprintf("    %s %s %s\n", t.chat.Spinner.View(), styleDim.Render(label), styleDim.Render(fmt.Sprintf("%.1fs", elapsed)))
}
func (t *TUI) renderCompactStatusLine() string {
	elapsed := 0.0
	if !t.chat.PhaseStart.IsZero() {
		elapsed = time.Since(t.chat.PhaseStart).Seconds()
	}
	return fmt.Sprintf("    %s %s\n", t.chat.Spinner.View(), styleDim.Render(fmt.Sprintf("%.1fs", elapsed)))
}
func (t *TUI) currentInputStatusLabel() string {
	if t.hasVisibleActiveProgress() && !t.chat.Compacting {
		return t.tr("status.running")
	}
	return t.currentStatusLabel()
}
func (t *TUI) currentStatusLabel() string {
	if t.chat.Compacting {
		return t.compactRunningLabel()
	}
	if t.chat.StatusLabel != "" {
		return t.chat.StatusLabel
	}
	if n := t.runningToolCount(); n > 0 {
		return fmt.Sprintf("%s · %d running", t.tr("status.exec_tool"), n)
	}
	if t.chat.ActiveInteractionKind() == chatpage.InteractionAskUser {
		return t.tr("tui.ask.waiting")
	}
	switch t.chat.Phase {
	case phaseFirstLLM:
		return t.tr("status.waiting_llm")
	case phaseLLM:
		return t.tr("status.responding")
	case phaseThinking:
		return t.tr("status.thinking")
	case phaseTool:
		return t.tr("status.exec_tool")
	case phaseWaitingAfterTool:
		if t.chat.LastWaitingTool == "spawn" {
			return t.tr("status.waiting_after_subtask")
		}
		return t.tr("status.waiting_after_tool")
	default:
		return ""
	}
}

func clipTailBytes(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	start := len(s) - maxBytes
	for start < len(s) && (s[start]&0xc0) == 0x80 {
		start++
	}
	return s[start:]
}

func clipHeadBytes(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	end := maxBytes
	for end > 0 && (s[end]&0xc0) == 0x80 {
		end--
	}
	return s[:end]
}

func clipTailLinesBytes(s string, maxLines, maxBytes int) string {
	s = clipTailBytes(s, maxBytes)
	if maxLines <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= maxLines {
		return s
	}
	return strings.Join(lines[len(lines)-maxLines:], "\n")
}

func clipHeadLinesBytes(s string, maxLines, maxBytes int) string {
	s = clipHeadBytes(s, maxBytes)
	if maxLines <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= maxLines {
		return s
	}
	return strings.Join(lines[:maxLines], "\n")
}

func (t *TUI) renderSessionRestoreToolSummary(summary protocol.ToolSummaryPayload) string {
	if summary.Total <= 0 && len(summary.Recent) == 0 && len(summary.Failures) == 0 && len(summary.Changes) == 0 {
		return ""
	}
	var lines []string
	lines = append(lines, t.tr("session.restore_tools_title"))
	if summary.Failed <= 0 {
		lines = append(lines, t.i18n.Tf("session.restore_tools_all_success", summary.Total))
	} else {
		lines = append(lines, t.i18n.Tf("session.restore_tools_stats", summary.Total, summary.Success, summary.Failed))
	}
	if len(summary.Failures) > 0 {
		parts := make([]string, 0, len(summary.Failures))
		for _, item := range summary.Failures {
			part := strings.TrimSpace(item.Tool)
			if item.Summary != "" {
				part += " · " + truncateRunesForUI(item.Summary, 72)
			}
			if part != "" {
				parts = append(parts, part)
			}
		}
		if len(parts) > 0 {
			lines = append(lines, t.i18n.Tf("session.restore_tools_failures", strings.Join(parts, "; ")))
		}
	}
	if len(summary.Changes) > 0 {
		parts := make([]string, 0, len(summary.Changes))
		for _, item := range summary.Changes {
			if item.Tool != "" && item.Count > 0 {
				parts = append(parts, fmt.Sprintf("%s ×%d", item.Tool, item.Count))
			}
		}
		if len(parts) > 0 {
			lines = append(lines, t.i18n.Tf("session.restore_tools_changes", strings.Join(parts, ", ")))
		}
	}
	if len(summary.Recent) > 0 {
		names := make([]string, 0, len(summary.Recent))
		for _, item := range summary.Recent {
			if item.Tool != "" {
				names = append(names, item.Tool)
			}
		}
		if len(names) > 0 {
			lines = append(lines, t.i18n.Tf("session.restore_tools_recent", strings.Join(names, " → ")))
		}
	}
	if summary.Omitted > 0 {
		lines = append(lines, t.i18n.Tf("session.restore_tools_omitted", summary.Omitted))
	}
	return strings.Join(lines, "\n")
}

func truncateRunesForUI(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

func (t *TUI) renderRestoreSummaryBox(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	width := max(36, min(76, t.width-6))
	inner := max(20, width-8)
	lines := strings.Split(content, "\n")
	if len(lines) > 0 {
		first := strings.TrimSpace(lines[0])
		if first == t.tr("session.restore_tools_title") || strings.HasPrefix(first, "上一轮工具操作摘要") {
			lines = lines[1:]
		}
	}
	if len(lines) > 5 {
		lines = append(lines[:4], "...")
	}
	var body []string
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimPrefix(line, "-"))
		if line == "" {
			continue
		}
		for _, wrapped := range textutil.WrapLine(line, inner) {
			body = append(body, styleDim.Render(wrapped))
		}
	}
	if len(body) == 0 {
		body = []string{styleDim.Render(content)}
	}
	title := styleHL.Render(t.tr("session.restore_tools_title"))
	return textutil.IndentLines(boxStyle.Width(width).Padding(1, 2).Render(title+"\n"+strings.Join(body, "\n")), "  ")
}

func renderInlineUserMessage(content string, width int) string {
	lines := strings.Split(textutil.IndentWrappedPlain(content, "", width), "\n")
	if len(lines) == 0 {
		return "  " + styleUserLine.Render("●")
	}
	lines[0] = "  " + styleUserLine.Render("● ") + lines[0]
	for i := 1; i < len(lines); i++ {
		lines[i] = "    " + lines[i]
	}
	return strings.Join(lines, "\n")
}

func parseOptionIndex(input string, maxOptions int) (int, bool) {
	input = strings.TrimSpace(input)
	if n, err := fmt.Sscanf(input, "%d", new(int)); n == 1 && err == nil {
		var idx int
		fmt.Sscanf(input, "%d", &idx)
		if idx >= 1 && idx <= maxOptions {
			return idx - 1, true
		}
	}
	return -1, false
}

func (t *TUI) renderAssistantMessage(msg *chatMsg) string {
	content, _ := msg.Content.(string)
	width := max(20, t.width-6)
	if msg.Streaming {
		if msg.Stream != nil {
			return textutil.IndentLines(t.cachedStreamingState(msg, width), "  ")
		}
		// 流式阶段避免每个 delta 都跑 Glamour，也避免对完整已生成内容反复 wrap。
		return textutil.IndentLines(t.cachedStreamingText(msg, content, width), "  ")
	}
	return textutil.IndentLines(t.cachedMarkdown(msg, content, width), "  ")
}

func (t *TUI) renderReasoningMessage(msg *chatMsg) string {
	content, _ := msg.Content.(string)
	if msg.Stream != nil {
		content = msg.Stream.Text()
	}
	out := t.renderThinkingBox(content, msg.Streaming, msg.StartedAt, msg.EndedAt)
	msg.Render = msgRenderCache{Width: t.width, Theme: currentTheme.Name, ContentLen: len(content), LineCount: chatpage.RenderedLineCount(out), Output: out, Mode: reasoningRenderMode(t.chat.ShowReasoningDetail)}
	return out
}

func reasoningRenderMode(detail bool) string {
	if detail {
		return "reasoning_detail"
	}
	return "reasoning_collapsed"
}

func (t *TUI) cachedMarkdown(msg *chatMsg, content string, width int) string {
	cache := msg.Render
	hash := renderContentHash(content)
	if cache.Output != "" && cache.Width == width && cache.Theme == currentTheme.Name && cache.ContentLen == len(content) && cache.ContentHash == hash {
		return cache.Output
	}
	out := RenderMarkdown(content, width)
	msg.Render = msgRenderCache{Width: width, Theme: currentTheme.Name, ContentLen: len(content), ContentHash: hash, LineCount: chatpage.RenderedLineCount(out), Output: out}
	return out
}

func (t *TUI) cachedStreamingState(msg *chatMsg, width int) string {
	started := time.Now()
	state := msg.Stream
	if state == nil {
		return ""
	}
	if width <= 0 {
		width = 20
	}
	tailLines := t.streamingRenderTailLines()
	pendingDeltas := len(state.Pending)
	defer func() {
		if elapsed := time.Since(started); elapsed >= chatpage.SlowPerfLogThreshold() {
			logging.Info("perf", "slow_tui_stream_render", logging.Event{"component": "tui", "duration_ms": elapsed.Milliseconds(), "pending_deltas": pendingDeltas, "lines": len(state.Lines), "raw_bytes": state.Raw.Len(), "width": width})
		}
	}()
	if state.Width != width {
		content := state.Text()
		out := renderStreamingText(content, width)
		lines := splitRenderedLines(out)
		state.Width = width
		state.Lines = lines
		state.DroppedLines = 0
		limitStreamingStateLines(state, tailLines)
		state.LastLineWidth = lastRenderedLineWidth(state.Lines)
		state.PendingNewlines = trailingNewlineCount(content)
		state.RenderedBytes = len(content)
		state.Pending = nil
		out = strings.Join(state.Lines, "\n")
		msg.Render = msgRenderCache{Width: width, ContentLen: len(content), LineCount: len(state.Lines)}
		return out
	}
	if len(state.Pending) > 0 {
		for _, delta := range state.Pending {
			appendStreamingDelta(&state.Lines, &state.LastLineWidth, &state.PendingNewlines, delta, width)
			state.RenderedBytes += len(delta)
			limitStreamingStateLines(state, tailLines)
		}
		state.Pending = nil
	}
	limitStreamingStateLines(state, tailLines)
	out := strings.Join(state.Lines, "\n")
	msg.Render = msgRenderCache{Width: width, ContentLen: state.Raw.Len(), LineCount: len(state.Lines)}
	return out
}

func (t *TUI) streamingRenderTailLines() int {
	height := t.chat.Viewport.Height()
	if height <= 0 {
		return 200
	}
	return max(120, min(600, height*4))
}

func limitStreamingStateLines(state *chatpage.StreamingTextState, maxLines int) {
	if state == nil || maxLines <= 0 || len(state.Lines) <= maxLines {
		return
	}
	drop := len(state.Lines) - maxLines
	for i := 0; i < drop; i++ {
		state.Lines[i] = ""
	}
	state.Lines = append([]string(nil), state.Lines[drop:]...)
	state.DroppedLines += drop
}

func (t *TUI) cachedStreamingText(msg *chatMsg, content string, width int) string {
	cache := msg.Render
	if content == "" {
		msg.Render = msgRenderCache{Width: width}
		return ""
	}
	content = textutil.ExpandTabs(content, 4)
	if cache.Width != width || cache.ContentLen > len(content) {
		out := renderStreamingText(content, width)
		lines := splitRenderedLines(out)
		msg.Render = msgRenderCache{Width: width, ContentLen: len(content), LineCount: len(lines), Output: out, StreamLines: lines, StreamLastLineWidth: lastRenderedLineWidth(lines), StreamPendingNewlines: trailingNewlineCount(content)}
		return out
	}
	if cache.ContentLen == len(content) && cache.Output != "" {
		return cache.Output
	}

	lines := append([]string(nil), cache.StreamLines...)
	lastWidth := cache.StreamLastLineWidth
	pendingNewlines := cache.StreamPendingNewlines
	appendStreamingDelta(&lines, &lastWidth, &pendingNewlines, content[cache.ContentLen:], width)
	out := strings.Join(lines, "\n")
	msg.Render = msgRenderCache{Width: width, ContentLen: len(content), LineCount: len(lines), Output: out, StreamLines: lines, StreamLastLineWidth: lastWidth, StreamPendingNewlines: pendingNewlines}
	return out
}

func appendStreamingDelta(lines *[]string, lastWidth *int, pendingNewlines *int, delta string, width int) {
	state := byte(0)
	segmentStart := 0
	ensureLine := func() {
		for *pendingNewlines > 0 {
			*lines = append(*lines, "")
			*lastWidth = 0
			(*pendingNewlines)--
		}
		if len(*lines) == 0 {
			*lines = append(*lines, "")
			*lastWidth = 0
		}
	}
	flushSegment := func(end int) {
		if end <= segmentStart {
			return
		}
		ensureLine()
		last := len(*lines) - 1
		// 流式渲染可能遇到超长 JSON/代码行。按连续片段追加，避免逐 grapheme 字符串拼接退化。
		(*lines)[last] += delta[segmentStart:end]
		segmentStart = end
	}
	for i := 0; i < len(delta); {
		if delta[i] == '\n' {
			flushSegment(i)
			(*pendingNewlines)++
			i++
			segmentStart = i
			state = 0
			continue
		}
		ensureLine()
		_, cellWidth, n, newState := ansi.GraphemeWidth.DecodeSequenceInString(delta[i:], state, nil)
		if n <= 0 {
			n = 1
			cellWidth = 1
		}
		if width > 0 && cellWidth > 0 && *lastWidth > 0 && *lastWidth+cellWidth > width {
			flushSegment(i)
			*lines = append(*lines, "")
			*lastWidth = 0
		}
		*lastWidth += cellWidth
		state = newState
		i += n
	}
	flushSegment(len(delta))
}

func splitRenderedLines(out string) []string {
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

func lastRenderedLineWidth(lines []string) int {
	if len(lines) == 0 {
		return 0
	}
	return lipgloss.Width(lines[len(lines)-1])
}

func trailingNewlineCount(s string) int {
	count := 0
	for i := len(s) - 1; i >= 0 && s[i] == '\n'; i-- {
		count++
	}
	return count
}

func renderContentHash(content string) uint64 {
	sum := sha256.Sum256([]byte(content))
	var out uint64
	for _, b := range sum[:8] {
		out = out<<8 | uint64(b)
	}
	return out
}

func renderStreamingText(content string, width int) string {
	if content == "" {
		return ""
	}
	content = textutil.ExpandTabs(content, 4)
	var lines []string
	for _, line := range strings.Split(strings.TrimRight(content, "\n"), "\n") {
		lines = append(lines, textutil.WrapLine(line, width)...)
	}
	return strings.Join(lines, "\n")
}

type attachmentItem = attachmentmodel.Item
type pendingImagePaste = attachmentmodel.PendingImagePaste

func (t *TUI) handlePaste(content string) tea.Cmd {
	pending, ok, blocked := attachmentmodel.DetectImagePaste(content)
	if blocked {
		t.appendNonToolMessage(chatMsg{Role: "error", Content: t.tr("tui.attachment.base64_blocked")})
		return nil
	}
	if !ok {
		t.chat.Textarea.InsertString(content)
		t.layoutChat()
		return nil
	}
	t.chat.EnqueueImagePaste(pending)
	t.layoutChat()
	return nil
}

func (t *TUI) updatePendingImagePaste(key string) tea.Cmd {
	if t.chat.ActiveImagePaste() == nil {
		return nil
	}
	switch key {
	case "enter":
		return t.confirmPendingImagePaste()
	case "esc":
		p := t.chat.CancelPendingImagePaste()
		if t.chat.PendingPasteShouldRestoreRaw(p) {
			t.chat.Textarea.InsertString(p.Raw)
		}
		t.layoutChat()
	}
	return nil
}

func (t *TUI) confirmPendingImagePaste() tea.Cmd {
	p := t.chat.ActiveImagePaste()
	if p == nil {
		return nil
	}
	t.chat.CancelPendingImagePaste()
	t.layoutChat()
	if len(p.Data) > 0 {
		path, name, size, err := t.savePastedImage(p)
		if err != nil {
			t.appendNonToolMessage(chatMsg{Role: "error", Content: err.Error()})
			return nil
		}
		p.SourceKind = protocol.AttachmentKindAttachment
		p.Path = path
		p.Name = name
		p.Size = size
	}
	t.chat.AddConfirmedImageAttachment(p)
	t.layoutChat()
	if p.SourceKind == protocol.AttachmentKindAttachment && p.Path != "" {
		// TUI 本地保存粘贴图片后，daemon 侧的附件统计不会自动变化；主动刷新状态，避免配置页显示旧数量。
		return t.attachmentStatusCmd()
	}
	return nil
}

func (t *TUI) updateAttachmentMode(key string) bool {
	return t.chat.UpdateAttachmentMode(key)
}

func (t *TUI) deleteSelectedAttachment() {
	t.chat.DeleteSelectedAttachment()
}

func (t *TUI) renderUserMessage(content any, width int) string {
	switch v := content.(type) {
	case userMessageContent:
		text := renderInlineUserMessage(v.Text, width)
		if len(v.Attachments) == 0 {
			return text
		}
		return text + "\n" + textutil.IndentLines(t.renderAttachmentList(v.Attachments, -1, false), "  ")
	case string:
		return renderInlineUserMessage(v, width)
	default:
		return renderInlineUserMessage(fmt.Sprint(v), width)
	}
}

func (t *TUI) renderAttachmentPanel() string {
	view := t.chat.AttachmentPanelView(t.attachmentHelp())
	if !view.Visible {
		return ""
	}
	if view.Pending != nil {
		return ""
	}
	return t.renderAttachmentBox(view.Items, view.Cursor, view.Mode, view.Help)
}

func (t *TUI) renderAttachmentBox(items []attachmentItem, cursor int, selectable bool, help string) string {
	width := max(36, t.width-4)
	inner := max(24, width-8)
	title := fmt.Sprintf("%s · %d %s", t.tr("tui.attachment.pending_title"), len(items), attachmentTypeSummary(items))
	var lines []string
	limit := min(len(items), 4)
	for i := 0; i < limit; i++ {
		item := items[i]
		prefix := "  "
		st := lipgloss.NewStyle()
		if selectable && i == cursor {
			prefix = styleCursor.Render("▶ ")
			st = styleHL
		}
		nameWidth := max(10, inner-22)
		line := fmt.Sprintf("%s%d  %-5s  %-*s  %s", prefix, i+1, item.Type, nameWidth, truncateMiddle(item.Name, nameWidth), formatAttachmentSize(item.Size))
		lines = append(lines, st.Render(line))
	}
	if len(items) > limit {
		lines = append(lines, styleDim.Render(fmt.Sprintf("  +%d more", len(items)-limit)))
	}
	if strings.TrimSpace(help) != "" {
		if len(lines) > 0 {
			lines = append(lines, styleDim.Render(strings.Repeat("─", inner)))
		}
		lines = append(lines, styleDim.Render(help))
	}
	return boxStyle.Width(width).Padding(0, 1).Render(styleHL.Render(title) + "\n" + strings.Join(lines, "\n"))
}

func attachmentTypeSummary(items []attachmentItem) string {
	if len(items) == 1 {
		if strings.TrimSpace(items[0].Type) != "" {
			return items[0].Type
		}
		return "item"
	}
	images := 0
	for _, item := range items {
		if item.Type == "image" {
			images++
		}
	}
	if images == len(items) {
		return "images"
	}
	return "items"
}

func (t *TUI) renderAttachmentList(items []attachmentItem, cursor int, selectable bool) string {
	var lines []string
	lines = append(lines, styleHL.Render(t.tr("tui.attachment.title")))
	limit := min(len(items), 4)
	for i := 0; i < limit; i++ {
		item := items[i]
		prefix := "  "
		st := lipgloss.NewStyle()
		if selectable && i == cursor {
			prefix = styleCursor.Render("▶ ")
			st = styleHL
		}
		line := fmt.Sprintf("%s%d  %-5s  %-24s  %s", prefix, i+1, item.Type, truncateMiddle(item.Name, 24), formatAttachmentSize(item.Size))
		lines = append(lines, st.Render(line))
	}
	if len(items) > limit {
		lines = append(lines, styleDim.Render(fmt.Sprintf("  +%d more", len(items)-limit)))
	}
	return strings.Join(lines, "\n")
}

func (t *TUI) renderPendingImagePasteOverlay(width int) string {
	content := strings.TrimSpace(t.renderPendingImagePaste())
	if content == "" {
		return ""
	}
	boxWidth := max(36, min(width-8, 72))
	return boxStyle.Width(boxWidth).Padding(1, 2).BorderForeground(ColorDim).Render(content)
}

func (t *TUI) renderPendingImagePaste() string {
	p := t.chat.ActiveImagePaste()
	if p == nil {
		return ""
	}
	title := t.tr("tui.attachment.detected") + " " + p.Name
	help := t.tr("tui.attachment.confirm_help")
	if p.SourceKind == "data_uri" {
		title = t.tr("tui.attachment.detected_data")
		help = t.tr("tui.attachment.confirm_data_help")
	}
	return styleHL.Render(title) + "\n" + styleDim.Render(help)
}

func (t *TUI) attachmentHelp() string {
	if t.chat.AttachmentDelete && len(t.chat.Attachments) > 0 {
		name := t.chat.Attachments[t.chat.AttachmentCursor].Name
		return styleError.Render(t.tr("tui.attachment.delete")+" "+name+"?") + " " + styleDim.Render(t.tr("tui.attachment.delete_help"))
	}
	if t.chat.AttachmentMode {
		return styleDim.Render(t.tr("tui.attachment.mode_help"))
	}
	return styleDim.Render(t.tr("tui.attachment.normal_help"))
}

func (t *TUI) savePastedImage(p *pendingImagePaste) (string, string, int64, error) {
	root := strings.TrimSpace(t.attachmentStatus.Root)
	if root == "" {
		return "", "", 0, fmt.Errorf("attachments directory is unavailable")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", "", 0, fmt.Errorf("create attachments dir: %w", err)
	}
	sum := sha256.Sum256(p.Data)
	name := "sha256-" + hex.EncodeToString(sum[:]) + attachmentmodel.ExtFromMime(p.MimeType)
	path := filepath.Join(root, name)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, p.Data, 0600); err != nil {
			return "", "", 0, fmt.Errorf("save pasted image: %w", err)
		}
	}
	return path, name, int64(len(p.Data)), nil
}

func formatAttachmentSize(n int64) string { return attachmentmodel.FormatSize(n) }
func truncateMiddle(s string, maxWidth int) string {
	return attachmentmodel.TruncateMiddle(s, maxWidth)
}
