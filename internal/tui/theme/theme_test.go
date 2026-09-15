package theme

import (
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseHexColor(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want bool
	}{
		{name: "valid lowercase", in: "#aabbcc", want: true},
		{name: "valid uppercase", in: "#AABBCC", want: true},
		{name: "missing hash", in: "aabbcc", want: false},
		{name: "short form", in: "#abc", want: false},
		{name: "ansi index", in: "14", want: false},
		{name: "named color", in: "red", want: false},
		{name: "bad digits", in: "#gggggg", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := parseHex(tc.in)
			if ok != tc.want {
				t.Fatalf("parseHex(%q) ok = %v, want %v", tc.in, ok, tc.want)
			}
		})
	}
}

// 适配的核心契约：与终端背景匹配的一侧原样使用，不匹配的一侧才调整。
func TestAdaptThemeKeepsMatchingSideUnchanged(t *testing.T) {
	colors := Colors{
		Accent: "#a6e3a1", Success: "#a6e3a1", Info: "#89b4fa",
		Warning: "#f9e2af", Error: "#f38ba8",
		Text: "#cdd6f4", Muted: "#a6adc8", Dim: "#6c7086", Surface: "#313244",
	}
	palette := Adapt("t", colors, DarkBackground)
	if got := ColorString(palette.Accent); got != "#a6e3a1" {
		t.Fatalf("dark-adapted accent = %q, want unchanged #a6e3a1", got)
	}
	if got := ColorString(palette.Text); got != "#cdd6f4" {
		t.Fatalf("dark-adapted text = %q, want unchanged #cdd6f4", got)
	}
}

func TestAdaptThemeDarkensLightTerminalColors(t *testing.T) {
	colors := Colors{
		Accent: "#a6e3a1", Success: "#a6e3a1", Info: "#89b4fa",
		Warning: "#f9e2af", Error: "#f38ba8",
		Text: "#cdd6f4", Muted: "#a6adc8", Dim: "#6c7086", Surface: "#313244",
	}
	palette := Adapt("t", colors, LightBackground)
	bg := LightBackground.baseline()

	// 浅色终端上，原本为深色终端设计的亮色必须被压暗以满足对比度。
	if ColorString(palette.Text) == "#cdd6f4" {
		t.Fatal("light adaptation must not keep a bright text color unchanged")
	}
	if ratio := contrastRatio(toRGBA(palette.Text), bg); ratio < contrastTextRatio {
		t.Fatalf("light-adapted text contrast = %.2f, want >= %.2f", ratio, contrastTextRatio)
	}
}

// toRGBA 把调色板里的颜色转回 RGBA 以便断言对比度。
func toRGBA(c color.Color) color.RGBA {
	r, g, b, a := c.RGBA()
	return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
}

// 色块文字色必须与背景形成对比，否则浅色主题下标签文字会看不见。
func TestOnColorContrastsWithBackground(t *testing.T) {
	for _, bgHex := range []string{"#a6e3a1", "#f9e2af", "#f38ba8", "#313244", "#ffffff", "#000000"} {
		bg, _ := parseHex(bgHex)
		on := onColor(bg)
		if ratio := contrastRatio(on, bg); ratio < 4.0 {
			t.Fatalf("onColor(%s) contrast = %.2f, want >= 4.0", bgHex, ratio)
		}
	}
}

func TestValidateThemeColors(t *testing.T) {
	base := func() Colors {
		return Colors{
			Accent: "#a6e3a1", Success: "#a6e3a1", Info: "#89b4fa",
			Warning: "#f9e2af", Error: "#f38ba8",
		}
	}
	if err := validateColors(func() *Colors { c := base(); return &c }()); err != nil {
		t.Fatalf("valid colors rejected: %v", err)
	}

	missing := base()
	missing.Accent = ""
	if err := validateColors(&missing); err == nil || !strings.Contains(err.Error(), "accent") {
		t.Fatalf("missing accent error = %v, want mention of accent", err)
	}

	bad := base()
	bad.Info = "blue"
	if err := validateColors(&bad); err == nil || !strings.Contains(err.Error(), "info") {
		t.Fatalf("invalid info error = %v, want mention of info", err)
	}
}

