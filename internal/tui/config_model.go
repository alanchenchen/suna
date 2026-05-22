package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/alanchenchen/suna/internal/protocol"
)

// Config 的页面模型和纯计算辅助。
// TUI 只维护当前页、光标、表单等轻量状态；真实配置持久化、active model、credential 均由 daemon 通过 protocol 处理。
type configRow struct{ kind, name, label, value string }

func (r configRow) selectable() bool {
	switch r.kind {
	case "section", "general_language", "general_theme", "general_guard", "open_config_dir", "add_model", "edit_model", "activate_model", "delete_model", "check_model", "model", "empty":
		return true
	default:
		return false
	}
}

func (t *TUI) configRows() []configRow {
	if t.configPage == "" {
		t.configPage = "home"
	}
	switch t.configPage {
	case "models":
		return t.configModelRows()
	case "detail":
		return t.configDetailRows()
	default:
		return t.configHomeRows()
	}
}

func (t *TUI) configHomeRows() []configRow {
	active := t.configState.ActiveModel
	if active == "" {
		active = t.tr("tui.config.none")
	}
	needs := 0
	for _, mc := range t.configModelsSnapshot() {
		if t.modelNeedsAttention(mc) {
			needs++
		}
	}
	rows := []configRow{
		{"section", "models", "▸ " + t.tr("tui.config.model_connections"), ""},
		{"info", "", "  " + t.tr("tui.config.active"), active},
		{"info", "", "  " + t.tr("tui.config.providers"), t.i18n.Tf("tui.config.providers_summary", len(t.configState.Models), needs)},
		{"info", "", "", ""},
		{"label", "", t.tr("tui.config.general.section"), ""},
		{"general_language", "", "  " + t.tr("tui.config.language"), t.currentLangDisplay()},
		{"general_theme", "", "  " + t.tr("tui.config.theme"), t.themeDisplay()},
		{"general_guard", "", "  " + t.tr("tui.config.guard_mode"), t.guardModeDisplay()},
		{"info", "", "", ""},
		{"label", "", t.tr("tui.config.local_files"), ""},
		{"info", "", "  " + t.tr("tui.config.config_path"), configFilePath()},
		{"info", "", "  " + t.tr("tui.config.credentials_path"), credentialsFilePath()},
		{"open_config_dir", "", "  " + t.tr("tui.config.open_config_folder"), configDataDir()},
	}
	t.ensureConfigCursor(rows)
	return rows
}

func (t *TUI) configModelRows() []configRow {
	models := t.configModelsSnapshot()
	var rows []configRow
	sort.Slice(models, func(i, j int) bool { return models[i].Ref() < models[j].Ref() })
	t.configModels = nil
	if len(models) == 0 {
		rows = append(rows, configRow{"add_model", "", t.tr("tui.config.add_first_model"), ""})
	}
	for _, mc := range models {
		ref := mc.Ref()
		t.configModels = append(t.configModels, ref)
		rows = append(rows, configRow{"model", ref, modelStatusMark(mc, t.isActiveModelRef(ref)) + " " + ref, t.modelSummary(mc)})
	}
	if len(models) > 0 {
		rows = append(rows, configRow{"add_model", "", "+ " + t.tr("tui.config.add_model"), ""})
	}
	t.ensureConfigCursor(rows)
	return rows
}

