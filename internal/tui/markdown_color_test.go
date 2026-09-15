package tui

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"
	"testing"

	themesys "github.com/alanchenchen/suna/internal/tui/theme"
)

// markdown 样式里的颜色必须能被 glamour 解析成 RGB 序列。
// 曾经用 fmt.Sprint(color.RGBA) 得到 "{230 233 247 255}"，glamour 静默回退成
// 纯黑，表现为正文异常发暗——本测试锁死这个契约。
func TestMarkdownColorsAreParseable(t *testing.T) {
	applyThemePalette(themesys.Adapt(themesys.Default, themesys.DefaultColors(), themesys.DarkBackground))

	for name, c := range map[string]color.Color{
		"text":    currentTheme.Text,
		"muted":   currentTheme.Muted,
		"dim":     currentTheme.Dim,
		"accent":  currentTheme.Accent,
		"surface": currentTheme.Surface,
	} {
		got := *colorPtr(c)
		if !strings.HasPrefix(got, "#") || len(got) != 7 {
			t.Fatalf("colorPtr(%s) = %q, want #rrggbb", name, got)
		}
	}

	out := RenderMarkdown("正文文本", 40)
	if strings.Contains(out, "38;2;0;0;0") {
		t.Fatalf("markdown fell back to black: %q", out)
	}
	// 期望值从当前主题派生，避免硬编码色值：改配色不应破坏契约测试。
	wantRGB := strings.ReplaceAll(*colorPtr(currentTheme.Text), "#", "")
	wantSeq := "38;2;" + fmt.Sprintf("%d;%d;%d",
		rgbComponent(wantRGB, 0), rgbComponent(wantRGB, 1), rgbComponent(wantRGB, 2))
	if !strings.Contains(out, wantSeq) {
		t.Fatalf("markdown did not use theme text color %s: %q", wantSeq, out)
	}
}

// rgbComponent 从 "rrggbb" 取第 i 个字节的十进制值。
func rgbComponent(hex string, i int) int {
	v, _ := strconv.ParseInt(hex[i*2:i*2+2], 16, 32)
	return int(v)
}
