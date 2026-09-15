package tui

import (
	"fmt"
	"image/color"

	tea "charm.land/bubbletea/v2"

	"github.com/alanchenchen/suna/internal/protocol"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
	themesys "github.com/alanchenchen/suna/internal/tui/theme"
)

// 主题选择浮层。
//
// 交互与 MCP/Skills 列表一致（↑↓ 选择 · Enter 应用 · Esc 关闭），但多一层语义：
// 光标移动即实时预览（不落库），Enter 才持久化，Esc 恢复进入列表前的主题。
// 这样用户可以快速横向比较多个主题，且预览不会污染配置。
//
// 重载不需要专门键位：每次打开列表都重新扫描 ~/.suna/themes/，
// 改完主题文件后 Esc 关闭再打开即可看到。

// openThemeOverlay 打开主题列表，并记住当前主题用于 Esc 恢复。
func (t *TUI) openThemeOverlay() {
	t.reloadThemeSpecs()
	t.themeBeforePreview = themesys.Normalize(t.theme)

	// 内置 default 始终排在首位，用户因此能随时切回它；
	// 它由 Suna 自己设计（不来自用户目录），色块用其调色板渲染。
	// Active 只标记已保存生效的主题，与光标位置无关（光标移动仅是预览）。
	items := make([]chatpage.ThemeItem, 0, len(t.themeSpecs)+1)
	items = append(items, chatpage.ThemeItem{
		Name:    themesys.Default,
		Display: t.tr("tui.theme.default"),
		Swatch:  t.themeSwatch(themesys.Spec{Name: themesys.Default, Colors: themesys.DefaultColors()}),
		Builtin: true,
		Active:  t.themeBeforePreview == themesys.Default,
	})
	for _, spec := range t.themeSpecs {
		items = append(items, chatpage.ThemeItem{
			Name:   spec.Name,
			Err:    spec.Err,
			Swatch: t.themeSwatch(spec),
			Active: t.themeBeforePreview == spec.Name,
		})
	}
	t.chat.SetThemeItems(items, t.themeBeforePreview)
	t.chat.ThemeOverlayOpen = true
}

// themeSwatch 用主题自身的语义色渲染一段色块，便于在列表里一眼看出配色。
// 不可用的主题返回空串（列表里用错误标记代替）。
func (t *TUI) themeSwatch(spec themesys.Spec) string {
	if spec.Err != "" {
		return ""
	}
	palette := themesys.Adapt(spec.Name, spec.Colors, t.terminalBackground)
	block := func(c color.Color) string {
		r, g, b, _ := c.RGBA()
		return fmt.Sprintf("\x1b[48;2;%d;%d;%dm \x1b[0m", r>>8, g>>8, b>>8)
	}
	return block(palette.Accent) + block(palette.Success) +
		block(palette.Info) + block(palette.Warning) + block(palette.Error)
}

// updateThemeOverlay 处理主题浮层的按键。
func (t *TUI) updateThemeOverlay(ks string, msg tea.Msg) (tea.Model, tea.Cmd) {
	if t.chat.ThemeList.Filtering() {
		switch ks {
		case "up":
			t.chat.ThemeList.MoveCursor(-1)
			t.previewSelectedTheme()
			return t, nil
		case "down":
			t.chat.ThemeList.MoveCursor(1)
			t.previewSelectedTheme()
			return t, nil
		}
	}
	if ks == "esc" && !t.chat.ThemeList.Filtering() {
		t.closeThemeOverlay(true)
		return t, nil
	}
	if ks == "enter" {
		return t, t.applySelectedTheme()
	}
	if ks == "up" || ks == "down" {
		t.chat.ThemeList.MoveCursor(map[bool]int{true: -1, false: 1}[ks == "up"])
		t.previewSelectedTheme()
		return t, nil
	}
	return t, t.chat.UpdateThemeList(msg)
}

// previewSelectedTheme 把光标所在主题立即应用（不落库）。
// 不可用的主题（解析失败）不预览，保持当前主题不变。
func (t *TUI) previewSelectedTheme() {
	name, err := t.chat.SelectedTheme()
	if err != "" {
		return
	}
	t.setTheme(name)
}

// applySelectedTheme 持久化当前选中的主题。
func (t *TUI) applySelectedTheme() tea.Cmd {
	name, errText := t.chat.SelectedTheme()
	if errText != "" {
		return nil
	}
	t.setTheme(name)
	t.chat.ThemeOverlayOpen = false
	return t.sendConfigSet(protocol.ConfigSetParams{
		Action: protocol.ConfigActionUpdateGeneral,
		Locale: string(t.i18n.Locale()),
		Theme:  name,
	})
}

// closeThemeOverlay 关闭列表；restore 为真时恢复进入列表前的主题（Esc 语义）。
func (t *TUI) closeThemeOverlay(restore bool) {
	t.chat.ThemeOverlayOpen = false
	if restore && t.themeBeforePreview != "" {
		t.setTheme(t.themeBeforePreview)
	}
	t.themeBeforePreview = ""
}

// renderThemeOverlay 渲染主题列表：每项显示主题名 + 用该主题主色渲染的色块预览。
// footer 用通用格式（↑↓ 移动 · Enter 应用 · Esc 关闭），与其他列表保持一致；
// 光标移动即预览的行为写进配置页入口说明，不在这里堆叠第二套键位文案。
func (t *TUI) renderThemeOverlay(width int) string {
	return t.renderNativeListOverlay(chatpage.NativeListTheme, &t.chat.ThemeList, width,
		t.nativeListText().Apply, "tui.theme.empty", "", "")
}
