package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/alanchenchen/suna/internal/protocol"
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

func (t *TUI) renderCompactPanel(r protocol.CompactResult) string {
	pct := func(tokens, window int) string {
		if window <= 0 {
			return ""
		}
		p := float64(tokens) / float64(window) * 100
		return fmt.Sprintf(" (%.0f%% %s)", p, t.i18n.T("compact.window"))
	}

	lines := []string{
		fmt.Sprintf("  %s", t.i18n.T("compact.done")),
		"",
		fmt.Sprintf("  %s: %s%s", t.i18n.T("compact.before"), fmtTok(r.BeforeTokens), pct(r.BeforeTokens, r.ContextWindow)),
		fmt.Sprintf("  %s: %s%s", t.i18n.T("compact.after"), fmtTok(r.AfterTokens), pct(r.AfterTokens, r.ContextWindow)),
		"",
		fmt.Sprintf("  %s: %s", t.i18n.T("compact.retained"), t.i18n.T("compact.dynamic_recent")),
	}
	if r.Noop {
		lines = append(lines, fmt.Sprintf("  %s", t.i18n.T("compact.noop")))
	}
	if r.TurnsCompressed > 0 {
		lines = append(lines, fmt.Sprintf("  %s: %d %s → 1 %s (~%s)", t.i18n.T("compact.summary"), r.TurnsCompressed, t.i18n.T("compact.turns"), t.i18n.T("compact.summary_unit"), fmtTok(r.SummaryTokens)))
	}
	if r.TruncatedOutputs > 0 {
		lines = append(lines, fmt.Sprintf("  %s: %d %s", t.i18n.T("compact.truncated"), r.TruncatedOutputs, t.i18n.T("compact.tool_outputs")))
	}
	lines = append(lines, "", fmt.Sprintf("  %s", t.i18n.T("compact.original")))

	body := strings.Join(lines, "\n")
	return boxStyle.Width(56).Padding(1, 2).Render(body)
}
