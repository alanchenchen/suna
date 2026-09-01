package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	coreconfig "github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/tui/components/combobox"
	"github.com/alanchenchen/suna/internal/tui/components/selection"
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
		t.config.Notice = ""
		switch t.config.InputFocus {
		case tuiconfig.ProviderFormProtocolIndex:
			switch m.String() {
			case "left":
				t.cycleProviderProtocol(-1)
				return t, nil
			case "right":
				t.cycleProviderProtocol(1)
				return t, nil
			}
		case tuiconfig.ProviderFormAuthModeIndex:
			switch m.String() {
			case "left":
				t.cycleProviderAuthMode(-1)
				return t, nil
			case "right":
				t.cycleProviderAuthMode(1)
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
			// model 字段 enter 打开模型选择浮层：用户在浮层内选择或输入；
			// 其他字段 enter 保持原有导航语义。
			if t.config.InputFocus == tuiconfig.ProviderFormModelIndex {
				return t, t.openModelPickerForForm()
			}
			if t.config.InputFocus == len(t.config.Inputs)-1 {
				return t, t.saveProviderForm()
			}
			if idx, ok := t.config.NextInput(len(t.config.Inputs)); ok {
				return t, t.focusConfigInput(idx)
			}
			return t, nil
		case "shift+tab", "up":
			if idx, ok := t.config.PrevInput(len(t.config.Inputs)); ok {
				return t, t.focusConfigInputWithDelta(idx, -1)
			}
			return t, nil
		case "tab", "down":
			if idx, ok := t.config.NextInput(len(t.config.Inputs)); ok {
				return t, t.focusConfigInputWithDelta(idx, 1)
			}
			return t, nil
		}
	}
	if t.config.InputFocus == tuiconfig.ProviderFormProtocolIndex || t.config.InputFocus == tuiconfig.ProviderFormAuthModeIndex || t.config.InputFocus == tuiconfig.ProviderFormModelIndex {
		// 选择行（protocol/auth_mode/model）不接收字符输入；model 的 enter 已在上方处理。
		return t, nil
	}
	var cmd tea.Cmd
	t.config.Inputs[t.config.InputFocus], cmd = t.config.Inputs[t.config.InputFocus].Update(msg)
	return t, cmd
}
func (t *TUI) openProviderForm(ref string, mc *tuiconfig.ModelConfig) {
	t.config.OpenProviderForm(ref, mc)
	t.config.Notice = ""
	t.initProviderForm(mc)
}

func (t *TUI) openProviderModelForm(provider string) {
	t.config.OpenProviderModelForm(provider)
	t.config.Notice = ""
	var template *tuiconfig.ModelConfig
	for _, mc := range t.configModelsSnapshot() {
		if mc.Provider == provider {
			copy := mc
			copy.Model = ""
			template = &copy
			break
		}
	}
	t.initProviderForm(template)
	if len(t.config.Inputs) > tuiconfig.ProviderFormAPIKeyIndex {
		t.config.Inputs[tuiconfig.ProviderFormAPIKeyIndex].SetValue("")
	}
	t.focusConfigInput(t.nextEditableConfigInput(0, 1))
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
		styles := textInputStyles()
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
		AuthMode:        t.tr("tui.config.provider.auth_mode"),
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
	return t.focusConfigInputWithDelta(idx, 1)
}

func (t *TUI) focusConfigInputWithDelta(idx, delta int) tea.Cmd {
	idx = t.nextEditableConfigInput(idx, delta)
	if !t.config.FocusInput(idx, len(t.config.Inputs)) {
		return nil
	}
	var cmds []tea.Cmd
	for i := range t.config.Inputs {
		if i == t.config.InputFocus {
			if i != tuiconfig.ProviderFormProtocolIndex && i != tuiconfig.ProviderFormAuthModeIndex && i != tuiconfig.ProviderFormModelIndex {
				cmds = append(cmds, t.config.Inputs[i].Focus())
			}
		} else {
			t.config.Inputs[i].Blur()
		}
	}
	return tea.Batch(cmds...)
}

func (t *TUI) configInputEditable(idx int) bool {
	if idx < 0 || idx >= len(t.config.Inputs) {
		return false
	}
	if t.config.FormProvider != "" && (idx == tuiconfig.ProviderFormProviderIndex || idx == tuiconfig.ProviderFormAPIKeyIndex) {
		return false
	}
	if idx == tuiconfig.ProviderFormAuthModeIndex && !t.providerFormUsesAnthropic() {
		return false
	}
	return true
}

