// Package selection 提供 TUI 纵向选择列表共用的轻量选中指示。
package selection

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Rail 在固定宽度内渲染选中条，避免光标移动时列表内容横向跳动。
func Rail(selected bool, indent int, style lipgloss.Style) string {
	indent = max(0, indent)
	if !selected {
		return strings.Repeat(" ", indent+2)
	}
	return strings.Repeat(" ", indent) + style.Render("▎ ")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
