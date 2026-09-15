package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
	themesys "github.com/alanchenchen/suna/internal/tui/theme"
)

// themeOverlayTUI 构造一个主题浮层已打开、条目已注入的 TUI。
// 直接注入条目而不走 openThemeOverlay，避免测试依赖用户真实主题目录。
func themeOverlayTUI(t *testing.T, saved string, names ...string) *TUI {
	t.Helper()
	tui := newEdgeTUI(t)
	tui.setTheme(saved)
	tui.themeBeforePreview = themesys.Normalize(saved)

	items := []chatpage.ThemeItem{{
		Name:    themesys.Default,
		Display: tui.tr("tui.theme.default"),
		Builtin: true,
		Active:  tui.themeBeforePreview == themesys.Default,
	}}
	for _, name := range names {
		items = append(items, chatpage.ThemeItem{
			Name:   name,
			Active: tui.themeBeforePreview == name,
		})
	}
	tui.chat.SetThemeItems(items, tui.themeBeforePreview)
	tui.chat.ThemeOverlayOpen = true
	// 渲染一次以完成列表尺寸设置；分页容量未设置时只会返回一项。
	tui.renderThemeOverlay(tui.width)
	return tui
}

// 勾选标记的是“已保存生效”的主题，而不是光标所在项：光标移动只是预览，
// 若把两者混为一谈，列表里每一项都会显示勾选，用户无法判断当前生效的是哪一个。
func TestThemeOverlayMarksOnlyActiveTheme(t *testing.T) {
	tui := themeOverlayTUI(t, "candidate", "candidate")
	defer tui.closeThemeOverlay(false)

	active := map[string]bool{}
	for _, item := range tui.chat.ThemeList.PageItems() {
		row, ok := item.Item.(chatpage.ThemeItem)
		if !ok {
			t.Fatalf("unexpected item type %T", item.Item)
		}
		active[row.Name] = row.Active
	}
	if len(active) != 2 {
		t.Fatalf("theme list items = %d, want 2 (default + candidate)", len(active))
	}
	if !active["candidate"] {
		t.Fatal("the saved theme must be marked active")
	}
	if active[themesys.Default] {
		t.Fatal("a theme that is not saved must not be marked active")
	}
}

// 内置 default 必须始终在列表里，否则用户预览自定义主题后无法切回。
func TestThemeOverlayCanSwitchBackToDefault(t *testing.T) {
	tui := themeOverlayTUI(t, "candidate", "candidate")
	defer tui.closeThemeOverlay(false)

	if name, _ := tui.chat.SelectedTheme(); name != "candidate" {
		t.Fatalf("cursor should start on the saved theme, got %q", name)
	}
	tui.chat.ThemeList.MoveCursor(-1)
	tui.previewSelectedTheme()
	if name, _ := tui.chat.SelectedTheme(); name != themesys.Default {
		t.Fatalf("cursor should reach builtin default, got %q", name)
	}
	tui.applySelectedTheme()

	if tui.theme != themesys.Default {
		t.Fatalf("theme = %q, want %q (builtin must be switchable back)", tui.theme, themesys.Default)
	}
}

// footer 用通用格式（移动 · 应用 · 关闭），不能再把整条帮助文案当成动作名重复拼接。
func TestThemeOverlayFooterHasNoDuplicatedHelp(t *testing.T) {
	tui := themeOverlayTUI(t, themesys.Default, "candidate")
	defer tui.closeThemeOverlay(false)

	plain := ansi.Strip(tui.renderThemeOverlay(tui.width))
	if strings.Contains(plain, "预览") || strings.Contains(plain, "Preview") {
		t.Fatalf("footer must not stack a second key legend:\n%s", plain)
	}
	if !strings.Contains(plain, "Enter 应用") {
		t.Fatalf("footer should offer the apply action:\n%s", plain)
	}
}
