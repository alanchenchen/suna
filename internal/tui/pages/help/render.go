package help

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

type Command struct {
	Cmd     string
	DescKey string
}

type RenderDeps struct {
	Tr    func(string) string
	HL    lipgloss.Style
	Brand lipgloss.Style
	Dim   lipgloss.Style
	Box   lipgloss.Style
}

func RenderOverlay(width int, commands []Command, deps RenderDeps) string {
	common := []string{
		deps.HL.Render(deps.Tr("tui.help.common")),
		"  " + deps.Brand.Render("Enter") + deps.Dim.Render("  ") + deps.Tr("tui.key.send"),
		"  " + deps.Brand.Render("Esc") + deps.Dim.Render("    ") + deps.Tr("tui.key.back"),
		"  " + deps.Brand.Render("?") + deps.Dim.Render("      ") + deps.Tr("tui.key.help"),
		"  " + deps.Brand.Render("Ctrl+C") + deps.Dim.Render(" ") + deps.Tr("tui.key.quit"),
	}
	commandLines := []string{deps.HL.Render(deps.Tr("tui.help.commands"))}
	for _, c := range commands {
		commandLines = append(commandLines, commandLine(c.Cmd, c.DescKey, deps))
	}
	more := []string{
		deps.HL.Render(deps.Tr("tui.help.more")),
		"  " + deps.Brand.Render("Shift+Enter/Ctrl+J") + deps.Dim.Render(" ") + deps.Tr("tui.key.newline"),
		"  " + deps.Brand.Render("Shift+drag") + deps.Dim.Render(" ") + deps.Tr("tui.help.copy_native"),
		"  " + deps.Brand.Render("Ctrl+T") + deps.Dim.Render(" ") + deps.Tr("tui.key.tool_detail"),
		"  " + deps.Brand.Render("Ctrl+R") + deps.Dim.Render(" ") + deps.Tr("tui.key.reasoning_detail"),
		"  " + deps.Brand.Render("PgUp/PgDn") + deps.Dim.Render(" ") + deps.Tr("tui.key.scroll_up") + "/" + deps.Tr("tui.key.scroll_down"),
	}
	body := strings.Join(common, "\n") + "\n\n" + strings.Join(commandLines, "\n") + "\n\n" + strings.Join(more, "\n")
	w := min(max(44, maxLineWidth(body)+4), max(20, width-8))
	return deps.Box.Width(w).Padding(1, 2).Render(body)
}

func RenderContent(commands []Command, deps RenderDeps) string {
	commandLines := []string{deps.HL.Render(deps.Tr("tui.help.commands"))}
	for _, c := range commands {
		commandLines = append(commandLines, commandLine(c.Cmd, c.DescKey, deps))
	}
	sections := []string{
		deps.HL.Render(deps.Tr("tui.help.discover")),
		helpText("tui.help.discover_commands", deps),
		helpText("tui.help.discover_config", deps),
		helpText("tui.help.discover_details", deps),
		"",
		deps.HL.Render(deps.Tr("tui.help.chat_basics")),
		helpLine("Enter", "tui.help.chat_send", deps),
		helpLine("Shift+Enter/Ctrl+J", "tui.help.chat_newline", deps),
		helpLine("Esc", "tui.help.chat_back", deps),
		helpLine("/help", "tui.key.help", deps),
		helpLine("↑/↓", "tui.help.chat_history", deps),
		helpLine("PgUp/PgDn", "tui.help.chat_scroll", deps),
		helpLine("Shift+drag", "tui.help.copy_native", deps),
		helpLine("Ctrl+C", "tui.key.quit", deps),
		"",
		strings.Join(commandLines, "\n"),
		"",
		deps.HL.Render(deps.Tr("tui.help.details")),
		helpLine("Ctrl+T", "tui.help.tool_detail", deps),
		helpLine("↑/↓", "tui.help.tool_switch", deps),
		helpLine("Ctrl+R", "tui.help.reasoning_detail", deps),
		"",
		deps.HL.Render(deps.Tr("tui.help.tools_safety")),
		helpText("tui.help.tool_guard", deps),
		helpText("tui.help.workspace_guard", deps),
		"",
		deps.HL.Render(deps.Tr("tui.help.config")),
		helpText("tui.help.config_menu", deps),
		helpText("tui.help.config_space", deps),
		helpText("tui.help.config_workspace", deps),
		"",
		deps.HL.Render(deps.Tr("tui.help.troubleshooting")),
		helpText("tui.help.slash_unknown", deps),
		helpText("tui.help.cancel", deps),
		helpText("tui.help.workspace_error", deps),
	}
	return "\n" + strings.Join(sections, "\n") + "\n"
}

func maxLineWidth(s string) int {
	maxWidth := 0
	for _, line := range strings.Split(s, "\n") {
		maxWidth = max(maxWidth, lipgloss.Width(line))
	}
	return maxWidth
}

func helpLine(keyText, descKey string, deps RenderDeps) string {
	return fmt.Sprintf("  %-14s %s", deps.Brand.Render(keyText), deps.Tr(descKey))
}

func helpText(key string, deps RenderDeps) string {
	return "  " + deps.Tr(key)
}

func commandLine(cmd, descKey string, deps RenderDeps) string {
	return fmt.Sprintf("  %-18s %s", deps.Brand.Render(cmd), deps.Tr(descKey))
}
