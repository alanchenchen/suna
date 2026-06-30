package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"

	overlay "github.com/alanchenchen/suna/internal/tui/components/overlay"
	tuiconfig "github.com/alanchenchen/suna/internal/tui/pages/config"
)

func (t *TUI) viewConfig() string {
	if t.config.ReasoningOpen {
		base := t.viewConfigPage()
		return overlay.OverlayBlock(base, t.viewReasoning())
	}
	if t.config.KindOpen {
		base := t.viewConfigPage()
		return overlay.OverlayBlock(base, t.viewProviderKind())
	}
	if t.config.WorkspaceOpen {
		return t.viewWorkspaceForm()
	}
	if t.config.FormOpen {
		return t.viewProviderForm()
	}
	base := t.viewConfigPage()
	if t.showHelp {
		return overlay.OverlayBlock(base, t.renderHelpOverlay(t.width))
	}
	return base
}

func (t *TUI) viewProviderKind() string {
	view := t.config.ProviderKindView(tuiconfig.ProviderKindLabels{
		Title: t.tr("tui.config.provider.kind_title"),
		Help:  t.tr("tui.config.kind_help"),
		Name:  func(opt string) string { return t.tr("tui.config.kind." + opt) },
		Desc:  func(opt string) string { return t.tr("tui.config.kind." + opt + ".desc") },
	}, min(max(48, t.width-8), 72))
	var lines []string
	for _, opt := range view.Options {
		cursor := "  "
		st := lipgloss.NewStyle()
		if opt.Selected {
			cursor = styleCursor.Render("▶ ")
			st = styleHL
		}
		lines = append(lines, cursor+st.Render(opt.Name))
		lines = append(lines, "    "+styleDim.Render(opt.Desc))
	}
	lines = append(lines, "", styleDim.Render(view.Help))
	body := strings.Join(lines, "\n")
	return boxStyle.Width(view.Width).Padding(1, 2).Render(styleHL.Render(view.Title) + "\n\n" + body)
}

func (t *TUI) viewConfigPage() string {
	rows := t.configRows()
	var sb strings.Builder
	title := t.configTitle()
	sb.WriteString(renderHeader(title, "[Esc] "+t.tr("tui.key.back"), t.width))
	sb.WriteString("\n\n")
	for i, row := range rows {
		if row.Kind == "label" {
			sb.WriteString("    " + styleDim.Render(row.Label) + "\n")
			continue
		}
		if row.Kind == "info" {
			t.renderConfigInfoRow(&sb, row.Label, row.Value)
			continue
		}
		t.renderConfigRow(&sb, i, row)
		if row.Kind == "model" {
			sb.WriteString("\n")
		}
	}
	if t.config.Error != "" {
		sb.WriteString("\n" + styleError.Render("  ✗ "+t.config.Error) + "\n")
	}
	if t.config.Notice != "" {
		sb.WriteString("\n" + styleDim.Render("  • "+t.config.Notice) + "\n")
	}
	if t.config.DeleteConfirm != "" {
		sb.WriteString("\n" + t.renderConfigDeleteConfirm() + "\n")
	}
	if help := t.configHelp(rows); help != "" {
		sb.WriteString("\n" + styleDim.Render("  "+help) + "\n")
	}
	return sb.String()
}

func (t *TUI) configTitle() string {
	return t.config.Title(t.tr("tui.config.title"), t.tr("tui.config.model_connections"), t.tr("tui.config.provider.section"))
}

func (t *TUI) configHelp(rows []tuiconfig.Row) string {
	return t.config.Help(rows, tuiconfig.HelpLabels{
		OpenModels:    t.tr("tui.config.help_open_models"),
		Language:      t.tr("tui.config.help_language"),
		Theme:         t.tr("tui.config.help_theme"),
		Guard:         t.tr("tui.config.help_guard"),
		Workspace:     t.tr("tui.config.help_workspace"),
		Attachments:   t.tr("tui.config.help_attachments"),
		OpenConfigDir: t.tr("tui.config.help_open_config_dir"),
		AddModel:      t.tr("tui.config.help_add_model"),
		ModelRow:      t.tr("tui.config.help_model_row"),
		EditModel:     t.tr("tui.config.help_edit_model"),
		Reasoning:     t.tr("tui.config.help_reasoning"),
		ActivateModel: t.tr("tui.config.help_activate_model"),
		DeleteModel:   t.tr("tui.config.help_delete_model"),
		Models:        t.tr("tui.config.help_models"),
		Detail:        t.tr("tui.config.help_detail"),
		Home:          t.tr("tui.config.help_home"),
	})
}

