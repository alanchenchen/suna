package tui

import (
	"fmt"

	"github.com/alanchenchen/suna/internal/ipc"
)

func (t *TUI) renderCompactPanel(r ipc.CompactResult) string {
	pct := func(tokens, window int) string {
		if window <= 0 {
			return ""
		}
		p := float64(tokens) / float64(window) * 100
		return fmt.Sprintf(" (%.0f%% %s)", p, t.i18n.T("compact.window"))
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("  %s", t.i18n.T("compact.done")))
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  %s: %s%s", t.i18n.T("compact.before"), fmtTok(r.BeforeTokens), pct(r.BeforeTokens, r.ContextWindow)))
	lines = append(lines, fmt.Sprintf("  %s: %s%s", t.i18n.T("compact.after"), fmtTok(r.AfterTokens), pct(r.AfterTokens, r.ContextWindow)))
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  %s: %s", t.i18n.T("compact.retained"), t.i18n.Tf("compact.keep_recent", 10)))
	if r.TurnsCompressed > 0 {
		lines = append(lines, fmt.Sprintf("  %s: %d %s → 1 %s (~%s)", t.i18n.T("compact.summary"),
			r.TurnsCompressed, t.i18n.T("compact.turns"), t.i18n.T("compact.summary_unit"), fmtTok(r.SummaryTokens)))
	}
	if r.TruncatedOutputs > 0 {
		lines = append(lines, fmt.Sprintf("  %s: %d %s", t.i18n.T("compact.truncated"), r.TruncatedOutputs, t.i18n.T("compact.tool_outputs")))
	}
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  %s", t.i18n.T("compact.original")))

	var bordered string
	for _, line := range lines {
		bordered += "│ " + line + "\n"
	}
	return "\n┌" + repeatStr("─", 50) + "┐\n" + bordered + "└" + repeatStr("─", 50) + "┘"
}

func repeatStr(s string, n int) string {
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}
