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
