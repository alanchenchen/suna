package tui

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/alanchenchen/suna/internal/protocol"
)

type providerFormValues struct {
	Provider      string
	Model         string
	APIKey        string
	Endpoint      string
	ContextWindow string
	Strengths     string
}

type tuiModelConfig struct {
	Provider      string
	Model         string
	BaseURL       string
	ContextWindow int
	Strengths     []string
	HasAPIKey     bool
}

func (m tuiModelConfig) Ref() string { return m.Provider + "/" + m.Model }

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

func (t *TUI) activeConfigModel() (tuiModelConfig, bool) {
	active := t.configState.ActiveModel
	if active == "" {
		provider, model := t.providerName, t.modelName
		if t.daemonStatus.Provider != "" {
			provider = t.daemonStatus.Provider
		}
		if t.daemonStatus.Model != "" {
			model = t.daemonStatus.Model
		}
		if provider != "" && model != "" {
			active = provider + "/" + model
		}
	}
	for _, mc := range t.configModelsSnapshot() {
		if mc.Ref() == active {
			return mc, true
		}
	}
	return tuiModelConfig{}, false
}

func defaultContextWindow(mc tuiModelConfig) int {
	if mc.ContextWindow > 0 {
		return mc.ContextWindow
	}
	switch mc.Provider {
	case "anthropic":
		return 200000
	default:
		return 128000
	}
}

func (t *TUI) updateConfig(msg tea.Msg) (tea.Model, tea.Cmd) {
	if t.configKindOpen {
		return t.updateProviderKind(msg)
	}
	if t.configWorkspaceOpen {
		return t.updateWorkspaceForm(msg)
	}
	if t.configFormOpen {
		return t.updateProviderForm(msg)
	}
	if t.configPage == "" {
		t.configPage = "home"
	}
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		t.width, t.height, t.ready = m.Width, m.Height, true
		return t, nil
	case tea.KeyPressMsg:
		if t.configSetupMode && !t.configFormOpen && len(t.configState.Models) == 0 {
			t.openProviderForm("", nil)
			return t, t.configInputs[t.configInputFocus].Focus()
		}
		if t.configDeleteConfirm != "" {
			switch m.String() {
			case "ctrl+c":
				t.doQuit()
				return t, tea.Quit
			case "left", "h", "up", "k", "tab", "shift+tab", "right", "l", "down", "j":
				if t.configDeleteCursor == 0 {
					t.configDeleteCursor = 1
				} else {
					t.configDeleteCursor = 0
				}
				return t, nil
			case "enter":
				if t.configDeleteCursor == 0 {
					t.configDeleteConfirm = ""
					t.configDeleteCursor = 0
					return t, nil
				}
				ref := t.configDeleteConfirm
				t.configDeleteConfirm = ""
				t.configDeleteCursor = 0
				return t, t.sendConfigSet(protocol.ConfigSetParams{Action: protocol.ConfigActionDeleteModel, ModelRef: ref})
			case "esc":
				t.configDeleteConfirm = ""
				t.configDeleteCursor = 0
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
		case "up", "k":
			t.moveConfigCursor(rows, -1)
			return t, nil
		case "down", "j":
			t.moveConfigCursor(rows, 1)
			return t, nil
		case "enter":
			return t, t.handleConfigAction(rows)
		case " ", "space":
			return t, t.activateSelectedConfigModel(rows)
		case "?", "f1":
			t.showHelp = !t.showHelp
			return t, nil
		}
	}
	return t, nil
}

func (t *TUI) openProviderKind() {
	t.configKindOpen = true
	t.configKindCursor = 0
	t.configProviderKind = "openai-compatible"
}

func (t *TUI) providerKindOptions() []string {
	return []string{"openai-compatible", "openai", "anthropic"}
}

func (t *TUI) updateProviderKind(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		options := t.providerKindOptions()
		switch m.String() {
		case "ctrl+c":
			t.doQuit()
			return t, tea.Quit
		case "esc":
			t.configKindOpen = false
			return t, nil
		case "up", "k":
			if t.configKindCursor > 0 {
				t.configKindCursor--
			}
			return t, nil
		case "down", "j":
			if t.configKindCursor < len(options)-1 {
				t.configKindCursor++
			}
			return t, nil
		case "enter":
			t.configProviderKind = options[t.configKindCursor]
			t.configKindOpen = false
			t.openProviderForm("", nil)
			return t, t.configInputs[t.configInputFocus].Focus()
		}
	}
	return t, nil
}