func TestLoadUserThemesSkipsBrokenAndReservesDefault(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "good.toml"), []byte(
		"[colors]\naccent=\"#a6e3a1\"\nsuccess=\"#a6e3a1\"\ninfo=\"#89b4fa\"\nwarning=\"#f9e2af\"\nerror=\"#f38ba8\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.toml"), []byte("not = [valid"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "default.toml"), []byte("[colors]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("ignored"), 0644); err != nil {
		t.Fatal(err)
	}

	specs := LoadUser(dir)
	if len(specs) != 3 {
		t.Fatalf("len(specs) = %d, want 3 (good, broken, default)", len(specs))
	}
	byName := map[string]Spec{}
	for _, s := range specs {
		byName[s.Name] = s
	}
	if byName["good"].Err != "" {
		t.Fatalf("good theme must parse, got error %q", byName["good"].Err)
	}
	if byName["broken"].Err == "" {
		t.Fatal("broken theme must report an error")
	}
	if byName["default"].Err == "" {
		t.Fatal("user file named default must be rejected to protect the builtin")
	}
	if _, ok := byName["notes"]; ok {
		t.Fatal("non-toml files must be ignored")
	}
}

func TestResolveThemePaletteFallsBackToDefault(t *testing.T) {
	for _, name := range []string{"missing", "broken"} {
		specs := []Spec{{Name: "broken", Err: "bad"}}
		palette := Resolve(name, specs, DarkBackground)
		if palette.Name != Default {
			t.Fatalf("Resolve(%q) = %q, want fallback to %q", name, palette.Name, Default)
		}
	}
}

func TestDefaultThemeIsModernDarkPalette(t *testing.T) {
	dark := Adapt(Default, DefaultColors(), DarkBackground)
	if !dark.Dark {
		t.Fatal("default dark adaptation must report Dark")
	}
	// 五个语义色必须彼此可区分，否则状态无法靠颜色辨认。
	seen := map[string]bool{}
	for _, c := range []color.Color{
		dark.Accent, dark.Success, dark.Info, dark.Warning, dark.Error,
	} {
		key := ColorString(c)
		if seen[key] {
			t.Fatalf("semantic colors must be distinct, duplicate %s", key)
		}
		seen[key] = true
	}
}

// TestDimKeepsAuthorIntent 锁定装饰色的适配策略：
// dim 是纯装饰（边框、分隔线），只防「完全看不见」，不抬到正文标准。
// 否则用户写不出想要的暗色边框——这正是自定义主题时最容易被劝退的地方。
func TestDimKeepsAuthorIntent(t *testing.T) {
	// 一个在深底上几乎不可见的装饰色。
	colors := DefaultColors()
	colors.Dim = "#3a3a3a"

	p := Adapt("t", colors, DarkBackground)
	bg := DarkBackground.baseline()
	ratio := contrastRatio(p.Dim.(color.RGBA), bg)

	// 必须被抬到可见（>= contrastDimRatio），但不得被抬到正文标准。
	if ratio < contrastDimRatio {
		t.Fatalf("dim contrast = %.2f, want >= %.2f (装饰色仍需可见)", ratio, contrastDimRatio)
	}
	if ratio >= contrastTextRatio {
		t.Fatalf("dim contrast = %.2f, want < %.2f (装饰色不应被抬到正文标准，否则用户写不出暗色边框)",
			ratio, contrastTextRatio)
	}
}

