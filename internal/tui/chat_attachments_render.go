package tui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/alanchenchen/suna/internal/protocol"
	attachmentmodel "github.com/alanchenchen/suna/internal/tui/components/attachment"
	textutil "github.com/alanchenchen/suna/internal/tui/components/text"
)

func (t *TUI) handlePaste(content string) tea.Cmd {
	pending, ok, blocked := attachmentmodel.DetectImagePaste(content)
	if t.canSteerCurrentRun() && (ok || blocked) {
		t.appendNonToolMessage(chatMsg{Role: "error", Content: t.tr("tui.chat.queue_text_only")})
		return nil
	}
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
			prefix = styleCursor.Render("▎ ")
			st = styleHL
		}
		nameWidth := max(10, inner-22)
		line := fmt.Sprintf("%s%d  %-5s  %-*s  %s", prefix, i+1, item.Type, nameWidth, truncateMiddle(item.Name, nameWidth), formatAttachmentSize(item.Size))
		lines = append(lines, st.Render(line))
	}
	if len(items) > limit {
		lines = append(lines, styleMuted.Render(fmt.Sprintf("  +%d more", len(items)-limit)))
	}
	if strings.TrimSpace(help) != "" {
		if len(lines) > 0 {
			lines = append(lines, styleDim.Render(strings.Repeat("─", inner)))
		}
		lines = append(lines, styleMuted.Render(help))
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
			prefix = styleCursor.Render("▎ ")
			st = styleHL
		}
		line := fmt.Sprintf("%s%d  %-5s  %-24s  %s", prefix, i+1, item.Type, truncateMiddle(item.Name, 24), formatAttachmentSize(item.Size))
		lines = append(lines, st.Render(line))
	}
	if len(items) > limit {
		lines = append(lines, styleMuted.Render(fmt.Sprintf("  +%d more", len(items)-limit)))
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
	return styleHL.Render(title) + "\n" + styleMuted.Render(help)
}

func (t *TUI) attachmentHelp() string {
	if t.chat.AttachmentDelete && len(t.chat.Attachments) > 0 {
		name := t.chat.Attachments[t.chat.AttachmentCursor].Name
		return styleError.Render(t.tr("tui.attachment.delete")+" "+name+"?") + " " + styleMuted.Render(t.tr("tui.attachment.delete_help"))
	}
	if t.chat.AttachmentMode {
		return styleMuted.Render(t.tr("tui.attachment.mode_help"))
	}
	return styleMuted.Render(t.tr("tui.attachment.normal_help"))
}

func (t *TUI) savePastedImage(p *pendingImagePaste) (string, string, int64, error) {
	if t.currentSession.ID == "" || t.attachmentStatus.SessionID != t.currentSession.ID {
		return "", "", 0, fmt.Errorf("attachments directory is not ready for this session")
	}
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

// renderMediaSummary 只转换快照标记为 media 的消息开头的连续图片引用。
// ProjectMessages 以换行连接图片与文本块；正文中的类似引用可能是用户原文，
// 不能全文搜索替换。首个非引用行及其后的内容保持原样，source 仍保留在模型历史中。
// ● 点和缩进由 renderInlineUserMessage 添加，这里只返回内容部分。
func (t *TUI) renderMediaSummary(summary string) string {
	var out strings.Builder
	remaining := summary
	for {
		end := strings.IndexByte(remaining, '\n')
		if end < 0 {
			end = len(remaining)
		}
		line := remaining[:end]
		name, size, ok := parseMediaReference(line)
		if !ok {
			out.WriteString(remaining)
			break
		}
		out.WriteString(styleDim.Render("📷") + " " + styleMuted.Render(t.tr("tui.chat.media_image")))
		if name != "" {
			out.WriteString(" · " + styleMuted.Render(name))
		}
		if size != "" {
			out.WriteString(" · " + styleMuted.Render(size))
		}
		if end == len(remaining) {
			break
		}
		out.WriteByte('\n')
		remaining = remaining[end+1:]
	}
	return out.String()
}

// parseMediaSummary 保留单条引用的展示信息接口；不完整或非引用文本不被转换。
func parseMediaSummary(summary string) (name, size string) {
	name, size, _ = parseMediaReference(summary)
	return name, size
}

// parseMediaReference 只接受单行的既有图片引用格式，不从正文中截取子串。
// 格式本身不记录块来源，和引用完全同形的用户文字仍无法区分。
func parseMediaReference(line string) (name, size string, ok bool) {
	var inner string
	legacy := false
	switch {
	case strings.HasPrefix(line, "[image: "):
		inner = strings.TrimPrefix(line, "[image: ")
	case strings.HasPrefix(line, "[uploaded image: "):
		inner = strings.TrimPrefix(line, "[uploaded image: ")
		legacy = true
	default:
		return "", "", false
	}
	if !strings.HasSuffix(inner, "]") || strings.ContainsAny(inner, "\r\n") {
		return "", "", false
	}
	inner = strings.TrimSuffix(inner, "]")
	sourceIndex := strings.Index(inner, ", source=")
	if sourceIndex < 0 || sourceIndex != strings.LastIndex(inner, ", source=") {
		return "", "", false
	}
	source := strings.TrimSpace(inner[sourceIndex+len(", source="):])
	// 未转义的 ] 可能是引用之后的正文；不能将其连同正文一起隐藏。
	if source == "" || strings.Contains(source, "]") {
		return "", "", false
	}
	fields := inner[:sourceIndex]
	if idx := strings.LastIndex(fields, ", "); idx >= 0 && isMediaSize(fields[idx+2:]) {
		size = fields[idx+2:]
		fields = fields[:idx]
	}
	hasMime := false
	if idx := strings.LastIndex(fields, ", "); idx >= 0 && strings.HasPrefix(fields[idx+2:], "image/") && len(fields[idx+2:]) > len("image/") {
		fields = fields[:idx]
		hasMime = true
	}
	// 无 MIME 的引用仍可由明确的 attachment、URL 或绝对路径识别。
	// 任意 source= 文字不足以证明这是图片引用。
	if !hasMime && !strings.HasPrefix(source, "attachment:") && !strings.HasPrefix(source, "http://") && !strings.HasPrefix(source, "https://") && !filepath.IsAbs(source) && !(legacy && source == "attachment") {
		return "", "", false
	}
	name = strings.TrimSpace(fields)
	if name == "" {
		return "", "", false
	}
	return name, size, true
}

// isMediaSize 识别 media.FormatSize 生成的 B、KB、MB 大小，避免把文件名中的数字当作大小。
func isMediaSize(value string) bool {
	switch {
	case strings.HasSuffix(value, "KB"), strings.HasSuffix(value, "MB"):
		value = value[:len(value)-2]
	case strings.HasSuffix(value, "B"):
		value = value[:len(value)-1]
	default:
		return false
	}
	digits, dot := 0, false
	for _, c := range value {
		if c >= '0' && c <= '9' {
			digits++
		} else if c == '.' && !dot && digits > 0 {
			dot = true
		} else {
			return false
		}
	}
	return digits > 0 && !strings.HasSuffix(value, ".")
}
