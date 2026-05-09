package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

type petState int

const (
	petIdle petState = iota
	petWorking
	petThinking
)

var bodyFill = lipgloss.NewStyle().Background(lipgloss.Color("14")).Foreground(lipgloss.Color("0"))

func renderPet(state petState, width int) string {
	var eyeRow, mouthRow string
	ant := "╶──╴"

	switch state {
	case petIdle:
		eyeRow, mouthRow = "  ◠  ◠  ", "   ω    "
	case petWorking:
		eyeRow, mouthRow = "  ▶  ◀  ", "   ω    "
	case petThinking:
		eyeRow, mouthRow = "  ○  ○  ", "   △    "
		ant = "╶─⊙╴"
	}

	row0 := "  " + ant
	row1 := "╭────────╮"
	row2 := "│" + bodyFill.Render(padCell(eyeRow, 8)) + "│"
	row3 := "│" + bodyFill.Render(padCell(mouthRow, 8)) + "│"
	row4 := "╰────────╯"

	var sb strings.Builder
	sb.WriteString(row0)
	sb.WriteString("\n")
	sb.WriteString(row1)
	sb.WriteString("\n")
	sb.WriteString(row2)
	sb.WriteString("\n")
	sb.WriteString(row3)
	sb.WriteString("\n")
	sb.WriteString(row4)

	return sb.String()
}

func padCell(s string, width int) string {
	for lipgloss.Width(s) < width {
		s += " "
	}
	return s
}

func renderMiniPet(state petState) string {
	var eyes string
	switch state {
	case petIdle:
		eyes = "◠◠"
	case petWorking:
		eyes = "▶◀"
	case petThinking:
		eyes = "○○"
	}
	return bodyFill.Render(" " + eyes + " ")
}
