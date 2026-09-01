package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"

	"github.com/alanchenchen/suna/internal/tui/components/combobox"
	"github.com/alanchenchen/suna/internal/tui/components/overlay"
	"github.com/alanchenchen/suna/internal/tui/components/selection"
	tuiconfig "github.com/alanchenchen/suna/internal/tui/pages/config"
)

func (t *TUI) viewConfig() string {
	if t.config.ReasoningOpen {
		base := t.viewConfigPage()
		return overlay.OverlayBlock(base, t.viewReasoning())
	}
	if t.config.WorkspaceOpen {
		return t.viewWorkspaceForm()
	}
	if t.config.FormOpen {
		// 模型选择浮层打开时渲染在表单之上；否则浮层状态已打开但画面仍是表单。
		if t.modelPickerOpen {
			return overlay.OverlayBlock(t.viewProviderForm(), t.renderModelPickerOverlay(t.width))
		}
		return t.viewProviderForm()
	}
	base := t.viewConfigPage()
	// 管理分组打开的 chat overlay 浮在 Config 页之上；esc 关闭后回到 Config。
	if t.chat.SkillsOverlayOpen {
		return overlay.OverlayBlock(base, t.renderSkillsOverlay(t.width))
	}
	if t.chat.MCPOverlayOpen {
		return overlay.OverlayBlock(base, t.renderMCPOverlay(t.width))
	}
	if t.chat.MemoryOverlayOpen {
		return overlay.OverlayBlock(base, t.renderMemoryOverlay(t.width))
	}
	if t.showHelp {
		return overlay.OverlayBlock(base, t.renderHelpOverlay(t.width))
	}
	return base
}

// renderModelPickerOverlay 渲染 provider 表单的模型选择浮层：
// 同步选择器（输入即过滤）+ 状态行（加载中/拉取失败）+ 键位提示。
func (t *TUI) renderModelPickerOverlay(width int) string {
	panelWidth := nativeListWidth(width, 60)
	innerWidth := max(1, panelWidth-6)
	t.modelCombobox.SetSize(innerWidth, 8)

	var lines []string
	lines = append(lines, styleHL.Render(t.tr("tui.config.picker.title")))
	lines = append(lines, styleDim.Render(strings.Repeat("─", innerWidth)))
	lines = append(lines, t.modelCombobox.View(combobox.Styles{
		Cursor: styleCursor,
		Value:  styleHL,
		Dim:    styleDim,
	}))
	if t.modelPickerLoading {
		lines = append(lines, "", styleDim.Render("• "+t.tr("tui.config.provider.models_loading")))
	}
	if detail := nativeListError(t.modelPickerError, innerWidth); detail != "" {
		lines = append(lines, detail)
	}
	lines = append(lines, "", t.modelPickerFooter())
	return boxStyle.Width(panelWidth).Padding(1, 2).Render(strings.Join(lines, "\n"))
}

// modelPickerFooter 按当前选择器状态显示最小键位集：
// 有候选时提示选择，输入非空时提示可直接确认自定义名。
func (t *TUI) modelPickerFooter() string {
	parts := []string{
		styleCursor.Render("↑↓") + " " + styleDim.Render(t.tr("tui.list.key.move")),
	}
	if t.modelCombobox.Count() > 0 {
		parts = append(parts, styleCursor.Render("Enter")+" "+styleDim.Render(t.tr("tui.config.picker.select")))
	}
	if t.modelCombobox.InputValue() != "" {
		parts = append(parts, styleCursor.Render("Enter")+" "+styleDim.Render(t.tr("tui.config.picker.use_input")))
	}
	parts = append(parts, styleCursor.Render("Esc")+" "+styleDim.Render(t.tr("tui.list.close")))
	return strings.Join(parts, styleDim.Render("  ·  "))
}

