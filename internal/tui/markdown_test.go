package tui

import (
	"strings"
	"testing"
)

func TestMarkdownCodeBlockUsesThemeWithoutCustomChroma(t *testing.T) {
	style := markdownStyleConfig()
	if style.CodeBlock.Theme == "" {
		t.Fatal("code block theme should be set to enable language-aware highlighting")
	}
	if style.CodeBlock.Chroma != nil {
		t.Fatal("code block chroma should stay nil; use upstream Chroma themes instead of custom token backgrounds")
	}
}

func TestDefaultFenceLanguageOnlyAddsOpeningFence(t *testing.T) {
	input := "before\n```\necho hi\n```\nafter"
	out := defaultFenceLanguage(input)
	if !strings.Contains(out, "```bash\necho hi\n```") {
		t.Fatalf("expected empty opening fence to become bash fence:\n%s", out)
	}
	if strings.Count(out, "```bash") != 1 {
		t.Fatalf("expected only opening fence to get default language:\n%s", out)
	}
}

func TestWrapLineLimitStopsAfterRequestedLines(t *testing.T) {
	out := wrapLineLimit(strings.Repeat("x", 5000), 10, 2)
	if len(out) != 3 {
		t.Fatalf("expected two wrapped lines plus ellipsis, got %d", len(out))
	}
	if out[2] != "..." {
		t.Fatalf("expected ellipsis marker, got %q", out[2])
	}
}
