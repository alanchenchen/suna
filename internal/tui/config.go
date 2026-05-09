package tui

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/ipc"
)

type providerFormValues struct {
	Provider  string
	Model     string
	APIKey    string
	Endpoint  string
	Strengths string
}

func (t *TUI) updateConfig(msg tea.Msg) (tea.Model, tea.Cmd) {
	if t.configFormOpen {
		return t.updateProviderForm(msg)
	}
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		t.width, t.height, t.ready = m.Width, m.Height, true
		return t, nil
	case tea.KeyPressMsg:
		rows := t.configRows()
		switch m.String() {
		case "ctrl+c":
			t.doQuit()
			return t, tea.Quit
		case "esc":
			return t, t.leaveConfig()
		case "up", "k":
			if t.configCursor > 0 {
				t.configCursor--
			}
			return t, nil
		case "down", "j":
			if t.configCursor < len(rows)-1 {
				t.configCursor++
			}
			return t, nil
		case "enter":
			return t, t.handleConfigAction(rows)
		case "a", "A":
			t.openProviderForm("", nil)
			return t, t.configInputs[t.configInputFocus].Focus()
		case "e", "E":
			if ref, ok := t.selectedConfigModel(rows); ok {
				cfg := t.configSnapshot()
				if cfg != nil {
					if mc, ok := cfg.ModelByRef(ref); ok {
						t.openProviderForm(ref, &mc)
						return t, t.configInputs[t.configInputFocus].Focus()
					}
				}
			}
		case "l":
			return t, t.toggleLanguage()
		case "?", "f1":
			t.prevMode = "config"
			t.mode = "help"
			t.initHelpPage()
			return t, nil
		}
	}
	return t, nil
}

func (t *TUI) updateProviderForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		t.width, t.height, t.ready = m.Width, m.Height, true
		return t, nil
	case tea.KeyPressMsg:
		t.configError = ""
		switch m.String() {
		case "ctrl+c":
			t.doQuit()
			return t, tea.Quit
		case "esc":
			if t.configSetupMode {
				return t, nil
			}
			t.configFormOpen = false
			return t, nil
		case "enter":
			if t.configInputFocus == len(t.configInputs)-1 {
				return t, t.saveProviderForm()
			}
			return t, t.focusConfigInput(t.configInputFocus + 1)
		case "shift+tab", "up":
			return t, t.focusConfigInput(max(0, t.configInputFocus-1))
		case "tab", "down":
			return t, t.focusConfigInput(min(len(t.configInputs)-1, t.configInputFocus+1))
		}
	}
	var cmd tea.Cmd
	t.configInputs[t.configInputFocus], cmd = t.configInputs[t.configInputFocus].Update(msg)
	return t, cmd
}

func (t *TUI) openProviderForm(ref string, mc *config.ModelConfig) {
	t.configFormOpen = true
	t.configFormTitle = "tui.config.provider.edit"
	t.configEditingName = ref
	if ref == "" {
		t.configFormTitle = "tui.config.provider.add"
	}
	t.initProviderForm(mc)
}

func (t *TUI) initProviderForm(mc *config.ModelConfig) {
	labels := []string{t.tr("tui.config.provider.type"), t.tr("tui.config.provider.model"), t.tr("tui.config.provider.api_key"), t.tr("tui.config.provider.endpoint"), t.tr("tui.config.provider.strengths")}
	placeholders := []string{"glm", "glm-4", "sk-...", "https://open.bigmodel.cn/api/paas/v4", "Go, 后端, 通用"}
	values := []string{"", "", "", "", ""}
	if mc != nil {
		values[0] = mc.Provider
		values[1] = mc.Model
		values[3] = mc.BaseURL
		values[4] = strings.Join(mc.Strengths, ", ")
	}
	t.configInputs = make([]textinput.Model, len(labels))
	for i := range labels {
		in := textinput.New()
		in.Prompt = labels[i] + ": "
		in.Placeholder = placeholders[i]
		in.SetValue(values[i])
		in.SetWidth(46)
		if i == 2 {
			in.EchoMode = textinput.EchoPassword
			in.EchoCharacter = '*'
		}
		styles := textinput.DefaultStyles(false)
		styles.Focused.Prompt = lipgloss.NewStyle().Foreground(ColorBrand).Bold(true)
		styles.Blurred.Prompt = lipgloss.NewStyle().Foreground(ColorDim)
		in.SetStyles(styles)
		t.configInputs[i] = in
	}
	t.configInputFocus = 0
	t.focusConfigInput(0)
}

func (t *TUI) focusConfigInput(idx int) tea.Cmd {
	if idx < 0 || idx >= len(t.configInputs) {
		return nil
	}
	var cmds []tea.Cmd
	for i := range t.configInputs {
		if i == idx {
			cmds = append(cmds, t.configInputs[i].Focus())
		} else {
			t.configInputs[i].Blur()
		}
	}
	t.configInputFocus = idx
	return tea.Batch(cmds...)
}

