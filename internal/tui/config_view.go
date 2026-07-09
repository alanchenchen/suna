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
		if row.Kind == "model" || row.Kind == "provider_end" || row.Kind == "add_provider_model" {
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
		if t.config.FormProvider != "" && i == 0 {
			lines = append(lines, styleDim.Render(t.tr("tui.config.provider.type")+": ")+styleHL.Render(t.config.FormProvider)+styleDim.Render("  "+t.tr("tui.config.locked")))
			continue
		}
		if t.config.FormProvider != "" && i == 3 {
			lines = append(lines, styleDim.Render(t.tr("tui.config.provider.api_key")+": ")+styleDim.Render(t.i18n.Tf("tui.config.api_key_reused", t.config.FormProvider)))
			continue
		}
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
	if row.Kind == "provider_header" {
		sb.WriteString(t.renderConfigProviderHeader(row.Label) + "\n")
		return
	}
	if row.Kind == "provider_end" {
		sb.WriteString(t.renderConfigProviderEnd() + "\n")
		return
	}
	if row.Kind == "model" {
		t.renderConfigModelRow(sb, idx, row)
		return
	}
	if row.Kind == "provider_add_model" {
		t.renderConfigProviderAddRow(sb, idx, row)
		return
	}
	label, value := row.Label, row.Value
	if row.Kind == "attachments_disabled" {
		sb.WriteString("    " + styleDim.Render(label))
		if value != "" {
			sb.WriteString(styleDim.Render("  ") + styleDim.Render(value))
		}
		sb.WriteString("\n")
		return
	}
	cursor := "    "
	st := lipgloss.NewStyle()
	if t.config.Cursor == idx {
		cursor = styleCursor.Render("  ▶ ")
		st = styleHL
	}
	if row.Kind == "add_provider_model" && t.config.Cursor != idx {
		st = styleBrand
	}
	sb.WriteString(cursor + t.configRowLabelStyle(label, st))
	if value != "" {
		valueStyle := styleDim
		sb.WriteString(styleDim.Render("  ") + valueStyle.Render(value))
	}
	sb.WriteString("\n")
}

func (t *TUI) renderConfigProviderHeader(provider string) string {
	name := strings.TrimSpace(provider)
	if name == "" {
		name = t.tr("tui.config.provider.unnamed")
	}
	label := lipgloss.NewStyle().Foreground(currentTheme.MutedText).Bold(true).Render(name)
	lineWidth := max(8, min(28, t.width-lipgloss.Width(name)-14))
	return "  " + styleDim.Render("╭─ ") + label + styleDim.Render(" "+strings.Repeat("─", lineWidth))
}

func (t *TUI) renderConfigProviderEnd() string {
	return ""
}

func (t *TUI) configModelLine(indent, content string) string {
	width := max(20, t.width-8-lipgloss.Width(indent))
	return indent + truncateDisplay(content, width)
}

func (t *TUI) renderConfigProviderAddRow(sb *strings.Builder, idx int, row tuiconfig.Row) {
	selected := t.config.Cursor == idx
	prefix := "    "
	label := t.tr("tui.config.add_model_short")
	if selected {
		prefix = "  " + styleCursor.Render("▶ ")
		label = styleHL.Render("+ " + label)
	} else {
		label = styleDim.Render("+ ") + styleBrand.Render(label)
	}
	sb.WriteString(t.configModelLine(prefix, label) + "\n")
}

func (t *TUI) configBadge(text string, active bool) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	st := lipgloss.NewStyle().Padding(0, 1).Bold(true)
	if active {
		return st.Foreground(currentTheme.ToolText).Background(ColorBrand).Render(text)
	}
	return st.Foreground(currentTheme.MutedText).Background(currentTheme.CodeBg).Render(text)
}

func (t *TUI) configSoftBadge(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return lipgloss.NewStyle().Foreground(currentTheme.MutedText).Background(currentTheme.CodeBg).Padding(0, 1).Render(text)
}

func (t *TUI) renderConfigModelRow(sb *strings.Builder, idx int, row tuiconfig.Row) {
	mc, ok := t.modelByRef(row.Name)
	if !ok {
		return
	}
	selected := t.config.Cursor == idx
	active := t.isActiveModelRef(mc.Ref())
	prefix := "    "
	bodyIndent := "      "
	if selected {
		prefix = "  " + styleCursor.Render("▶ ")
		bodyIndent = "      "
	} else if active {
		prefix = "  " + styleBrand.Render("▌ ")
		bodyIndent = "      "
	}

	nameStyle := lipgloss.NewStyle().Foreground(currentTheme.Text).Bold(true)
	if active {
		nameStyle = styleBrand
	}
	if selected {
		nameStyle = styleHL
	}

	badges := []string{}
	if active {
		badges = append(badges, t.configBadge(t.tr("tui.config.active_badge"), true))
	}
	badges = append(badges, t.configSoftBadge(string(mc.Protocol)))
	if tuiconfig.ModelNeedsAttention(mc) {
		badges = append([]string{styleError.Render(t.tr("tui.config.needs_attention_badge"))}, badges...)
	}
	line := nameStyle.Render(mc.Model)
	for _, badge := range badges {
		if badge != "" {
			line += " " + badge
		}
	}
	sb.WriteString(t.configModelLine(prefix, line) + "\n")

	meta := []string{}
	if mc.ContextWindow > 0 {
		meta = append(meta, fmtTok(mc.ContextWindow)+" ctx")
	}
	if mc.MaxOutputTokens > 0 {
		meta = append(meta, fmtTok(mc.MaxOutputTokens)+" out")
	}
	if reasoning := t.reasoningDisplay(mc); reasoning != "" && reasoning != t.tr("tui.config.reasoning.none") {
		meta = append(meta, reasoning)
	}
	if len(meta) > 0 {
		sb.WriteString(t.configModelLine(bodyIndent, styleDim.Render(strings.Join(meta, "  ·  "))) + "\n")
	}

	tail := []string{}
	if strings.TrimSpace(mc.BaseURL) != "" {
		tail = append(tail, t.displayEndpoint(mc.BaseURL))
	}
	if len(mc.Strengths) > 0 {
		tail = append(tail, strings.Join(mc.Strengths, " · "))
	}
	if len(tail) > 0 {
		sb.WriteString(t.configModelLine(bodyIndent, lipgloss.NewStyle().Foreground(currentTheme.SubtleText).Render(strings.Join(tail, "  ·  "))) + "\n")
	}
	if tuiconfig.ModelNeedsAttention(mc) {
		sb.WriteString(t.configModelLine(bodyIndent, styleError.Render(t.modelSummary(mc))) + "\n")
	}
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
	if strings.TrimSpace(label) == t.tr("tui.config.active_model") {
		sb.WriteString("  " + styleDim.Render(label) + styleDim.Render("  ") + styleHL.Render(value) + "\n")
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