func (t *TUI) viewConfigPage() string {
	rows := t.configRows()
	title := t.configTitle()
	header := renderHeader(title, "[Esc] "+t.tr("tui.key.back"), t.width)

	// 逐行收集渲染结果：每个 row 渲染到独立 builder 后拆行，
	// 这样能精确记录 cursor 所在行号，滚动截取时 cursor 自动跟随。
	var lines []string
	lines = append(lines, "")
	cursorLine := -1
	// cursorHeaderLine 记录 cursor 所属 provider 分组头的行号（-1 表示无），
	// 在同一循环内同步记录，避免重复渲染换算行号。
	cursorHeaderLine := -1
	lastHeaderLine := -1
	for i, row := range rows {
		var sb strings.Builder
		if row.Kind == "label" {
			sb.WriteString("    " + styleDim.Render(row.Label))
		} else if row.Kind == "info" {
			t.renderConfigInfoRow(&sb, row.Label, row.Value)
		} else {
			t.renderConfigRow(&sb, i, row)
			if row.Kind == "model" || row.Kind == "provider_end" || row.Kind == "add_provider_model" {
				sb.WriteString("\n")
			}
		}
		rowLines := strings.Split(sb.String(), "\n")
		// 渲染函数以 \n 结尾，Split 会产生尾部空元素（行尾而非空行），去掉。
		if n := len(rowLines); n > 0 && rowLines[n-1] == "" {
			rowLines = rowLines[:n-1]
		}
		if row.Kind == "provider_header" {
			lastHeaderLine = len(lines)
		}
		if i == t.config.Cursor {
			cursorLine = len(lines)
			cursorHeaderLine = lastHeaderLine
		}
		lines = append(lines, rowLines...)
	}
	if t.config.Error != "" {
		lines = append(lines, "", styleError.Render("  ✗ "+t.config.Error))
	}
	if t.config.Notice != "" {
		lines = append(lines, "", styleDim.Render("  • "+t.config.Notice))
	}
	if t.config.DeleteConfirm != "" {
		lines = append(lines, "", t.renderConfigDeleteConfirm())
	}
	if help := t.configHelp(rows); help != "" {
		lines = append(lines, "", styleDim.Render("  "+help))
	}

	// 可用高度 = 终端高度 - header 两行；底部留一行给滚动提示。
	avail := max(1, t.height-3)
	// cursor 行不可见时自动滚动到可见（最小滚动：贴底滚一行，贴顶滚一行）。
	if cursorLine >= 0 {
		if cursorLine < t.config.Scroll {
			t.config.Scroll = cursorLine
		} else if cursorLine >= t.config.Scroll+avail {
			t.config.Scroll = cursorLine - avail + 1
		}
		// cursor 所属 provider 的分组头保持可见：在某个 provider 的模型间移动时，
		// 头行留在视口外会让用户失去"当前在哪个 provider"的上下文。
		// 仅在头部与 cursor 能同时放进视口时生效；分组高超过视口时 cursor 可见性优先。
		if h := cursorHeaderLine; h >= 0 && h < cursorLine && cursorLine-h < avail {
			t.config.Scroll = min(t.config.Scroll, h)
		}
	}
	maxScroll := max(0, len(lines)-avail)
	if t.config.Scroll > maxScroll {
		t.config.Scroll = maxScroll
	}
	if t.config.Scroll < 0 {
		t.config.Scroll = 0
	}
	visible := lines[t.config.Scroll:min(len(lines), t.config.Scroll+avail)]
	out := header + "\n" + strings.Join(visible, "\n")
	if t.config.Scroll < maxScroll {
		out += "\n" + styleDim.Render("  ↓ "+t.tr("tui.config.scroll_more"))
	}
	return out
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
		ManageSkills:  t.tr("tui.config.help_manage_skills"),
		ManageMCP:     t.tr("tui.config.help_manage_mcp"),
		ManageMemory:  t.tr("tui.config.help_manage_memory"),
	})
}

