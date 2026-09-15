package tui

import (
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	textutil "github.com/alanchenchen/suna/internal/tui/components/text"
	themesys "github.com/alanchenchen/suna/internal/tui/theme"
)

// 语法高亮必须由主题调色板驱动：若回退到 chroma 内置主题，
// 代码块配色会与主题脱节，用户自定义主题时代码块仍是固定配色。
func TestMarkdownCodeBlockUsesThemeChroma(t *testing.T) {
	style := markdownStyleConfig()
	if got := style.CodeBlock.Theme; got != "" {
		t.Fatalf("CodeBlock.Theme = %q, want empty (theme-driven chroma)", got)
	}
	chroma := style.CodeBlock.Chroma
	if chroma == nil {
		t.Fatal("CodeBlock.Chroma is nil, want theme-derived config")
	}
	// 关键字取品牌色、字符串取正向色：验证映射确实来自调色板。
	wantKeyword := themesys.ColorString(currentTheme.Accent)
	if got := chroma.Keyword.Color; got == nil || *got != wantKeyword {
		t.Fatalf("chroma.Keyword.Color = %v, want %q", got, wantKeyword)
	}
	wantString := themesys.ColorString(currentTheme.Success)
	if got := chroma.LiteralString.Color; got == nil || *got != wantString {
		t.Fatalf("chroma.LiteralString.Color = %v, want %q", got, wantString)
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

func TestResolveThemePaletteFollowsTerminalBackground(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 120, height: 40}
	tui.setTheme(themesys.Default)

	tui.applyDetectedBackground(tea.BackgroundColorMsg{Color: color.RGBA{0, 0, 0, 255}})
	if !currentTheme.Dark {
		t.Fatal("default theme must adapt to a dark terminal")
	}
	darkText := currentTheme.Text

	tui.applyDetectedBackground(tea.BackgroundColorMsg{Color: color.RGBA{255, 255, 255, 255}})
	if currentTheme.Dark {
		t.Fatal("default theme must adapt to a light terminal")
	}
	if currentTheme.Text == darkText {
		t.Fatal("light adaptation must produce a different text color")
	}
}

func TestNormalizeThemeNameMigratesLegacyValues(t *testing.T) {
	for _, legacy := range []string{"auto", "dark", "light", ""} {
		if got := themesys.Normalize(legacy); got != themesys.Default {
			t.Fatalf("themesys.Normalize(%q) = %q, want %q", legacy, got, themesys.Default)
		}
	}
	if got := themesys.Normalize("mytheme"); got != "mytheme" {
		t.Fatalf("custom theme name must be preserved, got %q", got)
	}
}

// 终端背景变化后，所有主题都应重新适配：default 与用户主题都依赖它决定深浅。
func TestBackgroundColorMessageReadaptsTheme(t *testing.T) {
	tui := New(LocaleEN)
	tui.ready = true
	tui.setTheme(themesys.Default)

	_, _ = tui.Update(tea.BackgroundColorMsg{Color: color.RGBA{0, 0, 0, 255}})
	if !currentTheme.Dark {
		t.Fatal("dark terminal background must select the dark adaptation")
	}

	_, _ = tui.Update(tea.BackgroundColorMsg{Color: color.RGBA{255, 255, 255, 255}})
	if currentTheme.Dark {
		t.Fatal("light terminal background must select the light adaptation")
	}
}