func (t *TUI) nextEditableConfigInput(idx, delta int) int {
	if len(t.config.Inputs) == 0 {
		return idx
	}
	if delta == 0 {
		delta = 1
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(t.config.Inputs) {
		idx = len(t.config.Inputs) - 1
	}
	for idx >= 0 && idx < len(t.config.Inputs) {
		if t.configInputEditable(idx) {
			return idx
		}
		idx += delta
	}
	if t.configInputEditable(t.config.InputFocus) {
		return t.config.InputFocus
	}
	for i := range t.config.Inputs {
		if t.configInputEditable(i) {
			return i
		}
	}
	return t.config.InputFocus
}
func (t *TUI) saveProviderForm() tea.Cmd {
	v := t.providerFormValues()
	if err := t.validateProviderForm(v); err != nil {
		t.config.Error = err.Error()
		return nil
	}
	reasoning, cleared := t.providerFormReasoningForSave(v)
	t.pendingConfigNotice = ""
	if cleared {
		t.pendingConfigNotice = t.tr("tui.config.reasoning.cleared_on_protocol_change")
	}
	save := t.config.BuildProviderSave(v, reasoning)
	return t.sendConfigSet(save.Params)
}

func (t *TUI) providerFormReasoningForSave(v tuiconfig.ProviderFormValues) (map[string]any, bool) {
	existing, ok := t.modelByRef(t.config.EditingName)
	if !ok {
		return nil, false
	}
	if existing.Protocol != v.Protocol && len(existing.Reasoning) > 0 {
		return nil, true
	}
	return existing.Reasoning, false
}
func (t *TUI) providerFormValues() tuiconfig.ProviderFormValues {
	values := make([]string, len(t.config.Inputs))
	for i := range t.config.Inputs {
		values[i] = t.config.Inputs[i].Value()
	}
	v := tuiconfig.ProviderFormValuesFromStrings(values)
	if v.Protocol != coreconfig.ModelProtocolAnthropic {
		v.AuthMode = ""
	}
	return v
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
		InvalidProvider:        t.tr("tui.error.invalid_provider"),
	})
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
	styles := textInputStyles()
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
		cursor := selection.Rail(i == t.config.ReasoningCursor, 0, styleCursor)
		st := lipgloss.NewStyle()
		if i == t.config.ReasoningCursor {
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
	styles := textInputStyles()
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

// openModelPickerForForm 在模型表单的 model 字段 enter 时打开同步选择浮层：
// 缓存命中直接填充候选；未命中则异步拉取（daemon 不阻塞连接循环），
// 结果通过 config.models_result 通知回填。选择器输入即过滤；
// 拉取失败或列表为空时，输入值直接作为自定义模型名。
func (t *TUI) openModelPickerForForm() tea.Cmd {
	if len(t.config.Inputs) <= tuiconfig.ProviderFormModelIndex {
		return nil
	}
	provider := strings.TrimSpace(t.config.Inputs[tuiconfig.ProviderFormProviderIndex].Value())
	if provider == "" {
		t.config.Error = t.tr("tui.config.provider.required")
		return nil
	}
	// 同步选择器：输入框即筛选框，无异步过滤链路；每次打开重置为空输入。
	t.modelCombobox = combobox.New(t.tr("tui.config.picker.placeholder"))
	t.modelCombobox.SetPrompt(t.tr("tui.config.picker.filter") + " ")
	t.modelCombobox.SetEmptyHint(t.tr("tui.config.provider.models_empty"))
	t.modelCombobox.SetSize(min(max(36, t.width-16), 60), 8)
	t.modelCombobox.SetCurrent(strings.TrimSpace(t.config.Inputs[tuiconfig.ProviderFormModelIndex].Value()))
	// 不 Blur chat 输入框：浮层打开时 mode 是 Config，按键不会到达 chat 输入框；
	// Blur 会在回到 chat 后留下失焦状态，表现为输入框无法输入。
	t.modelPickerOpen = true
	t.modelPickerProvider = provider
	t.modelPickerError = ""
	if models, ok := t.modelsCache[provider]; ok {
		t.modelPickerLoading = false
		t.modelCombobox.SetItems(models)
		return t.modelCombobox.Focus()
	}
	t.modelPickerLoading = true
	return tea.Batch(t.modelCombobox.Focus(), func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification(notifyConfigError, fmt.Errorf("%s", t.tr("error.not_connected")))
		}
		if err := t.localCli.DiscoverModels(provider); err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		return nil
	})
}

