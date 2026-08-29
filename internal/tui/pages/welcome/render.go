package welcome

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type ViewDeps struct {
	Tr          func(string) string
	Brand       lipgloss.Style
	Dim         lipgloss.Style
	HL          lipgloss.Style
	Box         lipgloss.Style
	BorderColor color.Color
}

type ViewData struct {
	Width         int
	Height        int
	Pet           string
	Info          string
	Menu          string
	HasConfigured bool
	Help          string
}

// RenderView 渲染 welcome 门面：宠物动画居中作为视觉焦点，
// 品牌名使用渐变字（Brand→HL），状态信息压缩居中，底部是菜单框与帮助。
// 每个块先渲染，再按块宽相对视口宽度居中：
// 之前是“内容区内居中 + 统一左侧缩进”，右侧没有对称留白，
// 视口越宽内容整体越偏左（pet 与信息都会跑偏）。
// 超宽内容逐行截断，超窄终端也不溢出；高度足够时整体垂直居中。
func RenderView(data ViewData, deps ViewDeps) string {
	var sb strings.Builder

	sb.WriteString("\n")
	// 宠物动画作为整体块相对视口居中：pet 各行宽度一致（fillPetCell 补齐），
	// 块内左对齐 + 块整体居中，左边缘整齐。
	sb.WriteString(centerBlock(strings.TrimRight(data.Pet, "\n"), data.Width) + "\n")
	sb.WriteString("\n")
	// 品牌渐变字与副标题居中，形成门面焦点；两侧 ✦ 装饰用品牌色，
	// 与渐变起点同色系，增强品牌名存在感而不抢 pet 动画的焦点。
	sb.WriteString(centerLine(deps.Brand.Render("✦ ")+renderGradientBrand("Suna", deps)+deps.Brand.Render(" ✦"), data.Width) + "\n")
	sb.WriteString(centerLine(deps.Dim.Render(deps.Tr("tui.welcome.subtitle")), data.Width) + "\n")
	if !data.HasConfigured {
		sb.WriteString("\n" + centerLine(deps.HL.Render(deps.Tr("tui.welcome.setup_hint")), data.Width) + "\n")
	}
	// 分隔线：品牌区与状态区之间，渐变分隔保持克制。
	sb.WriteString("\n" + centerLine(renderGradientRule(min(24, data.Width), deps), data.Width) + "\n\n")
	// 状态信息作为整体块相对视口居中：块内行左对齐，行间整齐。
	sb.WriteString(centerBlock(strings.TrimRight(data.Info, "\n"), data.Width) + "\n")
	sb.WriteString("\n" + centerLine(renderGradientRule(min(24, data.Width), deps), data.Width) + "\n\n")
	// 菜单框：渲染后按实际框宽（含 border 与 padding）相对视口居中。
	box := welcomeBoxStyle(welcomeContentWidth(data.Width), deps).Render(strings.TrimRight(data.Menu, "\n"))
	sb.WriteString(centerBlock(box, data.Width) + "\n\n")
	sb.WriteString(centerLine(deps.Dim.Render(data.Help), data.Width) + "\n")
	out := sb.String()
	// 高度足够时整体垂直居中；内容高于视口时 PlaceVertical 是 no-op，安全。
	if data.Height > 0 {
		out = lipgloss.PlaceVertical(data.Height, lipgloss.Center, out)
	}
	return out
}

// renderGradientBrand 渲染品牌名渐变字：逐字符在 Brand 与 HL 之间渐变，
// 低色深终端由 lipgloss 自动降级为最接近的颜色，不破坏布局。
// 注意：必须按 rune 索引取色，range 字符串的 i 是字节偏移，中文会越界。
func renderGradientBrand(text string, deps ViewDeps) string {
	from := deps.Brand.GetForeground()
	to := deps.HL.GetForeground()
	if from == nil || to == nil {
		return deps.Brand.Render(text)
	}
	runes := []rune(text)
	colors := lipgloss.Blend1D(len(runes), from, to)
	var sb strings.Builder
	for i, r := range runes {
		sb.WriteString(lipgloss.NewStyle().Foreground(colors[i]).Bold(true).Render(string(r)))
	}
	return sb.String()
}

// renderGradientRule 渲染渐变分隔线：从 Dim 渐变到 Brand，替代单调的纯色横线。
func renderGradientRule(n int, deps ViewDeps) string {
	if n <= 0 {
		return ""
	}
	from := deps.Dim.GetForeground()
	to := deps.Brand.GetForeground()
	if from == nil || to == nil {
		return deps.Dim.Render(strings.Repeat("-", n))
	}
	colors := lipgloss.Blend1D(n, from, to)
	var sb strings.Builder
	for i := 0; i < n; i++ {
		sb.WriteString(lipgloss.NewStyle().Foreground(colors[i]).Render("-"))
	}
	return sb.String()
}

// centerLine 将单行内容在 width 内水平居中；超宽时截断。
// lipgloss.Width 按 ANSI 感知宽度计算，带样式的行不会溢出。
func centerLine(line string, width int) string {
	line = truncateANSI(line, width)
	gap := max(0, width-lipgloss.Width(line))
	left := gap / 2
	return strings.Repeat(" ", left) + line + strings.Repeat(" ", gap-left)
}

// centerBlock 将多行文本块在 width 内整体居中：块内行保持左对齐，
// 避免逐行居中造成行间参差；块宽取最长行，超宽时逐行截断。
func centerBlock(block string, width int) string {
	lines := strings.Split(block, "\n")
	blockWidth := 0
	for _, line := range lines {
		blockWidth = max(blockWidth, lipgloss.Width(line))
	}
	blockWidth = min(blockWidth, width)
	gap := max(0, width-blockWidth)
	left := gap / 2
	var sb strings.Builder
	for i, line := range lines {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(strings.Repeat(" ", left) + truncateANSI(line, blockWidth))
	}
	return sb.String()
}

func truncateANSI(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width == 1 {
		return ansi.Truncate(s, width, "")
	}
	return ansi.Truncate(s, width, "…")
}

func welcomeBoxStyle(width int, deps ViewDeps) lipgloss.Style {
	return deps.Box.Width(width).Padding(1, 2).Border(lipgloss.RoundedBorder()).BorderForeground(deps.BorderColor)
}