func (t *TUI) viewProviderForm() string {
	view := t.config.ProviderFormView(t.tr(t.config.FormTitle), t.tr("tui.config.setup_title"), t.tr("tui.config.form_help"), min(max(48, t.width-8), 72))
	var lines []string
	for i, in := range t.config.Inputs {
		if i == tuiconfig.ProviderFormProtocolIndex {
			lines = append(lines, t.providerProtocolInputView(in))
			continue
		}
		lines = append(lines, in.View())
	}
	if view.Error != "" {
		lines = append(lines, "", styleError.Render("✗ "+view.Error))
	}
	if view.Notice != "" {
		lines = append(lines, "", styleDim.Render("• "+view.Notice))
	}
	lines = append(lines, "", styleDim.Render(view.Help))
	body := strings.Join(lines, "\n")
	return boxStyle.Width(view.Width).Padding(1, 2).Render(styleHL.Render(view.Title) + "\n\n" + body)
}

func (t *TUI) viewWorkspaceForm() string {
	view := t.config.WorkspaceFormView(t.tr(t.config.FormTitle), t.tr("tui.config.workspace.help"), t.tr("tui.config.workspace.form_help"), min(max(54, t.width-8), 86))
	var lines []string
	for _, in := range t.config.Inputs {
		lines = append(lines, in.View())
	}
	for _, help := range strings.Split(view.Help, "\n") {
		if help != "" {
			lines = append(lines, "", styleDim.Render(help))
		}
	}
	if view.Error != "" {
		lines = append(lines, "", styleError.Render("✗ "+view.Error))
	}
	body := strings.Join(lines, "\n")
	return boxStyle.Width(view.Width).Padding(1, 2).Render(styleHL.Render(view.Title) + "\n\n" + body)
}

func (t *TUI) renderConfigRow(sb *strings.Builder, idx int, row tuiconfig.Row) {
	label, value := row.Label, row.Value
	cursor := "    "
	st := lipgloss.NewStyle()
	if row.Kind == "model" && t.isActiveModelRef(row.Name) {
		st = styleBrand
	}
	if t.config.Cursor == idx {
		cursor = styleCursor.Render("  ▶ ")
		st = styleHL
		if row.Kind == "model" && t.isActiveModelRef(row.Name) {
			st = styleBrand
		}
	}
	sb.WriteString(cursor + t.configRowLabelStyle(label, st))
	if value != "" {
		valueStyle := styleDim
		if row.Kind == "model" && t.isActiveModelRef(row.Name) {
			valueStyle = styleBrand
		}
		sb.WriteString(styleDim.Render("  ") + valueStyle.Render(value))
	}
	sb.WriteString("\n")
}

func (t *TUI) configRowLabelStyle(label string, st lipgloss.Style) string {
	switch tuiconfig.RowLabelTone(label, t.tr("tui.config.activate_model"), t.tr("tui.config.attachments"), t.tr("tui.config.open_config_folder"), t.tr("tui.config.delete_model")) {
	case tuiconfig.RowToneAgent:
		return styleAgent.Render(label)
	case tuiconfig.RowToneError:
		return styleError.Render(label)
	case tuiconfig.RowToneBrand:
		return styleBrand.Render(label)
	default:
		return st.Render(label)
	}
}

func (t *TUI) renderConfigDeleteConfirm() string {
	provider := ""
	offerAPIKey := t.shouldOfferDeleteAPIKey(t.config.DeleteConfirm)
	if offerAPIKey {
		if mc, ok := t.modelByRef(t.config.DeleteConfirm); ok {
			provider = mc.Provider
		}
	}
	view := t.config.DeleteConfirmView(tuiconfig.DeleteConfirmLabels{
		Cancel:               t.tr("tui.config.cancel"),
		DeleteModel:          t.tr("tui.config.delete_model"),
		DeleteModelAndAPIKey: t.tr("tui.config.delete_model_and_api_key"),
		Confirm:              t.i18n.Tf("tui.config.delete_confirm", t.config.DeleteConfirm),
		LastProviderKeyHint:  t.i18n.Tf("tui.config.delete_last_provider_key_hint", provider),
		Help:                 t.tr("tui.config.delete_help"),
	}, offerAPIKey, provider, min(max(44, t.width-8), 72))
	message := styleError.Render("✗ " + view.Message)
	if view.Hint != "" {
		message += "\n" + styleDim.Render(view.Hint)
	}
	buttons := make([]string, 0, len(view.Options))
	for i, label := range view.Options {
		buttons = append(buttons, t.configConfirmButton(i, label))
	}
	body := message + "\n\n" + strings.Join(buttons, "  ") + "\n" + styleDim.Render(view.Help)
	return boxStyle.Width(view.MaxWidth).Padding(1, 2).Render(body)
}

func (t *TUI) configConfirmButton(idx int, label string) string {
	if t.config.DeleteCursor == idx {
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

func (t *TUI) providerProtocolInputView(in textinput.Model) string {
	label := t.tr("tui.config.protocol." + in.Value())
	if strings.HasPrefix(label, "tui.config.protocol.") {
		label = in.Value()
	}
	prompt := in.Prompt
	style := styleDim
	if t.config.InputFocus == tuiconfig.ProviderFormProtocolIndex {
		prompt = styleBrand.Render(prompt)
		style = styleHL
	}
	return prompt + style.Render("‹ "+label+" ›")
}
