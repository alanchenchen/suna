package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	tuiconfig "github.com/alanchenchen/suna/internal/tui/pages/config"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
)

func (t *TUI) updateProviderForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		t.width, t.height, t.ready = m.Width, m.Height, true
		return t, nil
	case tea.KeyPressMsg:
		t.config.Error = ""
		if t.config.InputFocus == tuiconfig.ProviderFormProtocolIndex {
			switch m.String() {
			case "left":
				t.cycleProviderProtocol(-1)
				return t, nil
			case "right":
				t.cycleProviderProtocol(1)
				return t, nil
			}
		}
		switch m.String() {
		case "ctrl+c":
			t.doQuit()
			return t, tea.Quit
		case "esc":
			if t.config.CloseProviderForm() {
				t.mode = uipage.Welcome
			}
			return t, nil
		case "enter":
			if t.config.InputFocus == len(t.config.Inputs)-1 {
				return t, t.saveProviderForm()
			}
			if idx, ok := t.config.NextInput(len(t.config.Inputs)); ok {
				return t, t.focusConfigInput(idx)
			}
			return t, nil
		case "shift+tab", "up":
			if idx, ok := t.config.PrevInput(len(t.config.Inputs)); ok {
				return t, t.focusConfigInput(idx)
			}
			return t, nil
		case "tab", "down":
			if idx, ok := t.config.NextInput(len(t.config.Inputs)); ok {
				return t, t.focusConfigInput(idx)
			}
			return t, nil
		}
	}
	if t.config.InputFocus == tuiconfig.ProviderFormProtocolIndex {
		return t, nil
	}
	var cmd tea.Cmd
	t.config.Inputs[t.config.InputFocus], cmd = t.config.Inputs[t.config.InputFocus].Update(msg)
	return t, cmd
}
func (t *TUI) openProviderForm(ref string, mc *tuiconfig.ModelConfig) {
	t.config.OpenProviderForm(ref, mc)
	t.initProviderForm(mc)
}
func (t *TUI) initProviderForm(mc *tuiconfig.ModelConfig) {
	spec := t.config.ProviderFormSpec(t.providerFormLabels(), mc)
	t.config.Inputs = make([]textinput.Model, len(spec.Labels))
	for i := range spec.Labels {
		in := textinput.New()
		in.Prompt = spec.Labels[i] + ": "
		in.Placeholder = spec.Placeholders[i]
		in.SetValue(spec.Values[i])
		in.SetWidth(46)
		if i == spec.PasswordAt {
			in.EchoMode = textinput.EchoPassword
			in.EchoCharacter = '*'
		}
		styles := textinput.DefaultStyles(false)
		styles.Focused.Prompt = lipgloss.NewStyle().Foreground(ColorBrand).Bold(true)
		styles.Blurred.Prompt = lipgloss.NewStyle().Foreground(ColorDim)
		in.SetStyles(styles)
		t.config.Inputs[i] = in
	}
	t.config.InputFocus = 0
	t.focusConfigInput(0)
}

func (t *TUI) providerFormLabels() tuiconfig.ProviderFormLabels {
	return tuiconfig.ProviderFormLabels{
		Provider:        t.tr("tui.config.provider.type"),
		Protocol:        t.tr("tui.config.provider.protocol"),
		Model:           t.tr("tui.config.provider.model"),
		APIKey:          t.tr("tui.config.provider.api_key"),
		Endpoint:        t.tr("tui.config.provider.endpoint"),
		ContextWindow:   t.tr("tui.config.provider.context_window"),
		MaxOutputTokens: t.tr("tui.config.provider.max_output_tokens"),
		Strengths:       t.tr("tui.config.provider.strengths"),
		SubtaskFor:      t.tr("tui.config.provider.subtask_for"),
		StrengthsHint:   t.tr("tui.config.strengths_placeholder"),
		SubtaskForHint:  t.tr("tui.config.subtask_for_placeholder"),
	}
}

func (t *TUI) focusConfigInput(idx int) tea.Cmd {
	if !t.config.FocusInput(idx, len(t.config.Inputs)) {
		return nil
	}
	var cmds []tea.Cmd
	for i := range t.config.Inputs {
		if i == t.config.InputFocus {
			if i != tuiconfig.ProviderFormProtocolIndex {
				cmds = append(cmds, t.config.Inputs[i].Focus())
			}
		} else {
			t.config.Inputs[i].Blur()
		}
	}
	return tea.Batch(cmds...)
}
func (t *TUI) saveProviderForm() tea.Cmd {
	v := t.providerFormValues()
	if err := t.validateProviderForm(v); err != nil {
		t.config.Error = err.Error()
		return nil
	}
	var reasoning map[string]any
	if existing, ok := t.modelByRef(t.config.EditingName); ok {
		reasoning = existing.Reasoning
	}
	save := t.config.BuildProviderSave(v, reasoning)
	return t.sendConfigSet(save.Params)
}
func (t *TUI) providerFormValues() tuiconfig.ProviderFormValues {
	values := make([]string, len(t.config.Inputs))
	for i := range t.config.Inputs {
		values[i] = t.config.Inputs[i].Value()
	}
	return tuiconfig.ProviderFormValuesFromStrings(values)
}

