package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

func (t *TUI) viewConfig() string {
	if t.configReasoningOpen {
		base := t.viewConfigPage()
		return overlayBlock(base, t.viewReasoning())
	}
	if t.configKindOpen {
		base := t.viewConfigPage()
		return overlayBlock(base, t.viewProviderKind())
	}
	if t.configWorkspaceOpen {
		return t.viewWorkspaceForm()
	}
	if t.configFormOpen {
		return t.viewProviderForm()
	}
	base := t.viewConfigPage()
	if t.showHelp {
		return overlayBlock(base, t.renderHelpOverlay(t.width))
	}
	return base
}

func (t *TUI) viewProviderKind() string {
	options := t.providerKindOptions()
	var lines []string
	for i, opt := range options {
		cursor := "  "
		st := lipgloss.NewStyle()
		if i == t.configKindCursor {
			cursor = styleCursor.Render("▶ ")
			st = styleHL
		}
		lines = append(lines, cursor+st.Render(t.tr("tui.config.kind."+opt)))
		lines = append(lines, "    "+styleDim.Render(t.tr("tui.config.kind."+opt+".desc")))
	}
	lines = append(lines, "", styleDim.Render(t.tr("tui.config.kind_help")))
	body := strings.Join(lines, "\n")
	w := min(max(48, t.width-8), 72)
	return boxStyle.Width(w).Padding(1, 2).Render(styleHL.Render(t.tr("tui.config.provider.kind_title")) + "\n\n" + body)
}

func (t *TUI) viewConfigPage() string {
	rows := t.configRows()
	var sb strings.Builder
	title := t.configTitle()
	sb.WriteString(renderHeader(title, "[Esc] "+t.tr("tui.key.back"), t.width))
	sb.WriteString("\n\n")
	for i, row := range rows {
		if row.kind == "label" {
			sb.WriteString("    " + styleDim.Render(row.label) + "\n")
			continue
		}
		if row.kind == "info" {
			t.renderConfigInfoRow(&sb, row.label, row.value)
			continue
		}
		t.renderConfigRow(&sb, i, row.label, row.value)
		if row.kind == "model" {
			sb.WriteString("\n")
		}
	}
	if t.configError != "" {
		sb.WriteString("\n" + styleError.Render("  ✗ "+t.configError) + "\n")
	}
	if t.configDeleteConfirm != "" {
		sb.WriteString("\n" + t.renderConfigDeleteConfirm() + "\n")
	}
	if help := t.configHelp(rows); help != "" {
		sb.WriteString("\n" + styleDim.Render("  "+help) + "\n")
	}
	return sb.String()
}

func (t *TUI) configTitle() string {
	switch t.configPage {
	case "models":
		return t.tr("tui.config.model_connections")
	case "detail":
		if t.configDetailRef != "" {
			return t.tr("tui.config.provider.section") + ": " + t.configDetailRef
		}
	}
	return t.tr("tui.config.title")
}

func (t *TUI) configHelp(rows []configRow) string {
	if t.configDeleteConfirm != "" {
		return ""
	}
	if t.configReasoningOpen {
		return ""
	}
	if t.configCursor >= 0 && t.configCursor < len(rows) {
		switch rows[t.configCursor].kind {
		case "section":
			return t.tr("tui.config.help_open_models")
		case "general_language":
			return t.tr("tui.config.help_language")
		case "general_theme":
			return t.tr("tui.config.help_theme")
		case "general_guard":
			return t.tr("tui.config.help_guard")
		case "general_workspace":
			return t.tr("tui.config.help_workspace")
		case "clear_attachments":
			return t.tr("tui.config.help_attachments")
		case "open_config_dir":
			return t.tr("tui.config.help_open_config_dir")
		case "add_model":
			return t.tr("tui.config.help_add_model")
		case "model":
			return t.tr("tui.config.help_model_row")
		case "edit_model":
			return t.tr("tui.config.help_edit_model")
		case "edit_reasoning":
			return t.tr("tui.config.help_reasoning")
		case "activate_model":
			return t.tr("tui.config.help_activate_model")
		case "delete_model":
			return t.tr("tui.config.help_delete_model")
		}
	}
	switch t.configPage {
	case "models":
		return t.tr("tui.config.help_models")
	case "detail":
		return t.tr("tui.config.help_detail")
	default:
		return t.tr("tui.config.help_home")
	}
}

func (t *TUI) viewProviderForm() string {
	title := t.tr(t.configFormTitle)
	if t.configSetupMode {
		title = t.tr("tui.config.setup_title")
	}
	var lines []string
	for _, in := range t.configInputs {
		lines = append(lines, in.View())
	}
	if t.configError != "" {
		lines = append(lines, "", styleError.Render("✗ "+t.configError))
	}
	lines = append(lines, "", styleDim.Render(t.tr("tui.config.form_help")))
	body := strings.Join(lines, "\n")
	w := min(max(48, t.width-8), 72)
	return boxStyle.Width(w).Padding(1, 2).Render(styleHL.Render(title) + "\n\n" + body)
}

func (t *TUI) renderConfigRow(sb *strings.Builder, idx int, label, value string) {
	cursor := "    "
	st := lipgloss.NewStyle()
	if t.configCursor == idx {
		cursor = styleCursor.Render("  ▶ ")
		st = styleHL
	}
	sb.WriteString(cursor + t.configRowLabelStyle(label, st))
	if value != "" {
		sb.WriteString(styleDim.Render("  ") + value)
	}
	sb.WriteString("\n")
}

func (t *TUI) configRowLabelStyle(label string, st lipgloss.Style) string {
	trimmed := strings.TrimSpace(label)
	if strings.HasPrefix(trimmed, "+") || strings.Contains(label, t.tr("tui.config.activate_model")) {
		return styleAgent.Render(label)
	}
	if strings.Contains(label, t.tr("tui.config.attachments")) {
		return styleError.Render(label)
	}
	if strings.HasPrefix(trimmed, "▸") || strings.Contains(label, t.tr("tui.config.open_config_folder")) {
		return styleBrand.Render(label)
	}
	if strings.Contains(label, t.tr("tui.config.delete_model")) {
		return styleError.Render(label)
	}
	return st.Render(label)
}

func (t *TUI) renderConfigDeleteConfirm() string {
	message := styleError.Render("✗ " + t.i18n.Tf("tui.config.delete_confirm", t.configDeleteConfirm))
	if t.shouldOfferDeleteAPIKey(t.configDeleteConfirm) {
		if mc, ok := t.modelByRef(t.configDeleteConfirm); ok {
			message += "\n" + styleDim.Render(t.i18n.Tf("tui.config.delete_last_provider_key_hint", mc.Provider))
		}
	}
	buttons := make([]string, 0, 3)
	for i, label := range t.configDeleteOptions() {
		buttons = append(buttons, t.configConfirmButton(i, label))
	}
	body := message + "\n\n" + strings.Join(buttons, "  ") + "\n" + styleDim.Render(t.tr("tui.config.delete_help"))
	return boxStyle.Width(min(max(44, t.width-8), 72)).Padding(1, 2).Render(body)
}

func (t *TUI) configConfirmButton(idx int, label string) string {
	if t.configDeleteCursor == idx {
		return styleCursor.Render("▶ ") + styleHL.Render(label)
	}
	return styleDim.Render("  " + label)
}

func (t *TUI) renderConfigInfoRow(sb *strings.Builder, label, value string) {
	if label == "" && value == "" {
		sb.WriteString("\n")
		return
	}
	sb.WriteString("    " + styleDim.Render(fmt.Sprintf("%-12s", label)) + " " + value + "\n")
}
