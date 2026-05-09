package tui

import "charm.land/lipgloss/v2"

var (
	ColorBrand = lipgloss.Color("14")
	ColorDim   = lipgloss.Color("8")
	ColorUser  = lipgloss.Color("12")
	ColorAgent = lipgloss.Color("10")
	ColorTool  = lipgloss.Color("11")
	ColorError = lipgloss.Color("9")
	ColorHL    = lipgloss.Color("15")

	styleUser    = lipgloss.NewStyle().Bold(true).Foreground(ColorUser)
	styleAgent   = lipgloss.NewStyle().Bold(true).Foreground(ColorAgent)
	styleTool    = lipgloss.NewStyle().Bold(true).Foreground(ColorTool)
	styleError   = lipgloss.NewStyle().Bold(true).Foreground(ColorError)
	styleSystem  = lipgloss.NewStyle().Bold(true).Foreground(ColorDim)
	styleDim     = lipgloss.NewStyle().Foreground(ColorDim)
	styleHL      = lipgloss.NewStyle().Bold(true).Foreground(ColorHL)
	styleCursor  = lipgloss.NewStyle().Bold(true).Foreground(ColorBrand)
	styleLogo    = lipgloss.NewStyle().Foreground(ColorBrand).Bold(true)
	styleLogoDim = lipgloss.NewStyle().Foreground(ColorDim)
	styleBrand   = lipgloss.NewStyle().Foreground(ColorBrand).Bold(true)

	boxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(ColorDim)
)
