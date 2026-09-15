package tui

import (
	"image/color"
	"sync"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
	themesys "github.com/alanchenchen/suna/internal/tui/theme"
)

// 主题在 TUI 侧的职责只有两件：把调色板应用到样式，以及承载主题选择状态。
// 解析、校验与颜色适配都在 internal/tui/theme 子包里（纯逻辑，无 TUI 依赖）。

// ThemeDefault 是唯一的内置主题名（与子包保持一致，供 TUI 内部引用）。
const ThemeDefault = themesys.Default

// 所有颜色与样式集中在这里声明：它们是"当前主题"的唯一出口，
// 由 applyThemePalette 按调色板重建，避免样式定义散落在各渲染文件里。
var (
	ColorAccent  color.Color
	ColorDim     color.Color
	ColorInfo    color.Color
	ColorSuccess color.Color
	ColorWarning color.Color
	ColorError   color.Color
	ColorText    color.Color

	styleUser   lipgloss.Style
	styleAgent  lipgloss.Style
	styleTool   lipgloss.Style
	styleError  lipgloss.Style
	styleDim    lipgloss.Style
	styleMuted  lipgloss.Style
	styleHL     lipgloss.Style
	styleCursor lipgloss.Style
	styleBrand  lipgloss.Style
	// styleSelection 是内容区鼠标选区：中性表面底色 + 正文前景。
	// 选区是高频大面积交互，用高饱和品牌色会刺眼并压过内容；
	// 与 VS Code/iTerm 的选区行为一致。主题切换时在 applyThemePalette 重建。
	styleSelection lipgloss.Style
	boxStyle       lipgloss.Style

	styleUserLine      lipgloss.Style
	styleAgentLine     lipgloss.Style
	styleToolOk        lipgloss.Style
	styleToolErr       lipgloss.Style
	styleToolRun       lipgloss.Style
	styleToolDim       lipgloss.Style
	styleToolMuted     lipgloss.Style
	styleToolIntent    lipgloss.Style
	styleMetaPill      lipgloss.Style
	styleThinkingIcon  lipgloss.Style
	styleThinkingLabel lipgloss.Style
	styleThinkingValue lipgloss.Style
	styleGuardOK       lipgloss.Style
	styleGuardWarn     lipgloss.Style
	styleGuardErr      lipgloss.Style
	styleFilePath      lipgloss.Style
	styleSysLine       lipgloss.Style
	styleErrLine       lipgloss.Style

	// bodyFill 是 pet 大号身体色块：主题色背景 + 对比前景。
	bodyFill lipgloss.Style
)

var currentTheme = themesys.Adapt(ThemeDefault, themesys.DefaultColors(), themesys.DarkBackground)

// init 用内置 default 初始化全部样式，保证任何渲染路径（含测试）都不会遇到零值样式。
// 真实主题在 TUI 启动时由 applyResolvedTheme 覆盖。
func init() {
	applyThemePalette(currentTheme)
}

func clearMarkdownCache() {
	mdCache = sync.Map{}
}