func (t *TUI) configDetailRows() []configRow {
	mc, ok := t.modelByRef(t.configDetailRef)
	if !ok {
		return []configRow{{"empty", "", t.tr("tui.config.model_not_found"), ""}}
	}
	status := t.tr("tui.config.deactivated_status")
	if t.isActiveModelRef(mc.Ref()) {
		status = t.tr("tui.config.activated_status")
	}
	apiKey := t.tr("tui.config.missing")
	if mc.HasAPIKey {
		apiKey = t.tr("tui.config.configured")
	}
	lastCheck := t.configLastCheck
	if lastCheck == "" {
		lastCheck = t.tr("tui.config.not_checked")
	}
	rows := []configRow{
		{"info", "", t.tr("tui.config.status"), status},
		{"info", "", t.tr("tui.config.provider.type"), mc.Provider},
		{"info", "", t.tr("tui.config.provider.endpoint"), t.displayEndpoint(mc.BaseURL)},
		{"info", "", t.tr("tui.config.provider.api_key"), apiKey},
		{"info", "", t.tr("tui.config.provider.model"), modelStatusMark(mc, t.isActiveModelRef(mc.Ref())) + " " + mc.Model},
		{"info", "", t.tr("tui.config.provider.context_window"), contextDisplay(mc)},
		{"info", "", t.tr("tui.config.last_check"), lastCheck},
		{"info", "", "", ""},
		{"edit_model", "", "  " + t.tr("tui.config.edit_model"), ""},
	}
	if !t.isActiveModelRef(mc.Ref()) {
		rows = append(rows, configRow{"activate_model", mc.Ref(), "  " + t.tr("tui.config.activate_model"), ""})
	}
	rows = append(rows,
		configRow{"check_model", "", "  " + t.tr("tui.config.check_model"), ""},
		configRow{"delete_model", "", "  " + t.tr("tui.config.delete_model"), ""},
	)
	return rows
}

func (t *TUI) handleConfigAction(rows []configRow) tea.Cmd {
	if t.configCursor < 0 || t.configCursor >= len(rows) {
		return nil
	}
	switch row := rows[t.configCursor]; row.kind {
	case "section":
		t.configPage = row.name
		t.configCursor = 0
	case "general_language":
		return t.toggleLanguage()
	case "general_theme":
		return t.toggleTheme()
	case "general_guard":
		return t.toggleGuardMode()
	case "open_config_dir":
		return t.openConfigDirCmd()
	case "add_model":
		t.openProviderKind()
	case "edit_model":
		if mc, ok := t.modelByRef(t.configDetailRef); ok {
			t.openProviderForm(t.configDetailRef, &mc)
			return t.configInputs[t.configInputFocus].Focus()
		}
	case "activate_model":
		return t.activateModelRef(row.name)
	case "check_model":
		t.configLastCheck = t.tr("tui.config.check_not_implemented")
	case "delete_model":
		if t.configDetailRef != "" {
			t.configDeleteConfirm = t.configDetailRef
			t.configDeleteCursor = 0
		}
	case "model":
		t.openConfigDetail(row.name)
	}
	return nil
}

func (t *TUI) openConfigDetail(ref string) {
	t.configDetailRef = ref
	t.configPage = "detail"
	t.configCursor = t.configDetailDefaultCursor()
}

func (t *TUI) configDetailDefaultCursor() int {
	preferred := "edit_model"
	if mc, ok := t.modelByRef(t.configDetailRef); ok && !t.modelNeedsAttention(mc) && !t.isActiveModelRef(mc.Ref()) {
		preferred = "activate_model"
	}
	for i, row := range t.configDetailRows() {
		if row.kind == preferred {
			return i
		}
	}
	return 0
}

func configDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".suna"
	}
	return filepath.Join(home, ".suna")
}

func configFilePath() string {
	return filepath.Join(configDataDir(), "config.toml")
}

func credentialsFilePath() string {
	return filepath.Join(configDataDir(), "credentials.toml")
}

func (t *TUI) openConfigDirCmd() tea.Cmd {
	return func() tea.Msg {
		if err := os.MkdirAll(configDataDir(), 0755); err != nil {
			return ipcErrorNotification("config.error", err)
		}
		if err := openDirectory(configDataDir()); err != nil {
			return ipcErrorNotification("config.error", err)
		}
		return nil
	}
}

func (t *TUI) activateSelectedConfigModel(rows []configRow) tea.Cmd {
	if t.configPage != "models" {
		return nil
	}
	if ref, ok := t.selectedConfigModel(rows); ok {
		return t.activateModelRef(ref)
	}
	return nil
}

func (t *TUI) activateModelRef(ref string) tea.Cmd {
	if mc, ok := t.modelByRef(ref); ok && t.modelNeedsAttention(mc) {
		t.openConfigDetail(ref)
		t.configError = t.tr("tui.error.provider_incomplete")
		return nil
	}
	t.configState.ActiveModel = ref
	if mc, ok := t.modelByRef(ref); ok {
		t.providerName = mc.Provider
		t.modelName = mc.Model
		t.contextWindow = defaultContextWindow(mc)
	}
	return t.sendConfigSet(protocol.ConfigSetParams{Action: protocol.ConfigActionActivateModel, ActiveModel: ref})
}