// TestAdaptUsesDetectedBackground 锁定对比度基准：
// 适配必须相对检测到的真实终端底色计算，而不是固定的近黑/近白。
// 否则偏亮的深色终端（如 Nord #2e3440）上，按近黑基准达标的颜色会实际不足。
func TestAdaptUsesDetectedBackground(t *testing.T) {
	colors := DefaultColors()

	// 同一个主题、同为深色终端，但底色亮度不同。
	pureBlack := Background{Dark: true, RGB: color.RGBA{R: 0x00, G: 0x00, B: 0x00, A: 0xff}}
	lightGray := Background{Dark: true, RGB: color.RGBA{R: 0x3a, G: 0x3a, B: 0x3a, A: 0xff}}

	darkPalette := Adapt("t", colors, pureBlack)
	grayPalette := Adapt("t", colors, lightGray)

	// 偏亮底色上必须给出更亮的颜色，才能保持同样的对比度。
	darkLum := relativeLuminance(darkPalette.Text.(color.RGBA))
	grayLum := relativeLuminance(grayPalette.Text.(color.RGBA))
	if grayLum < darkLum {
		t.Fatalf("text luminance on gray bg = %.4f, want >= %.4f (on pure black)", grayLum, darkLum)
	}

	// 两种底色下都必须达标。
	for name, pair := range map[string][2]color.RGBA{
		"black": {darkPalette.Text.(color.RGBA), pureBlack.RGB},
		"gray":  {grayPalette.Text.(color.RGBA), lightGray.RGB},
	} {
		if ratio := contrastRatio(pair[0], pair[1]); ratio < contrastTextRatio {
			t.Errorf("%s bg text contrast = %.2f, want >= %.2f", name, ratio, contrastTextRatio)
		}
	}
}

// TestDefaultSemanticColorsHaveEvenWeight 锁定内置 default 的语义色权重契约：
// 五个语义色在深色终端上的对比度必须彼此接近。
// 否则某个状态色会比别的更抢眼（例如亮黄的工具名盖过正文），
// 或者本该醒目的错误色反而最弱——颜色应只表达"是什么"，不额外表达"多重要"。
func TestDefaultSemanticColorsHaveEvenWeight(t *testing.T) {
	p := Adapt(Default, DefaultColors(), DarkBackground)
	bg := DarkBackground.baseline()

	colors := map[string]color.Color{
		"accent":  p.Accent,
		"success": p.Success,
		"info":    p.Info,
		"warning": p.Warning,
		"error":   p.Error,
	}

	min, max := 0.0, 0.0
	for name, c := range colors {
		ratio := contrastRatio(c.(color.RGBA), bg)
		if ratio < 4.5 {
			t.Errorf("%s contrast = %.2f, want >= 4.5 (语义色必须可读)", name, ratio)
		}
		if min == 0 || ratio < min {
			min = ratio
		}
		if ratio > max {
			max = ratio
		}
	}

	// 权重离散度：最亮与最暗的语义色不应相差过大，否则视觉权重失衡。
	if max/min > 1.35 {
		t.Errorf("semantic contrast spread = %.2f..%.2f (%.2fx), want <= 1.35x",
			min, max, max/min)
	}
}

// TestDefaultPaletteContrastTiers 锁定三级文字色阶的对比度契约：
// Text 是用户要读的正文（WCAG AA 正文标准），Muted 是次要正文（仍须可读），
// Dim 只用于装饰，允许低对比但必须比 Muted 更弱，否则层级消失。
func TestDefaultPaletteContrastTiers(t *testing.T) {
	p := Adapt(Default, DefaultColors(), DarkBackground)
	bg := DarkBackground.baseline()

	text := contrastRatio(p.Text.(color.RGBA), bg)
	muted := contrastRatio(p.Muted.(color.RGBA), bg)
	dim := contrastRatio(p.Dim.(color.RGBA), bg)

	if text < 7 {
		t.Errorf("text contrast = %.2f, want >= 7 (正文必须清晰)", text)
	}
	if muted < 4.0 {
		t.Errorf("muted contrast = %.2f, want >= 4.0 (次要正文仍须可读)", muted)
	}
	if dim >= muted {
		t.Errorf("dim contrast = %.2f >= muted %.2f, want dim weaker (层级必须递减)", dim, muted)
	}
}
