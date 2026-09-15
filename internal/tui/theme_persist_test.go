package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// 主题浮层的 Enter 会关闭浮层并返回 config.set 持久化命令。
// 该命令必须沿 config 页的 overlay 转发路径传播到调用方：
// 若在“浮层已关闭”分支里被丢弃，用户会看到主题立即生效（内存态），
// 但配置从未写入，重启后回到旧主题——这正是本测试锁定的回归。
func TestThemeOverlayEnterPropagatesConfigSetCommand(t *testing.T) {
	tui := themeOverlayTUI(t, "default", "candidate")

	// 光标移到 candidate，然后按 Enter 应用。
	tui.chat.ThemeList.MoveCursor(1)
	_, cmd := tui.updateConfig(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on theme overlay must return the config.set command, got nil")
	}
	if tui.chat.ThemeOverlayOpen {
		t.Fatal("applying a theme must close the overlay")
	}
	if tui.theme != "candidate" {
		t.Fatalf("theme = %q, want candidate (applied in memory)", tui.theme)
	}
}

// Esc 关闭浮层只恢复预览前的主题，不应产生任何持久化副作用。
func TestThemeOverlayEscDoesNotPersist(t *testing.T) {
	tui := themeOverlayTUI(t, "default", "candidate")

	// 只走按键路径：预览由 updateThemeOverlay 触发，直接调 MoveCursor 不会预览。
	_, _ = tui.updateConfig(tea.KeyPressMsg{Code: tea.KeyDown})
	if tui.theme != "candidate" {
		t.Fatalf("moving the cursor must preview the theme, theme = %q", tui.theme)
	}

	_, cmd := tui.updateConfig(tea.KeyPressMsg{Code: tea.KeyEscape})
	if tui.theme != "default" {
		t.Fatalf("esc must restore the previous theme, theme = %q", tui.theme)
	}
	if cmd != nil {
		t.Fatal("esc must not schedule a config.set command")
	}
}
