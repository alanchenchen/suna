package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/alanchenchen/suna/internal/protocol"
)

type reasoningOption struct {
	Family    string
	Label     string
	Reasoning map[string]any
}

func (t *TUI) openReasoning(mc tuiModelConfig) {
	t.configReasoningOpen = true
	t.configReasoningCursor = 0
	t.configReasoningFamily = ""
	t.configEditingName = mc.Ref()
	t.configError = ""
}

func (t *TUI) updateReasoning(msg tea.Msg) (tea.Model, tea.Cmd) {
	if t.configReasoningFamily == "custom" {
		return t.updateReasoningCustom(msg)
	}
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		t.configError = ""
		items := t.reasoningMenuItems()
		switch m.String() {
		case "ctrl+c":
			t.doQuit()
			return t, tea.Quit
		case "esc":
			if t.configReasoningFamily != "" {
				t.configReasoningFamily = ""
				t.configReasoningCursor = 0
				return t, nil
			}
			t.configReasoningOpen = false
			return t, nil
		case "up":
			if t.configReasoningCursor > 0 {
				t.configReasoningCursor--
			}
			return t, nil
		case "down":
			if t.configReasoningCursor < len(items)-1 {
				t.configReasoningCursor++
			}
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
		t.configError = ""
		switch m.String() {
		case "ctrl+c":
			t.doQuit()
			return t, tea.Quit
		case "esc":
			t.configReasoningFamily = ""
			t.configReasoningCursor = 0
			return t, nil
		case "enter":
			return t, t.saveReasoningCustom()
		}
	}
	var cmd tea.Cmd
	t.configInputs[t.configInputFocus], cmd = t.configInputs[t.configInputFocus].Update(msg)
	return t, cmd
}

