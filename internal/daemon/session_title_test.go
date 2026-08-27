package daemon

import (
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/protocol"
)

func TestAutoTitleFromPartsUsesFirstTextLine(t *testing.T) {
	got := autoTitleFromParts([]protocol.MessagePart{
		{Type: "text", Text: "  修复登录问题\n补充测试  "},
		{Type: "image", Source: protocol.AttachmentRef{Name: "screen.png"}},
	})
	if want := "修复登录问题"; got != want {
		t.Fatalf("autoTitleFromParts() = %q, want %q", got, want)
	}
}

func TestAutoTitleFromPartsUsesImageWhenTextIsMissing(t *testing.T) {
	got := autoTitleFromParts([]protocol.MessagePart{{
		Type:   "image",
		Source: protocol.AttachmentRef{Name: "screen.png"},
	}})
	if want := "Inspect image: screen.png"; got != want {
		t.Fatalf("autoTitleFromParts() = %q, want %q", got, want)
	}
}

func TestAutoTitleFromPartsTruncatesLongTitle(t *testing.T) {
	got := autoTitleFromParts([]protocol.MessagePart{{
		Type: "text",
		Text: strings.Repeat("a", autoTitleMaxRunes+10),
	}})
	if got != strings.Repeat("a", autoTitleMaxRunes) {
		t.Fatalf("autoTitleFromParts() length = %d, want %d", len([]rune(got)), autoTitleMaxRunes)
	}
}