// applyThemePalette 把适配后的调色板铺到全部样式变量上。
func applyThemePalette(p themesys.Palette) {
	currentTheme = p
	ColorAccent, ColorDim, ColorInfo = p.Accent, p.Dim, p.Info
	ColorSuccess, ColorWarning, ColorError, ColorText = p.Success, p.Warning, p.Error, p.Text
	styleUser = lipgloss.NewStyle().Bold(true).Foreground(ColorInfo)
	styleAgent = lipgloss.NewStyle().Bold(true).Foreground(ColorSuccess)
	styleTool = lipgloss.NewStyle().Bold(true).Foreground(ColorWarning)
	styleError = lipgloss.NewStyle().Bold(true).Foreground(ColorError)
	styleDim = lipgloss.NewStyle().Foreground(ColorDim)
	// styleMuted 承载"次要正文"：帮助、描述、标签、元信息。
	// 它与 styleDim（纯装饰）区分开，避免可读文字落到装饰色的低对比度上。
	styleMuted = lipgloss.NewStyle().Foreground(currentTheme.Muted)
	styleHL = lipgloss.NewStyle().Bold(true).Foreground(ColorText)
	styleCursor = lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)
	styleBrand = lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	boxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(ColorDim)
	styleUserLine = lipgloss.NewStyle().Foreground(ColorInfo).Bold(true)
	styleAgentLine = lipgloss.NewStyle().Foreground(ColorSuccess).Bold(true)
	styleToolOk = lipgloss.NewStyle().Foreground(ColorSuccess).Bold(true)
	styleToolErr = lipgloss.NewStyle().Foreground(ColorError).Bold(true)
	styleToolRun = lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	styleToolDim = lipgloss.NewStyle().Foreground(ColorDim)
	// styleToolMuted 是工具/子任务面板里"用户要读的正文"（任务、上下文、结果、详情）。
	styleToolMuted = lipgloss.NewStyle().Foreground(currentTheme.Muted)
	styleToolIntent = lipgloss.NewStyle().Foreground(currentTheme.Muted)
	styleMetaPill = pill(ColorAccent, p.OnAccent)
	styleThinkingIcon = lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	styleThinkingLabel = lipgloss.NewStyle().Foreground(currentTheme.Muted)
	// styleSelection 是内容区鼠标选区：中性表面底色 + 正文前景。
	// 选区是高频大面积交互，用高饱和品牌色会显得刺眼，也压过内容本身。
	styleSelection = lipgloss.NewStyle().Background(currentTheme.Surface).Foreground(currentTheme.Text)
	styleThinkingValue = lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	styleGuardOK = pill(ColorSuccess, p.OnSuccess)
	styleGuardWarn = pill(ColorWarning, p.OnWarning)
	styleGuardErr = pill(ColorError, p.OnError)
	styleFilePath = lipgloss.NewStyle().Foreground(ColorText).Bold(true)
	styleSysLine = lipgloss.NewStyle().Foreground(currentTheme.Muted)
	styleErrLine = lipgloss.NewStyle().Foreground(ColorError).Bold(true)
	bodyFill = lipgloss.NewStyle().Background(ColorAccent).Foreground(p.OnAccent)
	clearMarkdownCache()
}

// pill 构造"色块 + 文字"样式：前景必须与背景同源（用对应的 On* 色），
// 否则浅色主题下文字可能落在对比不足的组合上。所有色块都应经由它构造。
func pill(bg, on color.Color) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(on).Background(bg).Padding(0, 1).Bold(true)
}

// onFor 返回与给定语义背景色配对的文字色，避免调用方手写 On* 造成错配。
func onFor(bg color.Color) color.Color {
	switch bg {
	case ColorAccent:
		return currentTheme.OnAccent
	case ColorSuccess:
		return currentTheme.OnSuccess
	case ColorWarning:
		return currentTheme.OnWarning
	case ColorError:
		return currentTheme.OnError
	default:
		// 中性背景（如 Dim/Surface）用正文色，保证可读。
		return currentTheme.Text
	}
}

// setTheme 设置主题名并应用（用户主题列表由 TUI 持有）。
func (t *TUI) setTheme(name string) {
	t.theme = themesys.Normalize(name)
	t.applyResolvedTheme()
}