func (t *TUI) viewReasoning() string {
	if t.configReasoningFamily == "custom" {
		return t.viewReasoningCustom()
	}
	items := t.reasoningMenuItems()
	var lines []string
	for i, item := range items {
		cursor := "  "
		st := lipgloss.NewStyle()
		if i == t.configReasoningCursor {
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
	for _, in := range t.configInputs {
		lines = append(lines, in.View())
	}
	if t.configError != "" {
		lines = append(lines, "", styleError.Render("✗ "+t.configError))
	}
	lines = append(lines, "", styleDim.Render(t.tr("tui.config.reasoning.custom_help")))
	return boxStyle.Width(min(max(56, t.width-8), 90)).Padding(1, 2).Render(styleHL.Render(t.tr("tui.config.reasoning.custom")) + "\n\n" + strings.Join(lines, "\n"))
}

func (t *TUI) reasoningMenuItems() []string {
	if t.configReasoningFamily == "" {
		return []string{t.tr("tui.config.reasoning.clear"), "GPT", "Claude", "DeepSeek V4", t.tr("tui.config.reasoning.custom")}
	}
	options := t.reasoningOptions(t.configReasoningFamily)
	items := make([]string, 0, len(options)+1)
	for _, opt := range options {
		items = append(items, opt.Label)
	}
	items = append(items, t.tr("tui.key.back"))
	return items
}

func (t *TUI) activateReasoningItem(items []string) tea.Cmd {
	if t.configReasoningCursor < 0 || t.configReasoningCursor >= len(items) {
		return nil
	}
	if t.configReasoningFamily == "" {
		switch t.configReasoningCursor {
		case 0:
			return t.saveReasoning(nil)
		case 1:
			t.configReasoningFamily = "gpt"
		case 2:
			t.configReasoningFamily = "claude"
		case 3:
			t.configReasoningFamily = "deepseek"
		case 4:
			t.openReasoningCustom()
		}
		t.configReasoningCursor = 0
		return nil
	}
	options := t.reasoningOptions(t.configReasoningFamily)
	if t.configReasoningCursor == len(options) {
		t.configReasoningFamily = ""
		t.configReasoningCursor = 0
		return nil
	}
	return t.saveReasoning(options[t.configReasoningCursor].Reasoning)
}

func (t *TUI) openReasoningCustom() {
	mc, _ := t.modelByRef(t.configDetailRef)
	data := "{}"
	if len(mc.Reasoning) > 0 {
		if b, err := json.Marshal(mc.Reasoning); err == nil {
			data = string(b)
		}
	}
	in := textinput.New()
	in.Prompt = t.tr("tui.config.reasoning.json") + ": "
	in.Placeholder = `{"reasoning_effort":"high"}`
	in.SetValue(data)
	in.SetWidth(68)
	styles := textinput.DefaultStyles(false)
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(ColorBrand).Bold(true)
	styles.Blurred.Prompt = lipgloss.NewStyle().Foreground(ColorDim)
	in.SetStyles(styles)
	t.configInputs = []textinput.Model{in}
	t.configInputFocus = 0
	t.configReasoningFamily = "custom"
	t.focusConfigInput(0)
}

func (t *TUI) saveReasoningCustom() tea.Cmd {
	value := "{}"
	if len(t.configInputs) > 0 {
		value = strings.TrimSpace(t.configInputs[0].Value())
	}
	if value == "" {
		value = "{}"
	}
	var reasoning map[string]any
	if err := json.Unmarshal([]byte(value), &reasoning); err != nil {
		t.configError = t.tr("tui.config.reasoning.invalid_json")
		return nil
	}
	return t.saveReasoning(reasoning)
}

func (t *TUI) saveReasoning(reasoning map[string]any) tea.Cmd {
	mc, ok := t.modelByRef(t.configDetailRef)
	if !ok {
		t.configError = t.tr("tui.config.model_not_found")
		return nil
	}
	if len(reasoning) == 0 {
		reasoning = nil
	}
	mc.Reasoning = reasoning
	t.updateConfigModelReasoning(mc.Ref(), reasoning)
	t.configReasoningOpen = false
	t.configReasoningFamily = ""
	return t.sendConfigSet(protocol.ConfigSetParams{Action: protocol.ConfigActionUpsertModel, ModelRef: mc.Ref(), Model: protocol.ConfigModel{Provider: mc.Provider, Model: mc.Model, BaseURL: mc.BaseURL, ContextWindow: mc.ContextWindow, Strengths: mc.Strengths, Reasoning: reasoning}})
}

func (t *TUI) reasoningOptions(family string) []reasoningOption {
	switch family {
	case "gpt":
		var out []reasoningOption
		for _, effort := range []string{"none", "minimal", "low", "medium", "high", "xhigh"} {
			label := strings.ToUpper(effort[:1]) + effort[1:]
			out = append(out, reasoningOption{Family: "GPT", Label: label, Reasoning: t.gptReasoning(effort)})
		}
		return out
	case "claude":
		return []reasoningOption{
			{Family: "Claude", Label: "Fast", Reasoning: thinkingBudget(1024)},
			{Family: "Claude", Label: "Balanced", Reasoning: thinkingBudget(2048)},
			{Family: "Claude", Label: "Deep", Reasoning: thinkingBudget(3072)},
		}
	case "deepseek":
		return []reasoningOption{
			{Family: "DeepSeek V4", Label: "Disabled", Reasoning: map[string]any{"thinking": map[string]any{"type": "disabled"}}},
			{Family: "DeepSeek V4", Label: "High", Reasoning: deepSeekReasoning("high")},
			{Family: "DeepSeek V4", Label: "Max", Reasoning: deepSeekReasoning("max")},
		}
	default:
		return nil
	}
}

func (t *TUI) gptReasoning(effort string) map[string]any {
	mc, _ := t.modelByRef(t.configDetailRef)
	if mc.Provider == "openai" {
		return map[string]any{"reasoning": map[string]any{"effort": effort}}
	}
	return map[string]any{"reasoning_effort": effort}
}

func thinkingBudget(tokens int) map[string]any {
	return map[string]any{"thinking": map[string]any{"type": "enabled", "budget_tokens": tokens}}
}

func deepSeekReasoning(effort string) map[string]any {
	return map[string]any{"thinking": map[string]any{"type": "enabled"}, "reasoning_effort": effort}
}

func (t *TUI) reasoningDisplay(mc tuiModelConfig) string {
	if len(mc.Reasoning) == 0 {
		return ""
	}
	if label, ok := t.matchReasoningLabel(mc); ok {
		return label
	}
	return t.tr("tui.config.reasoning.custom")
}

func (t *TUI) matchReasoningLabel(mc tuiModelConfig) (string, bool) {
	oldRef := t.configDetailRef
	t.configDetailRef = mc.Ref()
	defer func() { t.configDetailRef = oldRef }()
	for _, family := range []string{"gpt", "claude", "deepseek"} {
		for _, opt := range t.reasoningOptions(family) {
			if sameJSON(mc.Reasoning, opt.Reasoning) {
				return fmt.Sprintf("%s / %s", opt.Family, opt.Label), true
			}
		}
	}
	return "", false
}

func sameJSON(a, b map[string]any) bool {
	ab, errA := json.Marshal(a)
	bb, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(ab) == string(bb)
}
