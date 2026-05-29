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
	switch state {
	case petIdle:
		eyeRow, mouthRow = "  ◠  ◠  ", "   ω    "
	case petWorking:
		eyeRow, mouthRow = "  ▶  ◀  ", "   ω    "
	case petThinking:
		eyeRow, mouthRow = "  ○  ○  ", "   △    "
	}

	row0 := "╭────────╮"
	row1 := "│" + fillPetCell(eyeRow, 8) + "│"
	row2 := "│" + fillPetCell(mouthRow, 8) + "│"
	row3 := "╰────────╯"

	var sb strings.Builder
	sb.WriteString(row0)
	sb.WriteString("\n")
	sb.WriteString(row1)
	sb.WriteString("\n")
	sb.WriteString(row2)
	sb.WriteString("\n")
	sb.WriteString(row3)

	return sb.String()
}

func padCell(s string, width int) string {
	for lipgloss.Width(s) < width {
		s += " "
	}
	return s
}

func fillPetCell(s string, width int) string {
	// lipgloss 的背景色只覆盖实际渲染宽度；这里统一补齐宽度后再设置 Width，
	// 避免中文终端/不同字体下小 logo 出现蓝色填充断裂。
	return bodyFill.Width(width).Render(padCell(s, width))
}

func renderMiniPet(state petState) string {
	var eyeRow, baseInner string
	switch state {
	case petIdle:
		eyeRow, baseInner = " ◠  ◠ ", "──────"
	case petWorking:
		eyeRow, baseInner = " ▶  ◀ ", "──⚡──"
	case petThinking:
		eyeRow, baseInner = " ○  ○ ", "──△──"
	}

	return strings.Join([]string{
		"╭──────╮",
		"│" + fillPetCell(eyeRow, 6) + "│",
		"╰" + padMiniBase(baseInner, 6) + "╯",
	}, "\n")
}

func padMiniBase(s string, width int) string {
	for lipgloss.Width(s) < width {
		s += "─"
	}
	return s
}
