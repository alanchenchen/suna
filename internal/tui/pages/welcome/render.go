package welcome

import (
	"image/color"
	"strings"

	textutil "github.com/alanchenchen/suna/internal/tui/components/text"

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
	Pet           string
	Info          string
	Menu          string
	HasConfigured bool
}

func RenderView(data ViewData, deps ViewDeps) string {
	var sb strings.Builder
	w := welcomeContentWidth(data.Width)
	// 带边框的菜单会在 Style.Width 之外占用 padding 与 border 宽度。
	leftPad := max(0, (data.Width-min(data.Width, w+6))/2)
	pad := strings.Repeat(" ", leftPad)

	sb.WriteString("\n")
	renderTop(&sb, data.Pet, data.Info, pad, w)
	sb.WriteString("\n")
	sb.WriteString(pad + truncateANSI(deps.Brand.Render("Suna"), w) + "\n")
	sb.WriteString(pad + truncateANSI(deps.Dim.Render(deps.Tr("tui.welcome.subtitle")), w) + "\n")
	if !data.HasConfigured {
		sb.WriteString("\n" + pad + truncateANSI(deps.HL.Render(deps.Tr("tui.welcome.setup_hint")), w) + "\n")
	}
	sb.WriteString("\n")
	sb.WriteString(textutil.IndentLines(welcomeBoxStyle(w, deps).Render(strings.TrimRight(data.Menu, "\n")), pad) + "\n\n")
	sb.WriteString(pad + truncateANSI(deps.Dim.Render(deps.Tr("tui.welcome.help")), w) + "\n")
	return sb.String()
}

// renderTop 在状态信息与宠物图可并排时保持双栏；空间不足时改为纵向，
// 避免长状态值将终端横向撑开。
func renderTop(sb *strings.Builder, petText, infoText, pad string, width int) {
	pet := strings.Split(petText, "\n")
	info := strings.Split(infoText, "\n")
	petWidth := 0
	for _, line := range pet {
		petWidth = max(petWidth, lipgloss.Width(line))
	}

	const gap = 4
	if petWidth > 0 && width >= petWidth+gap+20 {
		infoWidth := width - petWidth - gap
		rows := max(len(pet), len(info))
		for i := 0; i < rows; i++ {
			left, right := "", ""
			if i < len(pet) {
				left = truncateANSI(pet[i], petWidth)
			}
			if i < len(info) {
				right = truncateANSI(info[i], infoWidth)
			}
			sb.WriteString(pad + left + strings.Repeat(" ", max(0, petWidth-lipgloss.Width(left))) + strings.Repeat(" ", gap) + right + "\n")
		}
		return
	}

	for _, line := range pet {
		sb.WriteString(pad + truncateANSI(line, width) + "\n")
	}
	infoWidth := width
	if petWidth > 0 && width >= petWidth+gap+20 {
		infoWidth = width - petWidth - gap
	}
	for _, line := range info {
		sb.WriteString(pad + truncateANSI(line, infoWidth) + "\n")
	}
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
