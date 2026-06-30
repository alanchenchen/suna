package tui

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/alanchenchen/suna/internal/protocol"
	attachmentmodel "github.com/alanchenchen/suna/internal/tui/components/attachment"
	textutil "github.com/alanchenchen/suna/internal/tui/components/text"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
)

const maxSystemMarkdownBytes = 4000

// transcriptBlockIndent 统一主流程 block（tool/thinking/subtask）的左缩进，避免同级块左右错位。
const transcriptBlockIndent = "  "

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
	// spinner 与耗时都延迟到 viewChat() 中替换，保持 transcript 签名稳定且耗时实时更新。
	return fmt.Sprintf("    %s %s%s\n", spinnerPlaceholder, styleDim.Render(label), styleDim.Render(phaseElapsedPlaceholder))
}
func (t *TUI) renderCompactStatusLine() string {
	// 同 renderCurrentStatusLine，spinner 与耗时都延迟到 viewChat() 中替换。
	return fmt.Sprintf("    %s%s\n", spinnerPlaceholder, styleDim.Render(phaseElapsedPlaceholder))
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
	state := msg.Stream
	if state == nil {
		return ""
	}
	if width <= 0 {
		width = 20
	}
	tailLines := t.streamingRenderTailLines()
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
