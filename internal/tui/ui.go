package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// UI 通用工具函数和小型面板渲染。
// 这里集中放置跨页面复用的纯布局逻辑，页面文件只保留各自状态机和主要渲染入口。

func fmtTok(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func joinNonEmpty(parts []string, sep string) string {
	var nonEmpty []string
	for _, p := range parts {
		if p != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	return strings.Join(nonEmpty, sep)
}

func renderHeader(title, right string, width int) string {
	if width <= 0 {
		width = 80
	}
	left := "  " + styleHL.Render(title)
	r := styleMuted.Render(right)
	pad := max(1, width-lipgloss.Width(left)-lipgloss.Width(r)-2)
	return left + strings.Repeat(" ", pad) + r + "\n" + styleDim.Render(strings.Repeat("─", width))
}
