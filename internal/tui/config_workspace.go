package tui

import "strings"

func (t *TUI) viewWorkspaceForm() string {
	var lines []string
	for _, in := range t.configInputs {
		lines = append(lines, in.View())
	}
	lines = append(lines, "", styleDim.Render(t.tr("tui.config.workspace.help")))
	if t.configError != "" {
		lines = append(lines, "", styleError.Render("✗ "+t.configError))
	}
	lines = append(lines, "", styleDim.Render(t.tr("tui.config.workspace.form_help")))
	body := strings.Join(lines, "\n")
	w := min(max(54, t.width-8), 86)
	return boxStyle.Width(w).Padding(1, 2).Render(styleHL.Render(t.tr(t.configFormTitle)) + "\n\n" + body)
}
