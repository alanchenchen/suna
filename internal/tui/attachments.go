package tui

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/alanchenchen/suna/internal/protocol"
)

const maxPastedImageBytes = 10 * 1024 * 1024

type attachmentItem struct {
	Type       string
	SourceKind string
	Path       string
	URL        string
	Name       string
	MimeType   string
	Size       int64
}

func (t *TUI) renderUserMessage(content any, width int) string {
	switch v := content.(type) {
	case userMessageContent:
		text := renderInlineUserMessage(v.text, width)
		if len(v.attachments) == 0 {
			return text
		}
		return text + "\n" + indentLines(t.renderAttachmentList(v.attachments, -1, false), "  ")
	case string:
		return renderInlineUserMessage(v, width)
	default:
		return renderInlineUserMessage(fmt.Sprint(v), width)
	}
}

func (t *TUI) renderAttachmentPanel() string {
	if t.pendingImagePaste != nil {
		return t.renderPendingImagePaste()
	}
	if len(t.attachments) == 0 {
		return ""
	}
	return t.renderAttachmentList(t.attachments, t.attachmentCursor, t.attachmentMode) + "\n" + t.attachmentHelp()
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
	p := t.pendingImagePaste
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
	if t.attachmentDelete && len(t.attachments) > 0 {
		name := t.attachments[t.attachmentCursor].Name
		return styleError.Render(t.tr("tui.attachment.delete")+" "+name+"?") + " " + styleDim.Render(t.tr("tui.attachment.delete_help"))
	}
	if t.attachmentMode {
		return styleDim.Render(t.tr("tui.attachment.mode_help"))
	}
	return styleDim.Render(t.tr("tui.attachment.normal_help"))
}

type pendingImagePaste struct {
	Raw        string
	SourceKind string
	Path       string
	URL        string
	Name       string
	MimeType   string
	Size       int64
	Data       []byte
}

func (t *TUI) handlePaste(content string) tea.Cmd {
	pending, ok, blocked := detectImagePaste(content)
	if blocked {
		t.messages = append(t.messages, chatMsg{role: "error", content: t.tr("tui.attachment.base64_blocked")})
		return nil
	}
	if !ok {
		t.ta.InsertString(content)
		t.layoutChat()
		return nil
	}
	t.pendingImagePaste = &pending
	t.attachmentMode = false
	t.attachmentDelete = false
	return nil
}

func (t *TUI) updatePendingImagePaste(key string) tea.Cmd {
	if t.pendingImagePaste == nil {
		return nil
	}
	switch key {
	case "enter", "y", "Y":
		return t.confirmPendingImagePaste()
	case "esc", "n", "N":
		p := t.pendingImagePaste
		t.pendingImagePaste = nil
		if p.SourceKind == "path" || p.SourceKind == "url" {
			t.ta.InsertString(p.Raw)
			t.layoutChat()
		}
	}
	return nil
}

func (t *TUI) confirmPendingImagePaste() tea.Cmd {
	p := t.pendingImagePaste
	if p == nil {
		return nil
	}
	t.pendingImagePaste = nil
	if p.SourceKind == "data_uri" {
		path, name, size, err := savePastedImage(p)
		if err != nil {
			t.messages = append(t.messages, chatMsg{role: "error", content: err.Error()})
			return nil
		}
		p.SourceKind = "path"
		p.Path = path
		p.Name = name
		p.Size = size
	}
	t.attachments = append(t.attachments, attachmentItem{Type: "image", SourceKind: p.SourceKind, Path: p.Path, URL: p.URL, Name: p.Name, MimeType: p.MimeType, Size: p.Size})
	t.attachmentCursor = len(t.attachments) - 1
	return nil
}

func (t *TUI) updateAttachmentMode(key string) bool {
	if t.attachmentDelete {
		switch key {
		case "enter", "y", "Y":
			t.deleteSelectedAttachment()
		case "esc", "n", "N":
			t.attachmentDelete = false
		}
		return true
	}
	if t.attachmentMode {
		switch key {
		case "up":
			if t.attachmentCursor > 0 {
				t.attachmentCursor--
			}
		case "down":
			if t.attachmentCursor < len(t.attachments)-1 {
				t.attachmentCursor++
			}
		case "delete", "backspace":
			if len(t.attachments) > 0 {
				t.attachmentDelete = true
			}
		case "esc":
			t.attachmentMode = false
		case "enter":
			t.attachmentMode = false
		default:
			return false
		}
		return true
	}
	if len(t.attachments) > 0 && (key == "up" || key == "down") {
		t.attachmentMode = true
		if key == "up" {
			t.attachmentCursor = len(t.attachments) - 1
		} else {
			t.attachmentCursor = 0
		}
		return true
	}
	return false
}

