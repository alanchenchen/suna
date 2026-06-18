package chat

import (
	"path/filepath"
	"strings"

	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/tui/components/attachment"
)

func (m *Model) EnqueueImagePaste(p attachment.PendingImagePaste) {
	m.EnqueueInteraction(Interaction{Kind: InteractionImagePasteConfirm, ID: "image_paste", ImagePaste: &p})
	m.AttachmentMode = false
	m.AttachmentDelete = false
}

func (m *Model) CancelPendingImagePaste() *attachment.PendingImagePaste {
	p := m.ActiveImagePaste()
	if p == nil {
		return nil
	}
	m.CancelActiveInteraction()
	return p
}

func (m *Model) AddConfirmedImageAttachment(p *attachment.PendingImagePaste) {
	if p == nil {
		return
	}
	item := attachment.Item{Type: "image", SourceKind: p.SourceKind, Path: p.Path, URL: p.URL, Name: p.Name, MimeType: p.MimeType, Size: p.Size}
	if idx := m.findDuplicateAttachment(item); idx >= 0 {
		m.AttachmentCursor = idx
		return
	}
	m.Attachments = append(m.Attachments, item)
	m.AttachmentCursor = len(m.Attachments) - 1
}

func (m *Model) findDuplicateAttachment(item attachment.Item) int {
	key := attachmentDedupKey(item)
	if key == "" {
		return -1
	}
	for i, existing := range m.Attachments {
		if attachmentDedupKey(existing) == key {
			return i
		}
	}
	return -1
}

func attachmentDedupKey(item attachment.Item) string {
	switch item.SourceKind {
	case protocol.AttachmentKindPath, protocol.AttachmentKindAttachment:
		path := strings.TrimSpace(item.Path)
		if path == "" {
			return ""
		}
		return "path:" + filepath.Clean(path)
	case protocol.AttachmentKindURL:
		url := strings.TrimSpace(item.URL)
		if url == "" {
			return ""
		}
		return item.SourceKind + ":" + url
	default:
		return ""
	}
}

func (m *Model) UpdateAttachmentMode(key string) bool {
	if m.AttachmentDelete {
		switch key {
		case "enter":
			m.DeleteSelectedAttachment()
		case "esc":
			m.AttachmentDelete = false
		}
		return true
	}
	if m.AttachmentMode {
		switch key {
		case "up":
			if m.AttachmentCursor > 0 {
				m.AttachmentCursor--
			}
		case "down":
			if m.AttachmentCursor < len(m.Attachments)-1 {
				m.AttachmentCursor++
			}
		case "delete", "backspace":
			if len(m.Attachments) > 0 {
				m.AttachmentDelete = true
			}
		case "esc":
			m.AttachmentMode = false
		default:
			return false
		}
		return true
	}
	if len(m.Attachments) > 0 && (key == "up" || key == "down") {
		m.AttachmentMode = true
		if key == "up" {
			m.AttachmentCursor = len(m.Attachments) - 1
		} else {
			m.AttachmentCursor = 0
		}
		return true
	}
	return false
}

func (m *Model) DeleteSelectedAttachment() {
	if m.AttachmentCursor < 0 || m.AttachmentCursor >= len(m.Attachments) {
		return
	}
	m.Attachments = append(m.Attachments[:m.AttachmentCursor], m.Attachments[m.AttachmentCursor+1:]...)
	m.AttachmentDelete = false
	if len(m.Attachments) == 0 {
		m.AttachmentMode = false
		m.AttachmentCursor = 0
		return
	}
	if m.AttachmentCursor >= len(m.Attachments) {
		m.AttachmentCursor = len(m.Attachments) - 1
	}
}

func (m *Model) PendingPasteShouldRestoreRaw(p *attachment.PendingImagePaste) bool {
	return p != nil && (p.SourceKind == protocol.AttachmentKindPath || p.SourceKind == protocol.AttachmentKindURL)
}