func (t *TUI) validateProviderForm(v tuiconfig.ProviderFormValues) error {
	return tuiconfig.ValidateProviderForm(v, t.config.SetupMode, tuiconfig.ProviderValidationLabels{
		Required:               t.tr("tui.error.required"),
		APIKeyRequired:         t.tr("tui.error.api_key_required"),
		EndpointRequired:       t.tr("tui.error.endpoint_required"),
		InvalidEndpoint:        t.tr("tui.error.invalid_endpoint"),
		InvalidContextWindow:   t.tr("tui.error.invalid_context_window"),
		InvalidMaxOutputTokens: t.tr("tui.error.invalid_max_output_tokens"),
		InvalidProtocol:        t.tr("tui.error.invalid_protocol"),
	})
}

func (t *TUI) openProviderKind() {
	t.config.OpenProviderKind()
}
func (t *TUI) providerKindOptions() []string {
	return tuiconfig.ProviderKindOptions()
}
func (t *TUI) updateProviderKind(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		switch m.String() {
		case "ctrl+c":
			t.doQuit()
			return t, tea.Quit
		case "esc":
			t.config.KindOpen = false
			return t, nil
		case "up":
			t.config.MoveProviderKind(-1)
			return t, nil
		case "down":
			t.config.MoveProviderKind(1)
			return t, nil
		case "enter":
			t.config.SelectProviderKind()
			t.openProviderForm("", nil)
			return t, t.config.Inputs[t.config.InputFocus].Focus()
		}
	}
	return t, nil
}

func (t *TUI) openWorkspaceForm() tea.Cmd {
	t.config.OpenWorkspaceForm()
	t.initWorkspaceForm()
	return t.config.Inputs[t.config.InputFocus].Focus()
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
	t.config.Inputs = []textinput.Model{in}
	t.config.InputFocus = 0
	t.focusConfigInput(0)
}
func (t *TUI) updateWorkspaceForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		t.width, t.height, t.ready = m.Width, m.Height, true
		return t, nil
	case tea.KeyPressMsg:
		t.config.Error = ""
		switch m.String() {
		case "ctrl+c":
			t.doQuit()
			return t, tea.Quit
		case "esc":
			t.config.CloseForm()
			return t, nil
		case "enter":
			return t, t.saveWorkspaceForm()
		}
	}
	var cmd tea.Cmd
	t.config.Inputs[t.config.InputFocus], cmd = t.config.Inputs[t.config.InputFocus].Update(msg)
	return t, cmd
}
func (t *TUI) saveWorkspaceForm() tea.Cmd {
	workspace := ""
	if len(t.config.Inputs) > 0 {
		workspace = strings.TrimSpace(t.config.Inputs[0].Value())
	}
	t.configState.Workspace = workspace
	return t.sendConfigSet(tuiconfig.BuildWorkspaceSave(workspace, string(t.i18n.Locale()), t.theme, t.configState.GuardMode))
}

type reasoningOption = tuiconfig.ReasoningOption

func (t *TUI) openReasoning(mc tuiconfig.ModelConfig) {
	t.config.OpenReasoning(mc)
}

func (t *TUI) updateReasoning(msg tea.Msg) (tea.Model, tea.Cmd) {
	if t.config.ReasoningFamily == "custom" {
		return t.updateReasoningCustom(msg)
	}
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		t.config.Error = ""
		items := t.reasoningMenuItems()
		switch m.String() {
		case "ctrl+c":
			t.doQuit()
			return t, tea.Quit
		case "esc":
			t.config.BackReasoning()
			return t, nil
		case "up":
			t.config.MoveReasoning(-1, len(items))
			return t, nil
		case "down":
			t.config.MoveReasoning(1, len(items))
			return t, nil
		case "enter":
			return t, t.activateReasoningItem(items)
		}
	}
	return t, nil
}

func (t *TUI) updateReasoningCustom(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		t.config.Error = ""
		switch m.String() {
		case "ctrl+c":
			t.doQuit()
			return t, tea.Quit
		case "esc":
			t.config.BackReasoning()
			return t, nil
		case "enter":
			return t, t.saveReasoningCustom()
		}
	}
	var cmd tea.Cmd
	t.config.Inputs[t.config.InputFocus], cmd = t.config.Inputs[t.config.InputFocus].Update(msg)
	return t, cmd
}