func (t *TUI) deleteSelectedAttachment() {
	if t.attachmentCursor < 0 || t.attachmentCursor >= len(t.attachments) {
		return
	}
	t.attachments = append(t.attachments[:t.attachmentCursor], t.attachments[t.attachmentCursor+1:]...)
	t.attachmentDelete = false
	if len(t.attachments) == 0 {
		t.attachmentMode = false
		t.attachmentCursor = 0
		return
	}
	if t.attachmentCursor >= len(t.attachments) {
		t.attachmentCursor = len(t.attachments) - 1
	}
}

func detectImagePaste(raw string) (pendingImagePaste, bool, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return pendingImagePaste{}, false, false
	}
	if p, ok := detectDataImageURI(trimmed); ok {
		return p, true, false
	}
	if looksLikeLargeBase64(trimmed) {
		return pendingImagePaste{}, false, true
	}
	if p, ok := detectImageURL(trimmed); ok {
		p.Raw = raw
		return p, true, false
	}
	if p, ok := detectImagePath(trimmed); ok {
		p.Raw = raw
		return p, true, false
	}
	return pendingImagePaste{}, false, false
}

func detectImagePath(raw string) (pendingImagePaste, bool) {
	path := strings.Trim(raw, "'\"")
	path = strings.ReplaceAll(path, "\\ ", " ")
	path = expandTilde(path)
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || !isImageName(path) {
		return pendingImagePaste{}, false
	}
	return pendingImagePaste{SourceKind: "path", Path: path, Name: filepath.Base(path), MimeType: imageMimeFromName(path), Size: info.Size()}, true
}

func detectImageURL(raw string) (pendingImagePaste, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || !isImageName(u.Path) {
		return pendingImagePaste{}, false
	}
	name := filepath.Base(u.Path)
	if name == "." || name == "/" || name == "" {
		name = "remote-image"
	}
	return pendingImagePaste{SourceKind: "url", URL: raw, Name: name, MimeType: imageMimeFromName(u.Path)}, true
}

func detectDataImageURI(raw string) (pendingImagePaste, bool) {
	if !strings.HasPrefix(raw, "data:image/") {
		return pendingImagePaste{}, false
	}
	idx := strings.Index(raw, ";base64,")
	if idx < 0 {
		return pendingImagePaste{}, false
	}
	mimeType := raw[len("data:"):idx]
	encoded := raw[idx+len(";base64,"):]
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) == 0 || len(data) > maxPastedImageBytes {
		return pendingImagePaste{}, false
	}
	ext := extFromMime(mimeType)
	return pendingImagePaste{SourceKind: "data_uri", Name: "pasted-image" + ext, MimeType: mimeType, Size: int64(len(data)), Data: data}, true
}

func savePastedImage(p *pendingImagePaste) (string, string, int64, error) {
	home, _ := os.UserHomeDir()
	if home == "" {
		return "", "", 0, fmt.Errorf("cannot find home directory for pasted image")
	}
	dir := filepath.Join(home, ".suna", "tmp")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", "", 0, fmt.Errorf("create temp image dir: %w", err)
	}
	name := fmt.Sprintf("paste-%d%s", time.Now().UnixNano(), extFromMime(p.MimeType))
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, p.Data, 0600); err != nil {
		return "", "", 0, fmt.Errorf("save pasted image: %w", err)
	}
	return path, name, int64(len(p.Data)), nil
}

func (a attachmentItem) toPart() protocol.MessagePart {
	return protocol.MessagePart{Type: "image", Source: protocol.AttachmentRef{Kind: a.SourceKind, Path: a.Path, URL: a.URL, MimeType: a.MimeType, Name: a.Name, Size: a.Size}}
}

func isImageName(name string) bool { return imageMimeFromName(name) != "" }

func imageMimeFromName(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return ""
	}
}

func extFromMime(mimeType string) string {
	switch mimeType {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
}

func expandTilde(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		if home != "" {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func looksLikeLargeBase64(s string) bool {
	if len(s) < 1024 || strings.ContainsAny(s, " \n\t") {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '+' && r != '/' && r != '=' {
			return false
		}
	}
	return true
}

func formatAttachmentSize(n int64) string {
	if n <= 0 {
		return "-"
	}
	if n < 1024 {
		return fmt.Sprintf("%dB", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
}

func truncateMiddle(s string, maxWidth int) string {
	if maxWidth <= 0 || lipgloss.Width(s) <= maxWidth {
		return s
	}
	r := []rune(s)
	if len(r) <= maxWidth || maxWidth <= 3 {
		return string(r[:min(len(r), maxWidth)])
	}
	keep := maxWidth - 1
	left := keep / 2
	right := keep - left
	return string(r[:left]) + "…" + string(r[len(r)-right:])
}
