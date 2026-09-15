package tui

import (
	"testing"

	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
	themesys "github.com/alanchenchen/suna/internal/tui/theme"
)

// 主题预览是内存态：只有 Enter 才落库。任何强制关闭（切 session、离开配置页）
// 都必须恢复进入列表前的主题，否则界面会停留在未落库的预览值上。
func TestThemePreviewRestoredOnForcedClose(t *testing.T) {
	tui := newEdgeTUI(t)
	tui.setTheme(themesys.Default)
	before := tui.theme

	// 打开列表 → 预览另一个主题（不按 Enter）。
	// 预览名必须登记在 themeSpecs 里：主题名归一按“可用集合”判定。
	// 这里不调 reloadThemeSpecs，否则会被真实用户目录覆盖。
	tui.themeSpecs = []themesys.Spec{{Name: "previewed-theme", Colors: themesys.DefaultColors()}}
	tui.themeBeforePreview = themesys.ResolveName(tui.theme, tui.themeSpecs)
	tui.chat.ThemeOverlayOpen = true
	tui.setTheme("previewed-theme")
	if tui.theme == before {
		t.Fatal("precondition: preview must change the active theme")
	}

	// 强制关闭（模拟切 session 触发的 ResetNativeLists）。
	tui.chat.ResetNativeLists()
	if !tui.chat.PendingThemeRestore {
		t.Fatal("forced close during preview must request a theme restore")
	}
	tui.chat.PendingThemeRestore = false
	tui.closeThemeOverlay(true)

	if tui.theme != before {
		t.Fatalf("preview must be rolled back on forced close: got %q, want %q", tui.theme, before)
	}
}

// Enter 确认后不应再被恢复逻辑回滚。
func TestThemeApplyClearsPreviewBaseline(t *testing.T) {
	tui := newEdgeTUI(t)
	tui.themeSpecs = []themesys.Spec{{Name: "picked", Colors: themesys.DefaultColors()}}
	tui.setTheme(themesys.Default)
	tui.themeBeforePreview = themesys.Default
	tui.chat.ThemeOverlayOpen = true

	tui.chat.SetThemeItems([]chatpage.ThemeItem{{Name: "picked"}}, "")
	tui.applySelectedTheme()

	if tui.chat.ThemeOverlayOpen {
		t.Fatal("applying a theme must close the list")
	}
	if tui.theme != "picked" {
		t.Fatalf("applied theme = %q, want picked", tui.theme)
	}
}