func (t *TUI) viewProviderForm() string {
	view := t.config.ProviderFormView(t.tr(t.config.FormTitle), t.tr("tui.config.setup_title"), t.providerFormHelp(), min(max(48, t.width-8), 72))
	var lines []string
	for i, in := range t.config.Inputs {
		if t.config.FormProvider != "" && i == tuiconfig.ProviderFormProviderIndex {
			lines = append(lines, styleDim.Render(t.tr("tui.config.provider.type")+": ")+styleHL.Render(t.config.FormProvider)+styleDim.Render("  "+t.tr("tui.config.locked")))
			continue
		}
		if t.config.FormProvider != "" && i == tuiconfig.ProviderFormAPIKeyIndex {
			lines = append(lines, styleDim.Render(t.tr("tui.config.provider.api_key")+": ")+styleDim.Render(t.i18n.Tf("tui.config.api_key_reused", t.config.FormProvider)))
			continue
		}
		if i == tuiconfig.ProviderFormModelIndex {
			lines = append(lines, t.providerModelChoiceView(in))
			continue
		}
		if i == tuiconfig.ProviderFormProtocolIndex {
			lines = append(lines, t.providerProtocolInputView(in))
			continue
		}
		if i == tuiconfig.ProviderFormAuthModeIndex {
			if t.providerFormUsesAnthropic() {
				lines = append(lines, t.providerAuthModeInputView(in))
			}
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

// providerFormHelp 返回表单底部说明：焦点字段的短动态提示一行 + 固定键位
// 一行。动态提示只讲"Enter 在这个字段是什么意思"（选模型/切换选项/输入），
// 固定行只保留全局导航键位，不重复 Enter 语义，避免窄终端折行。
func (t *TUI) providerFormHelp() string {
	parts := []string{styleDim.Render("  " + t.providerFieldHint(t.config.InputFocus))}
	parts = append(parts, styleDim.Render("  "+t.tr("tui.config.form_help")))
	return strings.Join(parts, "\n")
}

// providerFieldHint 返回焦点字段的 Enter 语义短提示。
func (t *TUI) providerFieldHint(idx int) string {
	switch idx {
	case tuiconfig.ProviderFormModelIndex:
		return t.tr("tui.config.hint.model")
	case tuiconfig.ProviderFormProtocolIndex:
		return t.tr("tui.config.hint.protocol")
	case tuiconfig.ProviderFormAuthModeIndex:
		return t.tr("tui.config.hint.auth_mode")
	}
	return t.tr("tui.config.hint.input")
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
	prefix := selection.Rail(t.config.Cursor == idx, 2, styleCursor)
	st := lipgloss.NewStyle()
	if t.config.Cursor == idx {
		st = styleHL
	}
	if row.Kind == "add_provider_model" && t.config.Cursor != idx {
		st = styleBrand
	}
	sb.WriteString(prefix + t.configRowLabelStyle(label, st))
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
	prefix := selection.Rail(selected, 2, styleCursor)
	label := t.tr("tui.config.add_model_short")
	if selected {
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
	active := t.isDefaultModelRef(mc.Ref())
	prefix := selection.Rail(selected, 2, styleCursor)
	bodyIndent := "      "

	nameStyle := lipgloss.NewStyle().Foreground(currentTheme.Text).Bold(true)
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

// providerModelChoiceView 将 model 字段渲染为纯值行，不用 ‹ › 箭头：
// 那是 protocol/auth_mode 循环切换的语义，model 字段用 Enter 打开选择浮层，
// 箭头会误导用户以为可以左右切换。未设置时显示提示文案。
func (t *TUI) providerModelChoiceView(in textinput.Model) string {
	value := strings.TrimSpace(in.Value())
	prompt := in.Prompt
	focused := t.config.InputFocus == tuiconfig.ProviderFormModelIndex
	if focused {
		prompt = styleBrand.Render(prompt)
	}
	if value == "" {
		value = t.tr("tui.config.provider.model_unset")
		if focused {
			return prompt + styleHL.Render(value)
		}
		return prompt + styleDim.Render(value)
	}
	style := styleDim
	if focused {
		style = styleHL
	}
	return prompt + style.Render(value)
}

func (t *TUI) providerProtocolInputView(in textinput.Model) string {
	label := t.tr("tui.config.protocol." + in.Value())
	if strings.HasPrefix(label, "tui.config.protocol.") {
		label = in.Value()
	}
	return t.providerChoiceInputView(in, label, tuiconfig.ProviderFormProtocolIndex)
}

func (t *TUI) providerAuthModeInputView(in textinput.Model) string {
	key := in.Value()
	if key == "" {
		key = "default"
	}
	label := t.tr("tui.config.auth_mode." + key)
	if strings.HasPrefix(label, "tui.config.auth_mode.") {
		label = key
	}
	return t.providerChoiceInputView(in, label, tuiconfig.ProviderFormAuthModeIndex)
}

func (t *TUI) providerChoiceInputView(in textinput.Model, label string, focusIndex int) string {
	prompt := in.Prompt
	style := styleDim
	if t.config.InputFocus == focusIndex {
		prompt = styleBrand.Render(prompt)
		style = styleHL
	}
	return prompt + style.Render("‹ "+label+" ›")
}