func (t *TUI) sendConfigSet(params protocol.ConfigSetParams) tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil || !t.localCli.Connected() {
			return localNotification{method: "config.error", params: []byte(fmt.Sprintf(`{"message":%q}`, t.tr("tui.error.daemon_not_connected")))}
		}
		if err := t.localCli.ConfigSet(params); err != nil {
			return localNotification{method: "config.error", params: []byte(fmt.Sprintf(`{"message":%q}`, err.Error()))}
		}
		return nil
	}
}

func (t *TUI) leaveConfig() tea.Cmd {
	if t.configSetupMode {
		t.configSetupMode = false
		t.configFormOpen = false
		t.configPage = "home"
		t.mode = "welcome"
		return nil
	}
	if t.configDeleteConfirm != "" {
		t.configDeleteConfirm = ""
		return nil
	}
	if t.configPage == "detail" {
		t.configPage = "models"
		t.configCursor = 0
		return nil
	}
	if t.configPage == "models" {
		t.configPage = "home"
		t.configCursor = 0
		return nil
	}
	if t.configFromMode != "" {
		t.mode = t.configFromMode
	} else {
		t.mode = "welcome"
	}
	return nil
}

func (t *TUI) toggleLanguage() tea.Cmd {
	locale := string(LocaleZH)
	if t.i18n.Locale() == LocaleZH {
		locale = string(LocaleEN)
	}
	t.i18n.SetLocale(LocaleID(locale))
	return t.sendConfigSet(protocol.ConfigSetParams{Action: protocol.ConfigActionUpdateGeneral, Locale: locale, Theme: t.theme})
}

func (t *TUI) toggleTheme() tea.Cmd {
	theme := nextTheme(t.theme)
	t.setTheme(theme)
	if t.mode == "chat" {
		t.syncContent()
	}
	return t.sendConfigSet(protocol.ConfigSetParams{Action: protocol.ConfigActionUpdateGeneral, Locale: string(t.i18n.Locale()), Theme: theme})
}

func (t *TUI) toggleGuardMode() tea.Cmd {
	mode := nextGuardMode(t.configState.GuardMode)
	t.configState.GuardMode = mode
	return t.sendConfigSet(protocol.ConfigSetParams{Action: protocol.ConfigActionUpdateGeneral, Locale: string(t.i18n.Locale()), Theme: t.theme, GuardMode: mode})
}

func (t *TUI) currentLangDisplay() string {
	if t.i18n.Locale() == LocaleZH {
		return t.tr("tui.lang.zh")
	}
	return t.tr("tui.lang.en")
}

func (t *TUI) guardModeDisplay() string {
	switch normalizeGuardMode(t.configState.GuardMode) {
	case "readonly":
		return t.tr("tui.guard.mode.readonly") + " · " + t.tr("tui.guard.mode.readonly.desc")
	case "auto":
		return t.tr("tui.guard.mode.auto") + " · " + t.tr("tui.guard.mode.auto.desc")
	case "smart":
		return t.tr("tui.guard.mode.smart") + " · " + t.tr("tui.guard.mode.smart.desc")
	default:
		return t.tr("tui.guard.mode.ask") + " · " + t.tr("tui.guard.mode.ask.desc")
	}
}

func normalizeGuardMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "readonly", "auto", "smart":
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return "ask"
	}
}

func nextGuardMode(mode string) string {
	switch normalizeGuardMode(mode) {
	case "ask":
		return "smart"
	case "smart":
		return "auto"
	case "auto":
		return "readonly"
	default:
		return "ask"
	}
}

func (t *TUI) ensureConfigCursor(rows []configRow) {
	if len(rows) == 0 {
		t.configCursor = 0
		return
	}
	if t.configCursor >= len(rows) {
		t.configCursor = len(rows) - 1
	}
	if t.configCursor < 0 {
		t.configCursor = 0
	}
	if !rows[t.configCursor].selectable() {
		t.moveConfigCursor(rows, 1)
	}
}

