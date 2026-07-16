package tui

import (
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	textutil "github.com/alanchenchen/suna/internal/tui/components/text"
)

func TestMarkdownCodeBlockUsesThemeWithoutCustomChroma(t *testing.T) {
	style := markdownStyleConfig()
	if got := style.CodeBlock.Theme; got == "" {
		t.Fatalf("CodeBlock.Theme = %q, want non-empty theme", got)
	}
	if got := style.CodeBlock.Chroma; got != nil {
		t.Fatalf("CodeBlock.Chroma = %#v, want nil", got)
	}
}

func TestDefaultFenceLanguageOnlyAddsOpeningFence(t *testing.T) {
	input := "before\n```\necho hi\n```\nafter"
	out := defaultFenceLanguage(input)
	if !strings.Contains(out, "```bash\necho hi\n```") {
		t.Fatalf("defaultFenceLanguage() = %q, want opening fence with bash", out)
	}
	if got := strings.Count(out, "```bash"); got != 1 {
		t.Fatalf("strings.Count(defaultFenceLanguage(), %q) = %d, want %d", "```bash", got, 1)
	}
}

func TestWrapLineLimitStopsAfterRequestedLines(t *testing.T) {
	out := textutil.WrapLineLimit(strings.Repeat("x", 5000), 10, 2)
	if got := len(out); got != 3 {
		t.Fatalf("len(textutil.WrapLineLimit()) = %d, want %d", got, 3)
	}
	if got := out[2]; got != "..." {
		t.Fatalf("textutil.WrapLineLimit()[2] = %q, want %q", got, "...")
	}
}

func TestResolveThemeUsesExpectedTheme(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		autoDark bool
		want     string
	}{
		{name: "auto uses light before terminal response", input: ThemeAuto, want: ThemeLight},
		{name: "auto uses dark terminal response", input: ThemeAuto, autoDark: true, want: ThemeDark},
		{name: "explicit dark", input: ThemeDark, want: ThemeDark},
		{name: "explicit light", input: ThemeLight, autoDark: true, want: ThemeLight},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveTheme(tt.input, tt.autoDark).Name; got != tt.want {
				t.Fatalf("resolveTheme(%q, %t).Name = %q, want %q", tt.input, tt.autoDark, got, tt.want)
			}
		})
	}
}

func TestBackgroundColorMessageOnlyChangesAutoTheme(t *testing.T) {
	applyTheme(ThemeDark)
	t.Cleanup(func() { applyTheme(ThemeDark) })

	tui := New(LocaleEN)
	tui.ready = true

	_, _ = tui.Update(tea.BackgroundColorMsg{Color: color.RGBA{0, 0, 0, 255}})
	if got := currentTheme.Name; got != ThemeDark {
		t.Fatalf("auto theme after dark background = %q, want %q", got, ThemeDark)
	}

	tui.setTheme(ThemeLight)
	_, _ = tui.Update(tea.BackgroundColorMsg{Color: color.RGBA{0, 0, 0, 255}})
	if got := currentTheme.Name; got != ThemeLight {
		t.Fatalf("explicit light theme after dark background = %q, want %q", got, ThemeLight)
	}
}
