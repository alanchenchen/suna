package config

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alanchenchen/suna/internal/protocol"
)

type ReasoningOption struct {
	Family    string
	Label     string
	Reasoning map[string]any
}

func (m *Model) OpenReasoning(mc ModelConfig) {
	m.ReasoningOpen = true
	m.ReasoningCursor = 0
	m.ReasoningFamily = ""
	m.EditingName = mc.Ref()
	m.Error = ""
}

func (m *Model) CloseReasoning() {
	m.ReasoningOpen = false
	m.ReasoningFamily = ""
}

func (m *Model) ReasoningMenuItems(tr func(string) string) []string {
	if m.ReasoningFamily == "" {
		return []string{tr("tui.config.reasoning.clear"), "GPT", "Claude", "DeepSeek V4", "MiniMax M3", tr("tui.config.reasoning.custom")}
	}
	options := ReasoningOptions(m.ReasoningFamily, "")
	items := make([]string, 0, len(options)+1)
	for _, opt := range options {
		items = append(items, opt.Label)
	}
	items = append(items, tr("tui.key.back"))
	return items
}

func (m *Model) MoveReasoning(delta, count int) {
	if count <= 0 {
		m.ReasoningCursor = 0
		return
	}
	m.ReasoningCursor += delta
	if m.ReasoningCursor < 0 {
		m.ReasoningCursor = 0
	}
	if m.ReasoningCursor >= count {
		m.ReasoningCursor = count - 1
	}
}

func (m *Model) BackReasoning() bool {
	if m.ReasoningFamily != "" {
		m.ReasoningFamily = ""
		m.ReasoningCursor = 0
		return true
	}
	m.ReasoningOpen = false
	return false
}

func (m *Model) SelectReasoningRoot() (action string) {
	switch m.ReasoningCursor {
	case 0:
		return "clear"
	case 1:
		m.ReasoningFamily = "gpt"
	case 2:
		m.ReasoningFamily = "claude"
	case 3:
		m.ReasoningFamily = "deepseek"
	case 4:
		m.ReasoningFamily = "minimax"
	case 5:
		return "custom"
	}
	m.ReasoningCursor = 0
	return ""
}

func (m *Model) SelectReasoningOption(options []ReasoningOption) (map[string]any, bool) {
	if m.ReasoningCursor == len(options) {
		m.ReasoningFamily = ""
		m.ReasoningCursor = 0
		return nil, false
	}
	if m.ReasoningCursor < 0 || m.ReasoningCursor >= len(options) {
		return nil, false
	}
	return options[m.ReasoningCursor].Reasoning, true
}

func ReasoningOptions(family, provider string) []ReasoningOption {
	switch family {
	case "gpt":
		var out []ReasoningOption
		for _, effort := range []string{"none", "minimal", "low", "medium", "high", "xhigh"} {
			label := strings.ToUpper(effort[:1]) + effort[1:]
			out = append(out, ReasoningOption{Family: "GPT", Label: label, Reasoning: GPTReasoning(provider, effort)})
		}
		return out
	case "claude":
		return []ReasoningOption{
			{Family: "Claude", Label: "Fast", Reasoning: ThinkingBudget(1024)},
			{Family: "Claude", Label: "Balanced", Reasoning: ThinkingBudget(2048)},
			{Family: "Claude", Label: "Deep", Reasoning: ThinkingBudget(3072)},
		}
	case "deepseek":
		return []ReasoningOption{
			{Family: "DeepSeek V4", Label: "Disabled", Reasoning: map[string]any{"thinking": map[string]any{"type": "disabled"}}},
			{Family: "DeepSeek V4", Label: "High", Reasoning: DeepSeekReasoning("high")},
			{Family: "DeepSeek V4", Label: "Max", Reasoning: DeepSeekReasoning("max")},
		}
	case "minimax":
		return []ReasoningOption{
			{Family: "MiniMax M3", Label: "Split", Reasoning: MiniMaxReasoning()},
		}
	default:
		return nil
	}
}

func GPTReasoning(provider, effort string) map[string]any {
	if provider == "openai" {
		return map[string]any{"reasoning": map[string]any{"effort": effort}}
	}
	return map[string]any{"reasoning_effort": effort}
}

func ThinkingBudget(tokens int) map[string]any {
	return map[string]any{"thinking": map[string]any{"type": "enabled", "budget_tokens": tokens}}
}

func DeepSeekReasoning(effort string) map[string]any {
	return map[string]any{"thinking": map[string]any{"type": "enabled"}, "reasoning_effort": effort}
}

func MiniMaxReasoning() map[string]any {
	return map[string]any{"reasoning_split": true}
}

func ReasoningDisplay(mc ModelConfig, customLabel string) string {
	if len(mc.Reasoning) == 0 {
		return ""
	}
	if label, ok := MatchReasoningLabel(mc); ok {
		return label
	}
	return customLabel
}

func MatchReasoningLabel(mc ModelConfig) (string, bool) {
	for _, family := range []string{"gpt", "claude", "deepseek", "minimax"} {
		for _, opt := range ReasoningOptions(family, mc.Provider) {
			if SameJSON(mc.Reasoning, opt.Reasoning) {
				return fmt.Sprintf("%s / %s", opt.Family, opt.Label), true
			}
		}
	}
	return "", false
}

func ReasoningCustomJSON(mc ModelConfig) string {
	if len(mc.Reasoning) == 0 {
		return "{}"
	}
	if b, err := json.Marshal(mc.Reasoning); err == nil {
		return string(b)
	}
	return "{}"
}

func ParseReasoningJSON(value string) (map[string]any, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "{}"
	}
	var reasoning map[string]any
	if err := json.Unmarshal([]byte(value), &reasoning); err != nil {
		return nil, err
	}
	return reasoning, nil
}

func (m *Model) OpenReasoningCustom() {
	m.ReasoningFamily = "custom"
	m.InputFocus = 0
}

func (m *Model) BuildReasoningSave(mc ModelConfig, reasoning map[string]any) protocol.ConfigSetParams {
	if len(reasoning) == 0 {
		reasoning = nil
	}
	m.CloseReasoning()
	return protocol.ConfigSetParams{
		Action:   protocol.ConfigActionUpsertModel,
		ModelRef: mc.Ref(),
		Model: protocol.ConfigModel{
			Provider:        mc.Provider,
			Model:           mc.Model,
			BaseURL:         mc.BaseURL,
			ContextWindow:   mc.ContextWindow,
			MaxOutputTokens: mc.MaxOutputTokens,
			Strengths:       mc.Strengths,
			Reasoning:       reasoning,
		},
	}
}

func SameJSON(a, b map[string]any) bool {
	ab, errA := json.Marshal(a)
	bb, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(ab) == string(bb)
}
