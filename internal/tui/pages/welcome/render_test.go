package welcome

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// TestRenderGradientBrandChinese 验证渐变函数对中文输入不越界：
// range 字符串的 i 是字节偏移，按 rune 索引取色才能覆盖多字节字符。
func TestRenderGradientBrandChinese(t *testing.T) {
	deps := ViewDeps{
		Brand: lipgloss.NewStyle().Foreground(lipgloss.Color("#00A8FF")),
		HL:    lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")),
	}
	for _, text := range []string{"Suna", "技能", "MCP 服务器", "记忆"} {
		out := renderGradientBrand(text, deps)
		if !strings.Contains(out, "\x1b[") {
			t.Fatalf("renderGradientBrand(%q) missing ANSI codes", text)
		}
	}
}
