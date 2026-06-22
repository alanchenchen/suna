package attachment

import "testing"

func TestNewImageDataPasteFiltersNonImages(t *testing.T) {
	if p, ok, blocked := NewImageDataPaste("clipboard_image", "clip", "", []byte("not an image")); ok || blocked || p.Data != nil {
		t.Fatalf("NewImageDataPaste(non-image) = (%+v, %v, %v), want ignored", p, ok, blocked)
	}
}

func TestNewImageDataPasteAcceptsPNGAndBlocksLargeData(t *testing.T) {
	pngData := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 'I', 'H', 'D', 'R'}
	p, ok, blocked := NewImageDataPaste("clipboard_image", "clip", "", pngData)
	if !ok || blocked {
		t.Fatalf("NewImageDataPaste(png) ok=%v blocked=%v", ok, blocked)
	}
	if p.MimeType != "image/png" || p.Name != "clip.png" || string(p.Data) != string(pngData) {
		t.Fatalf("pending = %+v, want png pending", p)
	}
	large := make([]byte, MaxPastedImageBytes+1)
	if _, ok, blocked := NewImageDataPaste("clipboard_image", "clip", "image/png", large); ok || !blocked {
		t.Fatalf("NewImageDataPaste(large) ok=%v blocked=%v, want blocked", ok, blocked)
	}
}
