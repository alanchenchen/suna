package tui

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/protocol"
	tuiconfig "github.com/alanchenchen/suna/internal/tui/pages/config"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
)

func (t *TUI) updateConfig(msg tea.Msg) (tea.Model, tea.Cmd) {
	if t.config.ReasoningOpen {
		return t.updateReasoning(msg)
	}
	if t.config.KindOpen {
		return t.updateProviderKind(msg)
	}
	if t.config.WorkspaceOpen {
		return t.updateWorkspaceForm(msg)
	}
	if t.config.FormOpen {
		return t.updateProviderForm(msg)
	}
	if t.config.Page == "" {
		t.config.Page = "home"
	}
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		t.width, t.height, t.ready = m.Width, m.Height, true
		return t, nil
	case tea.KeyPressMsg:
		t.config.Notice = ""
		if t.config.SetupMode && !t.config.FormOpen && len(t.configState.Models) == 0 {
			t.openProviderForm("", nil)
			return t, t.config.Inputs[t.config.InputFocus].Focus()
		}
		if t.config.DeleteConfirm != "" {
			switch m.String() {
			case "ctrl+c":
				t.doQuit()
				return t, tea.Quit
			case "left", "right":
				options := t.configDeleteOptions()
				if len(options) == 0 {
					return t, nil
				}
				delta := 1
				if m.String() == "left" {
					delta = -1
				}
				t.config.MoveDeleteCursor(delta, len(options))
				return t, nil
			case "enter":
				ref, deleteAPIKey, ok := t.config.ConfirmDelete(t.shouldOfferDeleteAPIKey(t.config.DeleteConfirm))
				if !ok {
					return t, nil
				}
				return t, t.sendConfigSet(protocol.ConfigSetParams{Action: protocol.ConfigActionDeleteModel, ModelRef: ref, DeleteAPIKey: deleteAPIKey})
			case "esc":
				t.config.CancelDelete()
				return t, nil
			}
		}
		rows := t.configRows()
		switch m.String() {
		case "ctrl+c":
			t.doQuit()
			return t, tea.Quit
		case "esc":
			return t, t.leaveConfig()
		case "up":
			t.moveConfigCursor(rows, -1)
			return t, nil
		case "down":
			t.moveConfigCursor(rows, 1)
			return t, nil
		case "enter":
			return t, t.handleConfigAction(rows)
		case " ", "space":
			return t, t.activateSelectedConfigModel(rows)
		case "?":
			t.showHelp = !t.showHelp
			return t, nil
		}
	}
	return t, nil
}

func (t *TUI) moveConfigCursor(rows []tuiconfig.Row, delta int) {
	t.config.MoveCursor(rows, delta)
}

// tuiconfig.Row 是配置页列表的最小渲染/交互单元；配置持久化始终由 daemon 处理。
func (t *TUI) hasConfiguredModel() bool {
	if len(t.configState.Models) > 0 {
		return true
	}
	return t.providerName != "" && t.modelName != ""
}

func (t *TUI) activeProviderModel() (string, string) {
	if mc, ok := t.activeConfigModel(); ok {
		return mc.Provider, mc.Model
	}
	provider, model := t.providerName, t.modelName
	if t.daemonStatus.Provider != "" {
		provider = t.daemonStatus.Provider
	}
	if t.daemonStatus.Model != "" {
		model = t.daemonStatus.Model
	}
	if provider == "" && model == "" && len(t.configState.Models) > 0 {
		cm := t.configState.Models[0]
		return cm.Provider, cm.Model
	}
	return provider, model
}

func (t *TUI) activeConfigModel() (tuiconfig.ModelConfig, bool) {
	return tuiconfig.ActiveModel(t.configModelsSnapshot(), tuiconfig.ActiveModelRef(
		t.configState,
		t.providerName,
		t.modelName,
		t.daemonStatus.Provider,
		t.daemonStatus.Model,
	))
}

func (t *TUI) configModelsSnapshot() []tuiconfig.ModelConfig {
	return tuiconfig.SnapshotFromProtocol(t.configState)
}

func (t *TUI) modelByRef(ref string) (tuiconfig.ModelConfig, bool) {
	for _, mc := range t.configModelsSnapshot() {
		if mc.Ref() == ref {
			return mc, true
		}
	}
	return tuiconfig.ModelConfig{}, false
}

