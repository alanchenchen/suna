package tui

import (
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
	"image/color"
	"os"
	"strings"
	"sync"

	"charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"
)

const (
	ThemeAuto  = "auto"
	ThemeDark  = "dark"
	ThemeLight = "light"
)

type themePalette struct {
	Name          string
	MarkdownStyle string
	Brand         color.Color
	Dim           color.Color
	User          color.Color
	Agent         color.Color
	Tool          color.Color
	Error         color.Color
	HL            color.Color
	Text          color.Color
	MutedText     color.Color
	SubtleText    color.Color
	CodeBg        color.Color
	ToolText      color.Color
}

var currentTheme = darkPalette()

func clearMarkdownCache() {
	mdCache = sync.Map{}
}

func darkPalette() themePalette {
	return themePalette{
		Name:          ThemeDark,
		MarkdownStyle: "dark",
		Brand:         lipgloss.Color("14"),
		Dim:           lipgloss.Color("8"),
		User:          lipgloss.Color("12"),
		Agent:         lipgloss.Color("10"),
		Tool:          lipgloss.Color("11"),
		Error:         lipgloss.Color("9"),
		HL:            lipgloss.Color("15"),
		Text:          lipgloss.Color("15"),
		MutedText:     lipgloss.Color("243"),
		SubtleText:    lipgloss.Color("244"),
		CodeBg:        lipgloss.Color("236"),
		ToolText:      lipgloss.Color("0"),
	}
}

func lightPalette() themePalette {
	return themePalette{
		Name:          ThemeLight,
		MarkdownStyle: "light",
		Brand:         lipgloss.Color("25"),
		Dim:           lipgloss.Color("240"),
		User:          lipgloss.Color("19"),
		Agent:         lipgloss.Color("28"),
		Tool:          lipgloss.Color("94"),
		Error:         lipgloss.Color("124"),
		HL:            lipgloss.Color("16"),
		Text:          lipgloss.Color("16"),
		MutedText:     lipgloss.Color("238"),
		SubtleText:    lipgloss.Color("244"),
		CodeBg:        lipgloss.Color("254"),
		ToolText:      lipgloss.Color("230"),
	}
}

func normalizeThemeName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case ThemeAuto, "":
		return ThemeAuto
	case ThemeLight:
		return ThemeLight
	default:
		return ThemeDark
	}
}

func resolveTheme(name string) themePalette {
	name = normalizeThemeName(name)
	switch name {
	case ThemeDark:
		return darkPalette()
	case ThemeAuto:
		if !terminalLooksDark() {
			return lightPalette()
		}
		return darkPalette()
	default:
		return lightPalette()
	}
}

func terminalLooksDark() bool {
	// 终端通常不暴露可靠的背景色 API；auto 只在明确识别为 dark 时使用深色。
	for _, key := range []string{"COLORFGBG", "TERMINAL_THEME", "THEME", "APPEARANCE"} {
		value := strings.ToLower(os.Getenv(key))
		if value == "" {
			continue
		}
		if strings.Contains(value, "dark") {
			return true
		}
	}
	return false
}