// applyResolvedTheme 按当前主题与终端背景应用调色板。
// 样式变量是全局的，因此任何页面都可能受影响；这里只刷新与当前页面相关的组件状态，
// 配置页/欢迎页的颜色会在下一次 View 时自然使用新样式。
//
// Chat 内的重建走帧门异步执行：主题切换会让所有 markdown 块缓存失效，
// 长会话下全量重建可达上百毫秒，同步执行会阻塞切回 Chat 的那一帧。
// 测试环境没有 program，退回同步重建以保持可断言性。
func (t *TUI) applyResolvedTheme() {
	applyThemePalette(themesys.Resolve(t.theme, t.themeSpecs, t.terminalBackground))
	t.applyConfigInputTheme()
	if t.mode == uipage.Chat {
		t.applyTextAreaTheme()
		t.refreshNativeLists()
		if t.program != nil {
			// 直接投递帧门消息：scheduleTranscriptSync 返回的 tea.Tick 需要由
			// Update 返回才能启动，而 setTheme 的调用点分布很广（配置页、通知、
			// 浮层），逐个传播 tea.Cmd 会污染大量签名。program.Send 走同一事件循环，
			// 效果等价且只改这一处。
			t.transcriptSyncDirty = true
			t.program.Send(transcriptSyncMsg{})
		} else {
			// 测试环境没有 program，同步重建以保持可断言性。
			t.syncContent()
		}
	}
	t.chat.Spinner.Style = lipgloss.NewStyle().Foreground(ColorAccent)
}

// applyDetectedBackground 记录终端背景并重新应用主题（所有主题都依赖它做适配）。
// 保存真实背景色而非仅深浅：对比度必须相对用户实际看到的底色计算。
func (t *TUI) applyDetectedBackground(background tea.BackgroundColorMsg) {
	r, g, b, _ := background.Color.RGBA()
	t.terminalBackground = themesys.Background{
		Dark: background.IsDark(),
		RGB:  color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 0xff},
	}
	t.applyResolvedTheme()
}

// reloadThemeSpecs 重新扫描用户主题目录（打开主题列表时调用，无需重启）。
func (t *TUI) reloadThemeSpecs() {
	t.themeSpecs = themesys.LoadUser(themesDir())
}

// themeDisplay 返回当前主题的展示名。
func (t *TUI) themeDisplay() string {
	name := themesys.Normalize(t.theme)
	if name == ThemeDefault {
		return t.tr("tui.theme.default")
	}
	return name
}

func (t *TUI) applyTextAreaTheme() {
	styles := textareaStyles()
	t.chat.Textarea.SetStyles(styles)
}

func textareaStyles() textarea.Styles {
	styles := textarea.DefaultStyles(currentTheme.Dark)
	styles.Focused.Text = lipgloss.NewStyle().Foreground(currentTheme.Text)
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(currentTheme.Muted)
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(ColorInfo).Bold(true)
	styles.Focused.CursorLine = lipgloss.NewStyle()
	styles.Blurred.Text = lipgloss.NewStyle().Foreground(currentTheme.Muted)
	styles.Blurred.Placeholder = lipgloss.NewStyle().Foreground(currentTheme.Muted)
	styles.Blurred.Prompt = lipgloss.NewStyle().Foreground(ColorAccent)
	styles.Focused.EndOfBuffer = lipgloss.NewStyle().Foreground(currentTheme.Muted)
	styles.Blurred.EndOfBuffer = lipgloss.NewStyle().Foreground(currentTheme.Muted)
	styles.Cursor.Color = ColorAccent
	return styles
}

func textInputStyles() textinput.Styles {
	styles := textinput.DefaultStyles(currentTheme.Dark)
	styles.Focused.Text = lipgloss.NewStyle().Foreground(currentTheme.Text)
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(currentTheme.Muted)
	styles.Focused.Suggestion = lipgloss.NewStyle().Foreground(currentTheme.Muted)
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	styles.Blurred.Text = lipgloss.NewStyle().Foreground(currentTheme.Muted)
	styles.Blurred.Placeholder = lipgloss.NewStyle().Foreground(currentTheme.Muted)
	styles.Blurred.Suggestion = lipgloss.NewStyle().Foreground(currentTheme.Muted)
	styles.Blurred.Prompt = lipgloss.NewStyle().Foreground(ColorDim)
	styles.Cursor.Color = ColorAccent
	return styles
}

func (t *TUI) applyConfigInputTheme() {
	for i := range t.config.Inputs {
		t.config.Inputs[i].SetStyles(textInputStyles())
	}
}
