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

	"github.com/alanchenchen/suna/internal/protocol"
	attachmentmodel "github.com/alanchenchen/suna/internal/tui/components/attachment"
	textutil "github.com/alanchenchen/suna/internal/tui/components/text"
)

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
	title := " ◎ " + t.tr("tui.chat.thinking") + " "
	if running {
		title = fmt.Sprintf(" ◎ %s %s %.1fs ", t.tr("tui.chat.thinking"), t.chat.Spinner.View(), elapsed.Seconds())
	} else if elapsed > 0 {
		title = fmt.Sprintf(" ◎ %s %.1fs ", t.tr("tui.chat.thinking"), elapsed.Seconds())
	}
	display := strings.TrimSpace(content)
	if running && display == "" {
		display = t.tr("status.thinking")
	}
	if !t.chat.ShowReasoningDetail {
		display = extractLastSentence(display)
		if display == "" {
			display = t.tr("tui.chat.thought_done")
		}
		display += "    [Ctrl+R " + t.tr("tui.key.reasoning_detail") + "]"
	} else {
		if running {
			display = renderStreamingText(strings.TrimSpace(content), inner)
		} else {
			display = RenderMarkdown(strings.TrimSpace(content), inner)
		}
	}
	lines := strings.Split(strings.TrimRight(display, "\n"), "\n")
	if running && !t.chat.ShowReasoningDetail && len(lines) > 4 {
		lines = append([]string{"..."}, lines[len(lines)-4:]...)
	}
	if t.chat.ShowReasoningDetail && len(lines) > 15 {
		if running {
			lines = append([]string{"..."}, lines[len(lines)-15:]...)
		} else {
			lines = append(lines[:15], "...")
		}
	}
	var sb strings.Builder
	sb.WriteString("    " + styleDim.Render("┌─"+title+strings.Repeat("─", max(0, width-lipgloss.Width(title)-3))+"┐") + "\n")
	for _, line := range lines {
		for _, wrapped := range textutil.WrapLine(line, inner) {
			sb.WriteString("    " + styleDim.Render("│ ") + wrapped + strings.Repeat(" ", max(0, inner-lipgloss.Width(wrapped))) + styleDim.Render(" │") + "\n")
		}
	}
	sb.WriteString("    " + styleDim.Render("└"+strings.Repeat("─", width-2)+"┘") + "\n")
	return sb.String()
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
	if strings.TrimSpace(p.Status) == "loading" {
		labelKey = "tui.skill.loading"
	}
	body := styleMetaPill.Render(t.tr(labelKey)) + " " + styleHL.Render(name)
	return textutil.IndentLines(boxStyle.BorderForeground(ColorBrand).Width(max(36, min(72, t.width-6))).Padding(1, 2).Render(body), "  ")
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
	lines := splitWrapped(body, inner, 18)
	content := title
	if len(lines) > 0 {
		content += "\n" + strings.Join(lines, "\n")
	}
	return textutil.IndentLines(boxStyle.BorderForeground(ColorBrand).Width(width).Padding(1, 2).Render(content), "  ")
}
func (t *TUI) hasVisibleActiveProgress() bool {
	if t.hasRunningTools() {
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
func (t *TUI) currentStatusLabel() string {
	if t.chat.StatusLabel != "" {
		return t.chat.StatusLabel
	}
	if n := t.runningToolCount(); n > 0 {
		return fmt.Sprintf("%s · %d running", t.tr("status.exec_tool"), n)
	}
	if t.chat.PendingAskID != "" {
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
		return t.tr("status.waiting_after_tool")
	default:
		return ""
	}
}

func extractLastSentence(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	sentences := strings.FieldsFunc(text, func(r rune) bool {
		return r == '.' || r == '。' || r == '\n'
	})
	for i := len(sentences) - 1; i >= 0; i-- {
		s := strings.TrimSpace(sentences[i])
		if s != "" {
			if len(s) > 80 {
				return s[:80] + "..."
			}
			return s
		}
	}
	return ""
}

func (t *TUI) renderRestoreSummaryBox(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	width := max(36, min(76, t.width-6))
	inner := max(20, width-8)
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && strings.Contains(lines[0], "：") {
		lines = lines[1:]
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
	title := styleHL.Render("上一轮操作摘要")
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
		// 流式阶段避免每个 delta 都跑 Glamour；最终 done 后仍使用完整 Markdown 渲染。
		return textutil.IndentLines(renderStreamingText(content, width), "  ")
	}
	return textutil.IndentLines(t.cachedMarkdown(msg, content, width), "  ")
}

func (t *TUI) renderReasoningMessage(msg *chatMsg) string {
	content, _ := msg.Content.(string)
	return t.renderThinkingBox(content, msg.Streaming, msg.StartedAt, msg.EndedAt)
}

func (t *TUI) cachedMarkdown(msg *chatMsg, content string, width int) string {
	cache := msg.Render
	if cache.Width == width && cache.Theme == currentTheme.Name && cache.Content == content {
		return cache.Output
	}
	out := RenderMarkdown(content, width)
	msg.Render = msgRenderCache{Width: width, Theme: currentTheme.Name, Content: content, Output: out}
	return out
}

func renderStreamingText(content string, width int) string {
	if content == "" {
		return ""
	}
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
	t.chat.SetPendingImagePaste(pending)
	return nil
}

func (t *TUI) updatePendingImagePaste(key string) tea.Cmd {
	if t.chat.PendingImagePaste == nil {
		return nil
	}
	switch key {
	case "enter":
		return t.confirmPendingImagePaste()
	case "esc":
		p := t.chat.CancelPendingImagePaste()
		if t.chat.PendingPasteShouldRestoreRaw(p) {
			t.chat.Textarea.InsertString(p.Raw)
			t.layoutChat()
		}
	}
	return nil
}

func (t *TUI) confirmPendingImagePaste() tea.Cmd {
	p := t.chat.PendingImagePaste
	if p == nil {
		return nil
	}
	t.chat.CancelPendingImagePaste()
	if p.SourceKind == "data_uri" {
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
		return t.renderPendingImagePaste()
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

func (t *TUI) renderPendingImagePaste() string {
	p := t.chat.PendingImagePaste
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