func (t *TUI) saveProviderForm() tea.Cmd {
	v := t.providerFormValues()
	if err := t.validateProviderForm(v); err != nil {
		t.configError = err.Error()
		return nil
	}
	cfg, err := loadOrNewConfig(t.cfgPath)
	if err != nil {
		t.configError = err.Error()
		return nil
	}
	newModel := config.ModelConfig{Provider: v.Provider, Model: v.Model, BaseURL: v.Endpoint, Strengths: splitCSV(v.Strengths)}
	newRef := newModel.Ref()
	updated := false
	for i, mc := range cfg.Models {
		if mc.Ref() == t.configEditingName || mc.Ref() == newRef {
			cfg.Models[i] = newModel
			updated = true
			break
		}
	}
	if !updated {
		cfg.Models = append(cfg.Models, newModel)
	}
	if cfg.ActiveModel == "" || t.configSetupMode || cfg.ActiveModel == t.configEditingName {
		cfg.ActiveModel = newRef
	}
	cfg.Locale = string(t.i18n.Locale())
	if cfg.TUI.Theme == "" {
		cfg.TUI.Theme = "dark"
	}
	if err := cfg.EnsureDataDir(); err != nil {
		t.configError = err.Error()
		return nil
	}
	if err := cfg.Save(t.cfgPath); err != nil {
		t.configError = err.Error()
		return nil
	}
	if v.APIKey != "" {
		if err := config.SaveCredential(cfg.DataDir, v.Provider, v.APIKey); err != nil {
			t.configError = err.Error()
			return nil
		}
	}
	t.configState = configParamsFromConfig(cfg)
	t.providerName, t.modelName = v.Provider, v.Model
	t.configFormOpen = false
	t.configEditingName = ""
	if t.configSetupMode {
		t.configSetupMode = false
		t.mode = "welcome"
	}
	return nil
}

func (t *TUI) providerFormValues() providerFormValues {
	vals := make([]string, 5)
	for i := range vals {
		if i < len(t.configInputs) {
			vals[i] = strings.TrimSpace(t.configInputs[i].Value())
		}
	}
	return providerFormValues{Provider: vals[0], Model: vals[1], APIKey: vals[2], Endpoint: vals[3], Strengths: vals[4]}
}

func (t *TUI) validateProviderForm(v providerFormValues) error {
	if v.Provider == "" || v.Model == "" {
		return fmt.Errorf("%s", t.tr("tui.error.required"))
	}
	if v.APIKey == "" && t.configSetupMode {
		return fmt.Errorf("%s", t.tr("tui.error.required"))
	}
	if v.Endpoint != "" {
		u, err := url.Parse(v.Endpoint)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("%s", t.tr("tui.error.invalid_endpoint"))
		}
	}
	if v.Provider != "openai" && v.Provider != "anthropic" && v.Endpoint == "" {
		return fmt.Errorf("%s", t.tr("tui.error.endpoint_required"))
	}
	return nil
}

func loadConfigFile(path string) (*config.Config, error) { return config.Load(path) }

func loadOrNewConfig(path string) (*config.Config, error) {
	cfg, err := config.Load(path)
	if err == nil {
		return cfg, nil
	}
	homeDir, _ := os.UserHomeDir()
	return &config.Config{TUI: config.TUIConfig{Theme: "dark"}, Locale: "zh", DataDir: filepath.Join(homeDir, ".suna")}, nil
}

type configRow struct{ kind, name, label, value string }

func (t *TUI) configRows() []configRow {
	cfg := t.configSnapshot()
	var rows []configRow
	if cfg != nil {
		models := append([]config.ModelConfig(nil), cfg.Models...)
		sort.Slice(models, func(i, j int) bool { return models[i].Ref() < models[j].Ref() })
		t.configModels = nil
		for _, mc := range models {
			ref := mc.Ref()
			t.configModels = append(t.configModels, ref)
			mark := "○"
			if ref == cfg.ActiveModel {
				mark = "◉"
			}
			rows = append(rows, configRow{"model", ref, mark + " " + ref, modelSummary(mc)})
		}
	}
	rows = append(rows, configRow{"language", "", "▸ " + t.tr("tui.config.general.section"), t.currentLangDisplay()})
	rows = append(rows, configRow{"theme", "", t.tr("tui.config.theme"), "Default"})
	rows = append(rows, configRow{"back", "", "← " + t.tr("tui.key.back"), ""})
	if t.configCursor >= len(rows) {
		t.configCursor = len(rows) - 1
	}
	return rows
}