func (t *TUI) selectedConfigModel(rows []configRow) (string, bool) {
	if t.configCursor < 0 || t.configCursor >= len(rows) || rows[t.configCursor].kind != "model" {
		return "", false
	}
	return rows[t.configCursor].name, true
}

func (t *TUI) configModelsSnapshot() []tuiModelConfig {
	models := make([]tuiModelConfig, 0, len(t.configState.Models))
	for _, cm := range t.configState.Models {
		models = append(models, tuiModelConfig{Provider: cm.Provider, Model: cm.Model, BaseURL: cm.BaseURL, ContextWindow: cm.ContextWindow, Strengths: cm.Strengths, HasAPIKey: cm.HasAPIKey})
	}
	return models
}

func (t *TUI) modelByRef(ref string) (tuiModelConfig, bool) {
	for _, mc := range t.configModelsSnapshot() {
		if mc.Ref() == ref {
			return mc, true
		}
	}
	return tuiModelConfig{}, false
}

func (t *TUI) isActiveModelRef(ref string) bool {
	if ref == "" {
		return false
	}
	if t.configState.ActiveModel != "" {
		return ref == t.configState.ActiveModel
	}
	provider, model := t.activeProviderModel()
	return ref == provider+"/"+model
}

func (t *TUI) viewConfig() string {
	if t.configKindOpen {
		base := t.viewConfigPage()
		return overlayBlock(base, t.viewProviderKind())
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
	if help := t.configHelp(); help != "" {
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

func (t *TUI) configHelp() string {
	if t.configDeleteConfirm != "" {
		return ""
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
	cancel := t.configConfirmButton(0, t.tr("tui.config.cancel"))
	delete := t.configConfirmButton(1, t.tr("tui.config.delete_model"))
	body := message + "\n\n" + cancel + "  " + delete + "\n" + styleDim.Render(t.tr("tui.config.delete_help"))
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

func splitCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func parsePositiveInt(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	if n < 0 {
		return 0
	}
	return n
}

func modelSummary(mc tuiModelConfig, active bool) string {
	var parts []string
	if active {
		parts = append(parts, "active")
	}
	if !mc.HasAPIKey {
		parts = append(parts, "missing_api_key")
	} else if mc.Ref() == "" {
		parts = append(parts, "invalid")
	}
	parts = append(parts, mc.Provider, mc.Model)
	if mc.ContextWindow > 0 {
		parts = append(parts, "ctx "+fmtTok(mc.ContextWindow))
	}
	if mc.BaseURL != "" {
		parts = append(parts, "endpoint_configured")
	}
	if len(mc.Strengths) > 0 {
		parts = append(parts, strings.Join(mc.Strengths, ", "))
	}
	return strings.Join(parts, " · ")
}

func (t *TUI) modelSummary(mc tuiModelConfig) string {
	raw := modelSummary(mc, t.isActiveModelRef(mc.Ref()))
	parts := strings.Split(raw, " · ")
	for i, part := range parts {
		switch part {
		case "active":
			parts[i] = t.tr("tui.config.activated_status")
		case "missing_api_key":
			parts[i] = t.tr("tui.config.missing_api_key")
		case "invalid":
			parts[i] = t.tr("tui.config.invalid")
		case "endpoint_configured":
			parts[i] = t.tr("tui.config.endpoint_configured")
		}
	}
	return strings.Join(parts, " · ")
}

func modelStatusMark(mc tuiModelConfig, active bool) string {
	if !mc.HasAPIKey || mc.Model == "" || (mc.Provider != "openai" && mc.Provider != "anthropic" && mc.BaseURL == "") {
		return "!"
	}
	if active {
		return "◉"
	}
	return "○"
}

func (t *TUI) modelNeedsAttention(mc tuiModelConfig) bool {
	return !mc.HasAPIKey || mc.Model == "" || (mc.Provider != "openai" && mc.Provider != "anthropic" && mc.BaseURL == "")
}

func (t *TUI) displayEndpoint(endpoint string) string {
	if endpoint == "" {
		return t.tr("tui.config.endpoint_default")
	}
	return endpoint
}

func contextDisplay(mc tuiModelConfig) string {
	return fmtTok(defaultContextWindow(mc))
}