func applyTheme(name string) {
	p := resolveTheme(name)
	currentTheme = p
	ColorBrand, ColorDim, ColorUser = p.Brand, p.Dim, p.User
	ColorAgent, ColorTool, ColorError, ColorHL = p.Agent, p.Tool, p.Error, p.HL
	styleUser = lipgloss.NewStyle().Bold(true).Foreground(ColorUser)
	styleAgent = lipgloss.NewStyle().Bold(true).Foreground(ColorAgent)
	styleTool = lipgloss.NewStyle().Bold(true).Foreground(ColorTool)
	styleError = lipgloss.NewStyle().Bold(true).Foreground(ColorError)
	styleSystem = lipgloss.NewStyle().Bold(true).Foreground(ColorDim)
	styleDim = lipgloss.NewStyle().Foreground(ColorDim)
	styleHL = lipgloss.NewStyle().Bold(true).Foreground(ColorHL)
	styleCursor = lipgloss.NewStyle().Bold(true).Foreground(ColorBrand)
	styleLogo = lipgloss.NewStyle().Foreground(ColorBrand).Bold(true)
	styleLogoDim = lipgloss.NewStyle().Foreground(ColorDim)
	styleBrand = lipgloss.NewStyle().Foreground(ColorBrand).Bold(true)
	boxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(ColorDim)
	styleUserLine = lipgloss.NewStyle().Foreground(ColorUser).Bold(true)
	styleAgentLine = lipgloss.NewStyle().Foreground(ColorAgent).Bold(true)
	styleToolPill = lipgloss.NewStyle().Foreground(p.ToolText).Background(ColorTool).Padding(0, 1).Bold(true)
	styleToolOk = lipgloss.NewStyle().Foreground(ColorAgent).Bold(true)
	styleToolErr = lipgloss.NewStyle().Foreground(ColorError).Bold(true)
	styleToolRun = lipgloss.NewStyle().Foreground(ColorBrand).Bold(true)
	styleToolDim = lipgloss.NewStyle().Foreground(ColorDim)
	styleToolIntent = lipgloss.NewStyle().Foreground(currentTheme.MutedText)
	styleToolAdd = lipgloss.NewStyle().Foreground(ColorAgent).Bold(true)
	styleToolDel = lipgloss.NewStyle().Foreground(ColorError).Bold(true)
	styleMetaPill = lipgloss.NewStyle().Foreground(p.ToolText).Background(ColorBrand).Padding(0, 1).Bold(true)
	styleGuardOK = lipgloss.NewStyle().Foreground(p.ToolText).Background(ColorAgent).Padding(0, 1).Bold(true)
	styleGuardWarn = lipgloss.NewStyle().Foreground(p.ToolText).Background(ColorTool).Padding(0, 1).Bold(true)
	styleGuardErr = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(ColorError).Padding(0, 1).Bold(true)
	styleFilePath = lipgloss.NewStyle().Foreground(ColorHL).Bold(true)
	styleSysLine = lipgloss.NewStyle().Foreground(ColorDim)
	styleErrLine = lipgloss.NewStyle().Foreground(ColorError).Bold(true)
	bodyFill = lipgloss.NewStyle().Background(ColorBrand).Foreground(p.ToolText)
	clearMarkdownCache()
}

func (t *TUI) setTheme(name string) {
	t.theme = normalizeThemeName(name)
	applyTheme(t.theme)
	if t.mode == uipage.Chat {
		t.applyTextAreaTheme()
	}
	t.chat.Spinner.Style = lipgloss.NewStyle().Foreground(ColorBrand)
}

func nextTheme(name string) string {
	switch normalizeThemeName(name) {
	case ThemeAuto:
		return ThemeDark
	case ThemeDark:
		return ThemeLight
	default:
		return ThemeAuto
	}
}

func (t *TUI) themeDisplay() string {
	switch normalizeThemeName(t.theme) {
	case ThemeAuto:
		return t.tr("tui.theme.auto")
	case ThemeLight:
		return t.tr("tui.theme.light")
	default:
		return t.tr("tui.theme.dark")
	}
}

func (t *TUI) applyTextAreaTheme() {
	styles := textareaStyles()
	t.chat.Textarea.SetStyles(styles)
}

func textareaStyles() textarea.Styles {
	styles := textarea.DefaultStyles(false)
	styles.Focused.Text = lipgloss.NewStyle().Foreground(currentTheme.Text)
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(currentTheme.SubtleText)
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(ColorUser).Bold(true)
	styles.Focused.CursorLine = lipgloss.NewStyle()
	styles.Blurred.Text = lipgloss.NewStyle().Foreground(currentTheme.MutedText)
	styles.Blurred.Placeholder = lipgloss.NewStyle().Foreground(currentTheme.SubtleText)
	styles.Blurred.Prompt = lipgloss.NewStyle().Foreground(ColorBrand)
	styles.Focused.EndOfBuffer = lipgloss.NewStyle().Foreground(currentTheme.SubtleText)
	styles.Blurred.EndOfBuffer = lipgloss.NewStyle().Foreground(currentTheme.SubtleText)
	return styles
}
