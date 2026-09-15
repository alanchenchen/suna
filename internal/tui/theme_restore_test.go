package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
	themesys "github.com/alanchenchen/suna/internal/tui/theme"
)

// 离开列表恢复的是"进入列表前已保存的主题"，而不是兜底的 default。
// 只有该主题本身不可用（被删除/写坏）时才落到 default。
func TestLeavingListRestoresSavedThemeNotDefault(t *testing.T) {
	tui := newEdgeTUI(t)
	tui.themeSpecs = []themesys.Spec{
		{Name: "saved-theme", Colors: themesys.DefaultColors()},
		{Name: "previewed-theme", Colors: themesys.DefaultColors()},
	}
	tui.setTheme("saved-theme") // 已保存的主题
	if tui.theme != "saved-theme" {
		t.Fatalf("precondition: theme = %q", tui.theme)
	}

	// 打开列表 → 预览另一个主题 → 离开（Esc 语义）。
	tui.themeBeforePreview = themesys.ResolveName(tui.theme, tui.themeSpecs)
	tui.chat.ThemeOverlayOpen = true
	tui.setTheme("previewed-theme")
	if tui.theme != "previewed-theme" {
		t.Fatalf("precondition: preview must apply, theme = %q", tui.theme)
	}

	tui.closeThemeOverlay(true)
	if tui.theme != "saved-theme" {
		t.Fatalf("leaving must restore the saved theme: got %q, want saved-theme", tui.theme)
	}
}

// 已保存主题被删除时，恢复路径落到 default（可用集合判定的正确行为）。
func TestLeavingListFallsBackWhenSavedThemeGone(t *testing.T) {
	tui := newEdgeTUI(t)
	tui.themeSpecs = []themesys.Spec{{Name: "other", Colors: themesys.DefaultColors()}}
	tui.theme = "saved-theme" // 文件已被删除，不在集合里

	tui.themeBeforePreview = themesys.ResolveName(tui.theme, tui.themeSpecs)
	if tui.themeBeforePreview != themesys.Default {
		t.Fatalf("baseline = %q, want %q", tui.themeBeforePreview, themesys.Default)
	}
	tui.chat.ThemeOverlayOpen = true
	tui.setTheme("other")
	tui.closeThemeOverlay(true)
	if tui.theme != themesys.Default {
		t.Fatalf("theme = %q, want %q", tui.theme, themesys.Default)
	}
}

// 走完整按键路径验证：Enter 之后离开列表不会把主题回滚。
func TestEnterThenLeaveKeepsAppliedTheme(t *testing.T) {
	tui := themeOverlayTUI(t, themesys.Default, "candidate")

	tui.chat.ThemeList.MoveCursor(1)
	_, cmd := tui.updateConfig(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter must persist the theme")
	}
	if tui.theme != "candidate" {
		t.Fatalf("theme = %q, want candidate", tui.theme)
	}
	// 再离开配置页（模拟切页），不应回滚。
	if tui.chat.ThemeOverlayOpen {
		t.Fatal("enter must close the overlay")
	}
	_ = tui.leaveConfig()
	if tui.theme != "candidate" {
		t.Fatalf("leaving after enter must keep the applied theme, got %q", tui.theme)
	}
}

// 列表里光标停在不可用主题上时不预览，当前主题保持不变。
func TestPreviewSkipsUnavailableTheme(t *testing.T) {
	tui := newEdgeTUI(t)
	tui.themeSpecs = []themesys.Spec{{Name: "ok", Colors: themesys.DefaultColors()}}
	tui.setTheme(themesys.Default)

	items := []chatpage.ThemeItem{
		{Name: themesys.Default, Display: tui.tr("tui.theme.default"), Builtin: true},
		{Name: "broken", Err: "bad color"},
	}
	tui.chat.SetThemeItems(items, themesys.Default)
	tui.chat.ThemeOverlayOpen = true

	tui.chat.ThemeList.MoveCursor(1) // 移到不可用主题
	tui.previewSelectedTheme()
	if tui.theme != themesys.Default {
		t.Fatalf("unavailable theme must not preview, theme = %q", tui.theme)
	}
}
