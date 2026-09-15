package chat

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// 主题列表项必须真正渲染出名称：delegate 缺少 ThemeItem 分支时列表会显示空行。
func TestThemeItemRendersNameAndError(t *testing.T) {
	d := nativeDelegate{styles: ListStyles{Text: lipgloss.NewStyle()}}

	ok := d.renderItem(60, false, ThemeItem{Name: "nord", Swatch: "[swatch]"})
	if !strings.Contains(ok, "nord") {
		t.Fatalf("theme item must render its name, got %q", ok)
	}
	if !strings.Contains(ok, "[swatch]") {
		t.Fatalf("theme item must render its swatch, got %q", ok)
	}

	broken := d.renderItem(60, false, ThemeItem{Name: "broken", Err: "colors.accent is required"})
	if !strings.Contains(broken, "broken") {
		t.Fatalf("broken theme must still render its name, got %q", broken)
	}
	if !strings.Contains(broken, "accent") {
		t.Fatalf("broken theme must render the parse error, got %q", broken)
	}
}

// 不可用主题不携带色块，列表里用错误标记代替。
func TestThemeItemWithoutSwatchStillRenders(t *testing.T) {
	d := nativeDelegate{styles: ListStyles{Text: lipgloss.NewStyle()}}
	out := d.renderItem(60, false, ThemeItem{Name: "plain"})
	if !strings.Contains(out, "plain") {
		t.Fatalf("theme item without swatch must render its name, got %q", out)
	}
}
