package tui

import (
	"image/color"
	"testing"

	themesys "github.com/alanchenchen/suna/internal/tui/theme"
)

// testBackground 把深浅布尔转成适配基准，供表驱动测试使用。
func testBackground(dark bool) themesys.Background {
	if dark {
		return themesys.DarkBackground
	}
	return themesys.LightBackground
}

// 色块的前景必须与背景配对（On* 由对应语义色算出），否则浅色主题下
// 文字可能落在对比不足的组合上。这里锁死 onFor 的配对关系。
func TestPillPairsForegroundWithBackground(t *testing.T) {
	for _, dark := range []bool{true, false} {
		applyThemePalette(themesys.Adapt(themesys.Default, themesys.DefaultColors(), testBackground(dark)))

		cases := []struct {
			name string
			bg   color.Color
			want color.Color
		}{
			{"accent", ColorAccent, currentTheme.OnAccent},
			{"success", ColorSuccess, currentTheme.OnSuccess},
			{"warning", ColorWarning, currentTheme.OnWarning},
			{"error", ColorError, currentTheme.OnError},
		}
		for _, tc := range cases {
			if got := onFor(tc.bg); got != tc.want {
				t.Fatalf("dark=%v %s: onFor returned mismatched foreground", dark, tc.name)
			}
		}
	}
}