// updateProviderModelPicker 处理 Config 页模型选择浮层的按键与滚轮。
// 浮层是同步选择器：输入即过滤，enter 确认光标所在候选（无输入时）
// 或输入值（自定义名）；esc 直接关闭。滚轮逐行移动光标。
func (t *TUI) updateProviderModelPicker(key string, msg tea.Msg) (tea.Model, tea.Cmd) {
	// 窗口尺寸更新必须继续生效：浮层渲染宽度依赖 t.width。
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		t.width, t.height = ws.Width, ws.Height
		return t, nil
	}
	// 滚轮：候选行逐行滚动，不循环。
	if mm, ok := any(msg).(tea.MouseWheelMsg); ok {
		switch mm.Mouse().Button {
		case tea.MouseWheelUp:
			t.modelCombobox.MoveCursor(-1)
		case tea.MouseWheelDown:
			t.modelCombobox.MoveCursor(1)
		}
		return t, nil
	}
	switch key {
	case "ctrl+c":
		t.doQuit()
		return t, tea.Quit
	case "up", "ctrl+p":
		t.modelCombobox.MoveCursor(-1)
		return t, nil
	case "down", "ctrl+n":
		t.modelCombobox.MoveCursor(1)
		return t, nil
	case "pgup":
		t.modelCombobox.PageUp()
		return t, nil
	case "pgdown":
		t.modelCombobox.PageDown()
		return t, nil
	case "enter":
		return t, t.applyProviderPickerSelection()
	case "esc":
		return t, t.closeProviderPicker()
	}
	// 其余按键（字符、退格、粘贴等）转给输入框，过滤结果同步重算。
	return t, t.modelCombobox.UpdateInput(msg)
}

// applyProviderPickerSelection 确认选择。规则与主流 combobox 一致：
//   - 能筛出候选：enter 选高亮项（筛选是选择手段，不是输入目的）；
//   - 筛不出候选（拉取失败、列表外自定义名）：enter 选输入值。
//
// 输入值无条件优先会让“筛选后 enter”拿到筛选词而不是高亮候选。
func (t *TUI) applyProviderPickerSelection() tea.Cmd {
	if name, ok := t.modelCombobox.Selected(); ok {
		return t.applyProviderPickerModel(name)
	}
	if value := t.modelCombobox.InputValue(); value != "" {
		return t.applyProviderPickerModel(value)
	}
	return nil
}

// closeProviderPicker 关闭模型选择浮层并聚焦表单 model 字段。
func (t *TUI) closeProviderPicker() tea.Cmd {
	t.modelPickerOpen = false
	t.modelPickerProvider = ""
	t.modelPickerLoading = false
	t.modelPickerError = ""
	return t.focusConfigInput(tuiconfig.ProviderFormModelIndex)
}

// applyProviderPickerModel 用选中的模型名填充表单 model 字段并关闭浮层。
func (t *TUI) applyProviderPickerModel(model string) tea.Cmd {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}
	if len(t.config.Inputs) > tuiconfig.ProviderFormModelIndex {
		t.config.Inputs[tuiconfig.ProviderFormModelIndex].SetValue(model)
	}
	t.modelPickerOpen = false
	t.modelPickerProvider = ""
	t.modelPickerLoading = false
	t.modelPickerError = ""
	return t.focusConfigInput(tuiconfig.ProviderFormModelIndex)
}

func (t *TUI) cycleProviderProtocol(delta int) {
	if len(t.config.Inputs) <= tuiconfig.ProviderFormProtocolIndex {
		return
	}
	current := tuiconfig.ModelProtocolValue(t.config.Inputs[tuiconfig.ProviderFormProtocolIndex].Value())
	next := tuiconfig.NextProviderProtocol(current, delta)
	t.config.Inputs[tuiconfig.ProviderFormProtocolIndex].SetValue(string(next))
	if next != coreconfig.ModelProtocolAnthropic && len(t.config.Inputs) > tuiconfig.ProviderFormAuthModeIndex {
		t.config.Inputs[tuiconfig.ProviderFormAuthModeIndex].SetValue("")
		if t.config.InputFocus == tuiconfig.ProviderFormAuthModeIndex {
			t.focusConfigInputWithDelta(tuiconfig.ProviderFormModelIndex, 1)
		}
	}
}

func (t *TUI) cycleProviderAuthMode(delta int) {
	if !t.providerFormUsesAnthropic() || len(t.config.Inputs) <= tuiconfig.ProviderFormAuthModeIndex {
		return
	}
	current := coreconfig.AuthMode(t.config.Inputs[tuiconfig.ProviderFormAuthModeIndex].Value())
	next := tuiconfig.NextAuthMode(current, delta)
	t.config.Inputs[tuiconfig.ProviderFormAuthModeIndex].SetValue(string(next))
}

func (t *TUI) providerFormUsesAnthropic() bool {
	if len(t.config.Inputs) <= tuiconfig.ProviderFormProtocolIndex {
		return false
	}
	return tuiconfig.ModelProtocolValue(t.config.Inputs[tuiconfig.ProviderFormProtocolIndex].Value()) == coreconfig.ModelProtocolAnthropic
}
