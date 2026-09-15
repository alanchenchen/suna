package tui

import (
	"testing"

	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
	themesys "github.com/alanchenchen/suna/internal/tui/theme"
)

// welcome 菜单在欢迎页渲染，而主题通常在配置页切换。
// 样式作为 View 的每帧输入传入，因此无需任何外部推送即可跟随主题。
func TestWelcomeMenuFollowsThemeSwitch(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 120, height: 40, mode: uipage.Welcome}
	tui.reloadThemeSpecs()
	tui.setTheme(themesys.Default)
	tui.initWelcomeList()

	before := tui.menu.View(tui.welcomeStyles())
	if before == "" {
		t.Fatal("precondition: welcome menu must render")
	}

	// 在配置页切换主题（真实入口），再回到欢迎页。
	tui.mode = uipage.Config
	tui.themeSpecs = []themesys.Spec{{Name: "probe", Colors: themesys.Colors{
		Accent: "#ff0000", Success: "#00ff00", Info: "#0000ff",
		Warning: "#ffff00", Error: "#ff00ff",
	}}}
	tui.setTheme("probe")
	tui.mode = uipage.Welcome

	if after := tui.menu.View(tui.welcomeStyles()); after == before {
		t.Fatalf("welcome menu must follow theme switch: got unchanged output %q", after)
	}
}

// 样式改为渲染时拉取后，切换主题不需要重建 list，滚动与选中状态必须保留。
func TestWelcomeThemeSwitchKeepsMenuState(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 120, height: 40, mode: uipage.Welcome}
	tui.reloadThemeSpecs()
	tui.setTheme(themesys.Default)
	tui.initWelcomeList()

	items := tui.welcomeMenuItems()
	if _, handled := tui.menu.UpdateKey("down", items); !handled {
		t.Fatal("precondition: down key must be handled")
	}
	selectedBefore := tui.menu.SelectedItem()

	tui.setTheme(themesys.Default)

	if got := tui.menu.SelectedItem(); got.Action != selectedBefore.Action {
		t.Fatalf("theme switch must not reset selection: got %v, want %v", got.Action, selectedBefore.Action)
	}
}