func (t *TUI) viewReasoning() string {
	if t.config.ReasoningFamily == "custom" {
		return t.viewReasoningCustom()
	}
	items := t.reasoningMenuItems()
	var lines []string
	for i, item := range items {
		cursor := "  "
		st := lipgloss.NewStyle()
		if i == t.config.ReasoningCursor {
			cursor = styleCursor.Render("▶ ")
			st = styleHL
		}
		lines = append(lines, cursor+st.Render(item))
	}
	lines = append(lines, "", styleDim.Render(t.tr("tui.config.reasoning.help")))
	return boxStyle.Width(min(max(48, t.width-8), 72)).Padding(1, 2).Render(styleHL.Render(t.tr("tui.config.reasoning")) + "\n\n" + strings.Join(lines, "\n"))
}

func (t *TUI) viewReasoningCustom() string {
	var lines []string
	for _, in := range t.config.Inputs {
		lines = append(lines, in.View())
	}
	if t.config.Error != "" {
		lines = append(lines, "", styleError.Render("✗ "+t.config.Error))
	}
	lines = append(lines, "", styleDim.Render(t.tr("tui.config.reasoning.custom_help")))
	return boxStyle.Width(min(max(56, t.width-8), 90)).Padding(1, 2).Render(styleHL.Render(t.tr("tui.config.reasoning.custom")) + "\n\n" + strings.Join(lines, "\n"))
}

func (t *TUI) reasoningMenuItems() []string {
	return t.config.ReasoningMenuItems(func(key string) string { return t.tr(key) })
}

func (t *TUI) activateReasoningItem(items []string) tea.Cmd {
	if t.config.ReasoningCursor < 0 || t.config.ReasoningCursor >= len(items) {
		return nil
	}
	if t.config.ReasoningFamily == "" {
		switch t.config.SelectReasoningRoot() {
		case "clear":
			return t.saveReasoning(nil)
		case "custom":
			t.openReasoningCustom()
		}
		return nil
	}
	options := t.reasoningOptions(t.config.ReasoningFamily)
	if reasoning, ok := t.config.SelectReasoningOption(options); ok {
		return t.saveReasoning(reasoning)
	}
	return nil
}

func (t *TUI) openReasoningCustom() {
	mc, _ := t.modelByRef(t.config.DetailRef)
	data := tuiconfig.ReasoningCustomJSON(mc)
	in := textinput.New()
	in.Prompt = t.tr("tui.config.reasoning.json") + ": "
	in.Placeholder = `{"reasoning_effort":"high"}`
	in.SetValue(data)
	in.SetWidth(68)
	styles := textinput.DefaultStyles(false)
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(ColorBrand).Bold(true)
	styles.Blurred.Prompt = lipgloss.NewStyle().Foreground(ColorDim)
	in.SetStyles(styles)
	t.config.Inputs = []textinput.Model{in}
	t.config.OpenReasoningCustom()
	t.focusConfigInput(0)
}

func (t *TUI) saveReasoningCustom() tea.Cmd {
	value := "{}"
	if len(t.config.Inputs) > 0 {
		value = t.config.Inputs[0].Value()
	}
	reasoning, err := tuiconfig.ParseReasoningJSON(value)
	if err != nil {
		t.config.Error = t.tr("tui.config.reasoning.invalid_json")
		return nil
	}
	return t.saveReasoning(reasoning)
}

func (t *TUI) saveReasoning(reasoning map[string]any) tea.Cmd {
	mc, ok := t.modelByRef(t.config.DetailRef)
	if !ok {
		t.config.Error = t.tr("tui.config.model_not_found")
		return nil
	}
	params := t.config.BuildReasoningSave(mc, reasoning)
	t.updateConfigModelReasoning(mc.Ref(), params.Model.Reasoning)
	return t.sendConfigSet(params)
}

func (t *TUI) reasoningOptions(family string) []reasoningOption {
	mc, _ := t.modelByRef(t.config.DetailRef)
	return tuiconfig.ReasoningOptions(family, string(mc.Protocol))
}

func (t *TUI) gptReasoning(effort string) map[string]any {
	mc, _ := t.modelByRef(t.config.DetailRef)
	return tuiconfig.GPTReasoning(string(mc.Protocol), effort)
}

func deepSeekReasoning(effort string) map[string]any {
	return tuiconfig.DeepSeekReasoning(effort)
}

func (t *TUI) reasoningDisplay(mc tuiconfig.ModelConfig) string {
	return tuiconfig.ReasoningDisplay(mc, t.tr("tui.config.reasoning.custom"))
}

func (t *TUI) matchReasoningLabel(mc tuiconfig.ModelConfig) (string, bool) {
	return tuiconfig.MatchReasoningLabel(mc)
}

func sameJSON(a, b map[string]any) bool {
	return tuiconfig.SameJSON(a, b)
}

func (t *TUI) cycleProviderProtocol(delta int) {
	if len(t.config.Inputs) <= tuiconfig.ProviderFormProtocolIndex {
		return
	}
	current := tuiconfig.ModelProtocolValue(t.config.Inputs[tuiconfig.ProviderFormProtocolIndex].Value())
	next := tuiconfig.NextProviderProtocol(current, delta)
	t.config.Inputs[tuiconfig.ProviderFormProtocolIndex].SetValue(string(next))
}
