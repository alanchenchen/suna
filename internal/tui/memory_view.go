package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/alanchenchen/suna/internal/protocol"
)

func (t *TUI) renderMemoryList(memories []protocol.MemoryItem) string {
	width := max(36, min(t.width-6, 92))
	inner := max(24, width-8)
	var lines []string
	lines = append(lines, styleHL.Render(t.tr("tui.memory.active_title")))
	for _, m := range memories {
		badge := fmt.Sprintf("%s:%d", m.Kind, m.Priority)
		if m.IsCore {
			badge = "core " + badge
		}
		prefix := styleDim.Render("• ") + styleTool.Render("["+badge+"]") + " "
		wrapped := wrapLine(strings.TrimSpace(m.Content), max(12, inner-lipglossWidthPlain(prefix)))
		if len(wrapped) == 0 {
			wrapped = []string{""}
		}
		lines = append(lines, prefix+wrapped[0])
		for _, line := range wrapped[1:] {
			lines = append(lines, strings.Repeat(" ", lipglossWidthPlain(prefix))+styleToolDim.Render(line))
		}
	}
	return boxStyle.Width(width).Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func lipglossWidthPlain(s string) int {
	return lipgloss.Width(s)
}