func (t *TUI) updateConfigModelReasoning(ref string, reasoning map[string]any) bool {
	for i, mc := range t.configState.Models {
		if mc.Provider+"/"+mc.Model == ref {
			t.configState.Models[i].Reasoning = reasoning
			return true
		}
	}
	return false
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

func (t *TUI) shouldOfferDeleteAPIKey(ref string) bool {
	mc, ok := t.modelByRef(ref)
	if !ok || !mc.HasAPIKey {
		return false
	}
	count := 0
	for _, existing := range t.configModelsSnapshot() {
		if existing.Provider == mc.Provider {
			count++
		}
	}
	return count == 1
}

func (t *TUI) configDeleteOptions() []string {
	options := tuiconfig.DeleteOptions(
		t.tr("tui.config.cancel"),
		t.tr("tui.config.delete_model"),
		t.tr("tui.config.delete_model_and_api_key"),
		t.shouldOfferDeleteAPIKey(t.config.DeleteConfirm),
	)
	t.config.ClampDeleteCursor(len(options))
	return options
}

func (t *TUI) currentLangDisplay() string {
	if t.i18n.Locale() == LocaleZH {
		return t.tr("tui.lang.zh")
	}
	return t.tr("tui.lang.en")
}

func (t *TUI) guardModeDisplay() string {
	switch tuiconfig.NormalizeGuardMode(t.configState.GuardMode) {
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

func (t *TUI) workspaceDisplay() string {
	workspace := strings.TrimSpace(t.configState.Workspace)
	if workspace == "" {
		return t.tr("tui.config.disabled")
	}
	return workspace
}

func (t *TUI) attachmentUsageDisplay() string {
	return fmt.Sprintf("%s / %d files", formatAttachmentSize(t.attachmentStatus.Bytes), t.attachmentStatus.Count)
}

func (t *TUI) displayEndpoint(endpoint string) string {
	if endpoint == "" {
		return t.tr("tui.config.missing")
	}
	return endpoint
}

func contextDisplay(mc tuiconfig.ModelConfig) string {
	return fmtTok(mc.ContextWindow)
}

func maxOutputDisplay(mc tuiconfig.ModelConfig) string {
	return fmtTok(mc.MaxOutputTokens)
}

func (t *TUI) modelNeedsAttention(mc tuiconfig.ModelConfig) bool {
	return tuiconfig.ModelNeedsAttention(mc)
}

func configDataDir() string {
	return config.DefaultDataDir()
}

func configFilePath() string {
	return config.DataDirConfigPath(configDataDir())
}

func credentialsFilePath() string {
	return config.DataDirCredentialsPath(configDataDir())
}

func (t *TUI) modelSummary(mc tuiconfig.ModelConfig) string {
	raw := tuiconfig.ModelSummary(mc, t.isActiveModelRef(mc.Ref()), fmtTok)
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

func (t *TUI) configRows() []tuiconfig.Row {
	return t.config.Rows(t.configRowsDeps())
}

func (t *TUI) configRowsDeps() tuiconfig.RowsDeps {
	return tuiconfig.RowsDeps{
		Tr:               func(key string) string { return t.tr(key) },
		ProvidersSummary: func(total, needs int) string { return t.i18n.Tf("tui.config.providers_summary", total, needs) },
		Models:           t.configModelsSnapshot(),
		ActiveModel:      t.configState.ActiveModel,
		IsActive:         t.isActiveModelRef,
		NeedsAttention:   t.modelNeedsAttention,
		ModelSummary:     t.modelSummary,
		CurrentLanguage:  t.currentLangDisplay(),
		Theme:            t.themeDisplay(),
		GuardMode:        t.guardModeDisplay(),
		Workspace:        t.workspaceDisplay(),
		ConfigPath:       configFilePath(),
		CredentialsPath:  credentialsFilePath(),
		AttachmentUsage:  t.attachmentUsageDisplay(),
		ConfigDir:        configDataDir(),
		DisplayEndpoint:  t.displayEndpoint,
		ContextDisplay:   contextDisplay,
		MaxOutputDisplay: maxOutputDisplay,
		ReasoningDisplay: t.reasoningDisplay,
	}
}

func (t *TUI) configHomeRows() []tuiconfig.Row {
	return t.config.HomeRows(t.configRowsDeps())
}

func (t *TUI) configModelRows() []tuiconfig.Row {
	return t.config.ModelRows(t.configRowsDeps())
}

func (t *TUI) configDetailRows() []tuiconfig.Row {
	return t.config.DetailRows(t.configRowsDeps())
}

func (t *TUI) ensureConfigCursor(rows []tuiconfig.Row) {
	t.config.EnsureCursor(rows)
}

func (t *TUI) selectedConfigModel(rows []tuiconfig.Row) (string, bool) {
	return t.config.SelectedModel(rows)
}

func (t *TUI) handleConfigAction(rows []tuiconfig.Row) tea.Cmd {
	if t.config.Cursor < 0 || t.config.Cursor >= len(rows) {
		return nil
	}
	switch row := rows[t.config.Cursor]; row.Kind {
	case "section":
		t.config.Page = row.Name
		t.config.Cursor = 0
	case "general_language":
		return t.toggleLanguage()
	case "general_theme":
		return t.toggleTheme()
	case "general_guard":
		return t.toggleGuardMode()
	case "general_workspace":
		return t.openWorkspaceForm()
	case "clear_attachments":
		t.chat.Attachments = nil
		return t.attachmentClearCmd()
	case "open_config_dir":
		return t.openConfigDirCmd()
	case "add_model":
		t.openProviderKind()
	case "edit_model":
		if mc, ok := t.modelByRef(t.config.DetailRef); ok {
			t.openProviderForm(t.config.DetailRef, &mc)
			return t.config.Inputs[t.config.InputFocus].Focus()
		}
	case "edit_reasoning":
		if mc, ok := t.modelByRef(t.config.DetailRef); ok {
			t.openReasoning(mc)
		}
	case "activate_model":
		return t.activateModelRef(row.Name)
	case "delete_model":
		t.config.BeginDelete(t.config.DetailRef)
	case "model":
		t.openConfigDetail(row.Name)
	}
	return nil
}
func (t *TUI) openConfigDirCmd() tea.Cmd {
	return func() tea.Msg {
		if err := os.MkdirAll(configDataDir(), 0755); err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		if err := openDirectory(configDataDir()); err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		return nil
	}
}
func (t *TUI) activateSelectedConfigModel(rows []tuiconfig.Row) tea.Cmd {
	if t.config.Page != "models" {
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
		t.config.Error = t.tr("tui.error.provider_incomplete")
		return nil
	}
	t.setActiveModelRef(ref)
	return t.sendConfigSet(protocol.ConfigSetParams{Action: protocol.ConfigActionActivateModel, ActiveModel: ref})
}
func (t *TUI) setActiveModelRef(ref string) {
	t.configState.ActiveModel = ref
	if mc, ok := t.modelByRef(ref); ok {
		t.providerName = mc.Provider
		t.modelName = mc.Model
		t.contextWindow = mc.ContextWindow
	}
}
func (t *TUI) sendConfigSet(params protocol.ConfigSetParams) tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil || !t.localCli.Connected() {
			return localNotification{method: notifyConfigError, params: []byte(fmt.Sprintf(`{"message":%q}`, t.tr("tui.error.daemon_not_connected")))}
		}
		if err := t.localCli.ConfigSet(params); err != nil {
			return localNotification{method: notifyConfigError, params: []byte(fmt.Sprintf(`{"message":%q}`, err.Error()))}
		}
		return nil
	}
}
func (t *TUI) leaveConfig() tea.Cmd {
	target := t.config.LeaveTarget()
	if target != uipage.None {
		t.mode = target
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
	if t.mode == uipage.Chat {
		t.syncContent()
	}
	return t.sendConfigSet(protocol.ConfigSetParams{Action: protocol.ConfigActionUpdateGeneral, Locale: string(t.i18n.Locale()), Theme: theme})
}
func (t *TUI) toggleGuardMode() tea.Cmd {
	mode := tuiconfig.NextGuardMode(t.configState.GuardMode)
	t.configState.GuardMode = mode
	return t.sendConfigSet(protocol.ConfigSetParams{Action: protocol.ConfigActionUpdateGeneral, Locale: string(t.i18n.Locale()), Theme: t.theme, GuardMode: mode})
}

func (t *TUI) openConfigDetail(ref string) {
	t.config.OpenDetail(ref, t.configDetailDefaultCursor(ref))
}
func (t *TUI) openConfigDetailIfPresent(ref string) bool {
	if ref == "" {
		return false
	}
	if _, ok := t.modelByRef(ref); !ok {
		return false
	}
	t.openConfigDetail(ref)
	return true
}

func (t *TUI) returnToConfigModels() {
	t.config.ReturnToModels(t.configModelCursorForActive())
}
func (t *TUI) configProviderFormRef() string {
	return tuiconfig.ProviderFormRef(t.providerFormValues())
}
func (t *TUI) configModelCursorForActive() int {
	active := t.configState.ActiveModel
	if active == "" {
		provider, model := t.activeProviderModel()
		if provider != "" && model != "" {
			active = provider + "/" + model
		}
	}
	return tuiconfig.ModelCursorForActive(t.configModelRows(), active)
}
func (t *TUI) configDetailDefaultCursor(ref string) int {
	preferred := "edit_model"
	if mc, ok := t.modelByRef(ref); ok && !t.modelNeedsAttention(mc) && !t.isActiveModelRef(mc.Ref()) {
		preferred = "activate_model"
	}
	return tuiconfig.DetailDefaultCursor(t.configDetailRowsForRef(ref), preferred)
}

func (t *TUI) configDetailRowsForRef(ref string) []tuiconfig.Row {
	old := t.config.DetailRef
	t.config.DetailRef = ref
	rows := t.config.DetailRows(t.configRowsDeps())
	t.config.DetailRef = old
	return rows
}
