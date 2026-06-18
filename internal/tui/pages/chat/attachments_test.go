package chat

import (
	"testing"

	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/tui/components/attachment"
)

func TestAddConfirmedImageAttachmentSkipsDuplicatePath(t *testing.T) {
	var m Model
	first := &attachment.PendingImagePaste{SourceKind: protocol.AttachmentKindPath, Path: "/tmp/a.png", Name: "a.png", MimeType: "image/png", Size: 10}
	duplicate := &attachment.PendingImagePaste{SourceKind: protocol.AttachmentKindPath, Path: "/tmp/a.png", Name: "copy.png", MimeType: "image/png", Size: 10}

	m.AddConfirmedImageAttachment(first)
	m.AddConfirmedImageAttachment(duplicate)

	if got, want := len(m.Attachments), 1; got != want {
		t.Fatalf("len(Attachments) = %d, want %d", got, want)
	}
	if got, want := m.AttachmentCursor, 0; got != want {
		t.Fatalf("AttachmentCursor = %d, want %d", got, want)
	}
}

func TestAddConfirmedImageAttachmentSkipsDuplicateURL(t *testing.T) {
	var m Model
	first := &attachment.PendingImagePaste{SourceKind: protocol.AttachmentKindURL, URL: "https://example.com/a.png", Name: "a.png", MimeType: "image/png"}
	duplicate := &attachment.PendingImagePaste{SourceKind: protocol.AttachmentKindURL, URL: "https://example.com/a.png", Name: "again.png", MimeType: "image/png"}

	m.AddConfirmedImageAttachment(first)
	m.AddConfirmedImageAttachment(duplicate)

	if got, want := len(m.Attachments), 1; got != want {
		t.Fatalf("len(Attachments) = %d, want %d", got, want)
	}
}

func TestAddConfirmedImageAttachmentAllowsDifferentSources(t *testing.T) {
	var m Model
	first := &attachment.PendingImagePaste{SourceKind: protocol.AttachmentKindPath, Path: "/tmp/a.png", Name: "a.png", MimeType: "image/png"}
	second := &attachment.PendingImagePaste{SourceKind: protocol.AttachmentKindPath, Path: "/tmp/b.png", Name: "b.png", MimeType: "image/png"}

	m.AddConfirmedImageAttachment(first)
	m.AddConfirmedImageAttachment(second)

	if got, want := len(m.Attachments), 2; got != want {
		t.Fatalf("len(Attachments) = %d, want %d", got, want)
	}
	if got, want := m.AttachmentCursor, 1; got != want {
		t.Fatalf("AttachmentCursor = %d, want %d", got, want)
	}
}
