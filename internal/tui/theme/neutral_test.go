package theme

import (
	"image/color"
	"testing"
)

// 中性色在浅底适配时必须保留「绝对彩度」而不是 HSL 饱和度：
// 近白的浅蓝正文（#e4e9f7，HSL S=54%）若保持 HSL 饱和度压暗，
// 会变成高饱和蓝 #4e6fcb，让整个界面染上蓝调。
func TestNeutralKeepsAbsoluteChromaOnLightBackground(t *testing.T) {
	light := Background{Dark: false}
	p := Adapt("default", DefaultColors(), light)
	text, _ := p.Text.(color.RGBA)

	if got := absoluteChroma(text); got > 0.12 {
		t.Fatalf("light text chroma = %.3f, want <= 0.12 (近中性灰)；颜色 %s 会让界面整体偏色", got, formatHex(text))
	}
	// 仍应保留主题色相（冷调蓝），只是彩度很低。
	if h := rgbToHSL(text).H; h < 200 || h > 245 {
		t.Fatalf("light text hue = %.1f, want 200..245（保留原主题色温）", h)
	}
	// 对比度必须达标。
	if r := contrastRatio(text, light.baseline()); r < contrastTextRatio {
		t.Fatalf("light text contrast = %.2f, want >= %.1f", r, contrastTextRatio)
	}
}

// 深底上中性色本就匹配，必须原样使用（不因适配而改动）。
func TestNeutralUnchangedOnMatchingBackground(t *testing.T) {
	dark := Background{Dark: true}
	c := DefaultColors()
	p := Adapt("default", c, dark)

	text, _ := p.Text.(color.RGBA)
	want, _ := parseHex(c.Text)
	if text != want {
		t.Fatalf("dark text = %s, want %s (匹配终端必须原样)", formatHex(text), c.Text)
	}
}