func (t *TUI) configSnapshot() *config.Config {
	if len(t.configState.Models) > 0 {
		cfg := &config.Config{ActiveModel: t.configState.ActiveModel, Locale: t.configState.Locale, TUI: config.TUIConfig{Theme: t.configState.Theme}}
		for _, cm := range t.configState.Models {
			cfg.Models = append(cfg.Models, config.ModelConfig{Provider: cm.Provider, Model: cm.Model, BaseURL: cm.BaseURL, Strengths: cm.Strengths})
		}
		return cfg
	}
	cfg, _ := loadConfigFile(t.cfgPath)
	return cfg
}

func (t *TUI) selectedConfigModel(rows []configRow) (string, bool) {
	if t.configCursor < 0 || t.configCursor >= len(rows) || rows[t.configCursor].kind != "model" {
		return "", false
	}
	return rows[t.configCursor].name, true
}

func (t *TUI) handleConfigAction(rows []configRow) tea.Cmd {
	if t.configCursor < 0 || t.configCursor >= len(rows) {
		return nil
	}
	row := rows[t.configCursor]
	switch row.kind {
	case "model":
		return t.activateModel(row.name)
	case "language":
		return t.toggleLanguage()
	case "back":
		return t.leaveConfig()
	}
	return nil
}

func (t *TUI) activateModel(ref string) tea.Cmd {
	cfg, err := loadOrNewConfig(t.cfgPath)
	if err != nil || cfg == nil {
		return nil
	}
	if _, ok := cfg.ModelByRef(ref); !ok {
		return nil
	}
	cfg.ActiveModel = ref
	cfg.Save(t.cfgPath)
	t.configState = configParamsFromConfig(cfg)
	if mc, ok := cfg.ModelByRef(ref); ok {
		t.providerName, t.modelName = mc.Provider, mc.Model
	}
	return nil
}

func (t *TUI) leaveConfig() tea.Cmd {
	if t.configSetupMode {
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
	if t.i18n.Locale() == LocaleZH {
		t.i18n.SetLocale(LocaleEN)
	} else {
		t.i18n.SetLocale(LocaleZH)
	}
	return nil
}

func (t *TUI) currentLangDisplay() string {
	if t.i18n.Locale() == LocaleZH {
		return "中文"
	}
	return "English"
}

func (t *TUI) viewConfig() string {
	if t.configFormOpen {
		return t.viewProviderForm()
	}
	rows := t.configRows()
	var sb strings.Builder
	sb.WriteString(renderHeader(t.tr("tui.config.title"), "[Esc] "+t.tr("tui.key.back"), t.width))
	sb.WriteString("\n\n")
	for i, row := range rows {
		t.renderConfigRow(&sb, i, row.label, row.value)
		if row.kind == "model" && i == len(t.configModels)-1 {
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\n" + styleDim.Render("  "+t.tr("tui.config.help")) + "\n")
	return sb.String()
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
	lines = append(lines, "", styleDim.Render("Enter "+t.tr("tui.key.send")+" · Esc "+t.tr("tui.key.back")))
	body := strings.Join(lines, "\n")
	w := min(max(48, t.width-8), 72)
	return "\n" + styleHL.Render("  "+title) + "\n\n  " + boxStyle.Width(w).Padding(1, 2).Render(body) + "\n"
}

func (t *TUI) renderConfigRow(sb *strings.Builder, idx int, label, value string) {
	cursor := "    "
	st := lipgloss.NewStyle()
	if t.configCursor == idx {
		cursor = styleCursor.Render("  ▶ ")
		st = styleHL
	}
	sb.WriteString(cursor + st.Render(label))
	if value != "" {
		sb.WriteString(styleDim.Render("  ") + value)
	}
	sb.WriteString("\n")
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

func modelSummary(mc config.ModelConfig) string {
	parts := []string{mc.Model}
	if mc.BaseURL != "" {
		parts = append(parts, "endpoint: "+mc.BaseURL)
	}
	if len(mc.Strengths) > 0 {
		parts = append(parts, strings.Join(mc.Strengths, ", "))
	}
	return strings.Join(parts, " · ")
}

func configParamsFromConfig(cfg *config.Config) ipc.ConfigParams {
	out := ipc.ConfigParams{ActiveModel: cfg.ActiveModel, Locale: cfg.Locale, Theme: cfg.TUI.Theme}
	for _, mc := range cfg.Models {
		out.Models = append(out.Models, ipc.ConfigModel{Provider: mc.Provider, Model: mc.Model, BaseURL: mc.BaseURL, Strengths: mc.Strengths, HasAPIKey: mc.APIKey != ""})
	}
	return out
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func renderHeader(title, right string, width int) string {
	if width <= 0 {
		width = 80
	}
	left := "  " + styleHL.Render(title)
	r := styleDim.Render(right)
	pad := max(1, width-lipgloss.Width(left)-lipgloss.Width(r)-2)
	return left + strings.Repeat(" ", pad) + r + "\n" + styleDim.Render(strings.Repeat("─", width))
}