func (t *TUI) moveConfigCursor(rows []configRow, delta int) {
	if len(rows) == 0 {
		t.configCursor = 0
		return
	}
	idx := t.configCursor
	for step := 0; step < len(rows); step++ {
		idx += delta
		if idx < 0 {
			idx = len(rows) - 1
		}
		if idx >= len(rows) {
			idx = 0
		}
		if rows[idx].selectable() {
			t.configCursor = idx
			return
		}
	}
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
				t.configFormOpen = false
				t.mode = "welcome"
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

func (t *TUI) openProviderForm(ref string, mc *tuiModelConfig) {
	t.configWorkspaceOpen = false
	t.configFormOpen = true
	t.configFormTitle = "tui.config.provider.edit"
	t.configEditingName = ref
	if ref == "" {
		t.configFormTitle = "tui.config.provider.add"
	}
	t.initProviderForm(mc)
}

func (t *TUI) openWorkspaceForm() tea.Cmd {
	t.configWorkspaceOpen = true
	t.configFormOpen = true
	t.configFormTitle = "tui.config.workspace.edit"
	t.configEditingName = ""
	t.initWorkspaceForm()
	return t.configInputs[t.configInputFocus].Focus()
}

func (t *TUI) initWorkspaceForm() {
	in := textinput.New()
	in.Prompt = t.tr("tui.config.workspace") + ": "
	in.Placeholder = t.tr("tui.config.workspace.placeholder")
	in.SetValue(t.configState.Workspace)
	in.SetWidth(64)
	styles := textinput.DefaultStyles(false)
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(ColorBrand).Bold(true)
	styles.Blurred.Prompt = lipgloss.NewStyle().Foreground(ColorDim)
	in.SetStyles(styles)
	t.configInputs = []textinput.Model{in}
	t.configInputFocus = 0
	t.focusConfigInput(0)
}

func (t *TUI) updateWorkspaceForm(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			t.configWorkspaceOpen = false
			t.configFormOpen = false
			return t, nil
		case "enter":
			return t, t.saveWorkspaceForm()
		}
	}
	var cmd tea.Cmd
	t.configInputs[t.configInputFocus], cmd = t.configInputs[t.configInputFocus].Update(msg)
	return t, cmd
}

func (t *TUI) saveWorkspaceForm() tea.Cmd {
	workspace := ""
	if len(t.configInputs) > 0 {
		workspace = strings.TrimSpace(t.configInputs[0].Value())
	}
	t.configState.Workspace = workspace
	return t.sendConfigSet(protocol.ConfigSetParams{Action: protocol.ConfigActionUpdateGeneral, Locale: string(t.i18n.Locale()), Theme: t.theme, GuardMode: t.configState.GuardMode, Workspace: &workspace})
}

func (t *TUI) initProviderForm(mc *tuiModelConfig) {
	labels := []string{t.tr("tui.config.provider.type"), t.tr("tui.config.provider.model"), t.tr("tui.config.provider.api_key"), t.tr("tui.config.provider.endpoint"), t.tr("tui.config.provider.context_window"), t.tr("tui.config.provider.strengths")}
	placeholders := []string{"Zhipu", "glm-5.1", "sk-...", "https://api.example.com/v1", "128000", t.tr("tui.config.strengths_placeholder")}
	values := []string{"", "", "", "", "", ""}
	if mc != nil {
		values[0] = mc.Provider
		values[1] = mc.Model
		values[3] = mc.BaseURL
		if mc.ContextWindow > 0 {
			values[4] = strconv.Itoa(mc.ContextWindow)
		}
		values[5] = strings.Join(mc.Strengths, ", ")
	} else {
		switch t.configProviderKind {
		case "openai":
			values[0] = "openai"
			placeholders[1] = "gpt-4o-mini"
			placeholders[3] = t.tr("tui.config.endpoint_default")
		case "anthropic":
			values[0] = "anthropic"
			placeholders[1] = "claude-sonnet-4-20250514"
			placeholders[4] = "200000"
			placeholders[3] = t.tr("tui.config.endpoint_default")
		default:
			values[0] = ""
		}
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
	params := protocol.ConfigSetParams{
		Action:   protocol.ConfigActionUpsertModel,
		ModelRef: t.configEditingName,
		APIKey:   v.APIKey,
		Model: protocol.ConfigModel{
			Provider:      v.Provider,
			Model:         v.Model,
			BaseURL:       v.Endpoint,
			ContextWindow: parsePositiveInt(v.ContextWindow),
		},
	}
	params.Model.Strengths = splitCSV(v.Strengths)
	if t.configSetupMode {
		params.ActiveModel = v.Provider + "/" + v.Model
	}
	return t.sendConfigSet(params)
}

func (t *TUI) providerFormValues() providerFormValues {
	vals := make([]string, 6)
	for i := range vals {
		if i < len(t.configInputs) {
			vals[i] = strings.TrimSpace(t.configInputs[i].Value())
		}
	}
	return providerFormValues{Provider: vals[0], Model: vals[1], APIKey: vals[2], Endpoint: vals[3], ContextWindow: vals[4], Strengths: vals[5]}
}

func (t *TUI) validateProviderForm(v providerFormValues) error {
	if v.Provider == "" || v.Model == "" {
		return fmt.Errorf("%s", t.tr("tui.error.required"))
	}
	if t.configSetupMode && v.APIKey == "" {
		return fmt.Errorf("%s", t.tr("tui.error.api_key_required"))
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
	if v.ContextWindow != "" {
		ctx, err := strconv.Atoi(v.ContextWindow)
		if err != nil || ctx <= 0 {
			return fmt.Errorf("%s", t.tr("tui.error.invalid_context_window"))
		}
	}
	return nil
}
