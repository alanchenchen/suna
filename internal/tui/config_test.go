package tui

import (
	"testing"

	"charm.land/bubbles/v2/textinput"

	"github.com/alanchenchen/suna/internal/protocol"
	tuiconfig "github.com/alanchenchen/suna/internal/tui/pages/config"
)

func TestConfigDeleteOptionsOfferAPIKeyForLastProviderModel(t *testing.T) {
	tui := &TUI{
		i18n:   newTranslator(LocaleEN),
		config: tuiconfig.Model{DeleteConfirm: "openai/gpt-4o-mini"},
		configState: protocol.ConfigParams{Models: []protocol.ConfigModel{
			{Provider: "openai", Model: "gpt-4o-mini", HasAPIKey: true},
			{Provider: "anthropic", Model: "claude-sonnet", HasAPIKey: true},
		}},
	}

	if !tui.shouldOfferDeleteAPIKey("openai/gpt-4o-mini") {
		t.Fatalf("shouldOfferDeleteAPIKey = false, want true")
	}
	options := tui.configDeleteOptions()
	if len(options) != 3 {
		t.Fatalf("len(options) = %d, want 3: %#v", len(options), options)
	}
}

func TestConfigDeleteOptionsHideAPIKeyWhenProviderStillUsed(t *testing.T) {
	tui := &TUI{
		i18n:   newTranslator(LocaleEN),
		config: tuiconfig.Model{DeleteConfirm: "openai/gpt-4o-mini"},
		configState: protocol.ConfigParams{Models: []protocol.ConfigModel{
			{Provider: "openai", Model: "gpt-4o-mini", HasAPIKey: true},
			{Provider: "openai", Model: "gpt-4o", HasAPIKey: true},
		}},
	}

	if tui.shouldOfferDeleteAPIKey("openai/gpt-4o-mini") {
		t.Fatalf("shouldOfferDeleteAPIKey = true, want false")
	}
	options := tui.configDeleteOptions()
	if len(options) != 2 {
		t.Fatalf("len(options) = %d, want 2: %#v", len(options), options)
	}
}

func TestConfigDeleteOptionsHideAPIKeyWhenMissing(t *testing.T) {
	tui := &TUI{
		i18n:   newTranslator(LocaleEN),
		config: tuiconfig.Model{DeleteConfirm: "openai/gpt-4o-mini"},
		configState: protocol.ConfigParams{Models: []protocol.ConfigModel{
			{Provider: "openai", Model: "gpt-4o-mini"},
		}},
	}

	if tui.shouldOfferDeleteAPIKey("openai/gpt-4o-mini") {
		t.Fatalf("shouldOfferDeleteAPIKey = true, want false")
	}
	options := tui.configDeleteOptions()
	if len(options) != 2 {
		t.Fatalf("len(options) = %d, want 2: %#v", len(options), options)
	}
}

func TestSwitchModelRefUpdatesActiveProviderModel(t *testing.T) {
	tui := &TUI{
		i18n:         newTranslator(LocaleEN),
		providerName: "openai",
		modelName:    "gpt-4o-mini",
		configState: protocol.ConfigParams{ActiveModel: "openai/gpt-4o-mini", Models: []protocol.ConfigModel{
			{Provider: "openai", Model: "gpt-4o-mini", ContextWindow: 128000},
			{Provider: "anthropic", Model: "claude-sonnet", ContextWindow: 200000},
		}},
	}

	cmd := tui.switchModelRef("anthropic/claude-sonnet")
	if cmd == nil {
		t.Fatalf("switchModelRef returned nil cmd")
	}
	if tui.configState.ActiveModel != "anthropic/claude-sonnet" {
		t.Fatalf("ActiveModel = %q", tui.configState.ActiveModel)
	}
	if tui.providerName != "anthropic" || tui.modelName != "claude-sonnet" {
		t.Fatalf("provider/model = %q/%q", tui.providerName, tui.modelName)
	}
	if tui.contextWindow != 200000 {
		t.Fatalf("contextWindow = %d", tui.contextWindow)
	}
}

func TestConfigModelRefAfterEditUsesNewRef(t *testing.T) {
	tui := &TUI{
		i18n:   newTranslator(LocaleEN),
		config: tuiconfig.Model{EditingName: "openai/gpt-4o-mini", Inputs: providerInputsForTest("openai", "openai_responses", "gpt-4o", "", "https://api.openai.com/v1", "128000", "", "", "")},
		configState: protocol.ConfigParams{ActiveModel: "openai/gpt-4o", Models: []protocol.ConfigModel{
			{Provider: "openai", Model: "gpt-4o", BaseURL: "https://api.openai.com/v1", ContextWindow: 128000},
		}},
	}

	if got := tui.configProviderFormRef(); got != "openai/gpt-4o" {
		t.Fatalf("configProviderFormRef = %q", got)
	}
	if !tui.openConfigDetailIfPresent(tui.configProviderFormRef()) {
		t.Fatalf("openConfigDetailIfPresent returned false")
	}
	if tui.config.Page != "detail" || tui.config.DetailRef != "openai/gpt-4o" {
		t.Fatalf("detail page/ref = %q/%q", tui.config.Page, tui.config.DetailRef)
	}
}

func TestReturnToConfigModelsClearsMissingDetail(t *testing.T) {
	tui := &TUI{
		i18n:   newTranslator(LocaleEN),
		config: tuiconfig.Model{Page: "detail", DetailRef: "openai/gpt-4o-mini"},
		configState: protocol.ConfigParams{ActiveModel: "anthropic/claude-sonnet", Models: []protocol.ConfigModel{
			{Provider: "anthropic", Model: "claude-sonnet"},
		}},
	}

	if _, ok := tui.modelByRef(tui.config.DetailRef); ok {
		t.Fatalf("deleted model unexpectedly exists")
	}
	tui.returnToConfigModels()
	if tui.config.Page != "models" || tui.config.DetailRef != "" {
		t.Fatalf("page/ref = %q/%q, want models/empty", tui.config.Page, tui.config.DetailRef)
	}
	rows := tui.configModelRows()
	if tui.config.Cursor < 0 || tui.config.Cursor >= len(rows) || rows[tui.config.Cursor].Name != "anthropic/claude-sonnet" {
		t.Fatalf("cursor = %d rows = %#v", tui.config.Cursor, rows)
	}
}

func providerInputsForTest(values ...string) []textinput.Model {
	inputs := make([]textinput.Model, len(values))
	for i, value := range values {
		in := textinput.New()
		in.SetValue(value)
		inputs[i] = in
	}
	return inputs
}

func TestGPTReasoningUsesResponsesForOpenAI(t *testing.T) {
	tui := &TUI{config: tuiconfig.Model{DetailRef: "openai/gpt-5"}, configState: testReasoningConfigWithProtocol("openai", "gpt-5", "openai_responses")}
	got := tui.gptReasoning("high")
	reasoning, ok := got["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("gptReasoning(high)[reasoning] = %#v, want map", got["reasoning"])
	}
	if got := reasoning["effort"]; got != "high" {
		t.Fatalf("gptReasoning(high)[reasoning][effort] = %#v, want %q", got, "high")
	}
}

func TestGPTReasoningUsesChatForCompatible(t *testing.T) {
	tui := &TUI{config: tuiconfig.Model{DetailRef: "deepseek/deepseek-v4-pro"}, configState: testReasoningConfig("deepseek", "deepseek-v4-pro")}
	got := tui.gptReasoning("none")
	if got["reasoning_effort"] != "none" {
		t.Fatalf("gptReasoning(none)[reasoning_effort] = %#v, want %q", got["reasoning_effort"], "none")
	}
}

func TestClaudeReasoningPresetsMatchClaudeCodeStyleBudgets(t *testing.T) {
	options := tuiconfig.ReasoningOptions("claude", "anthropic")
	if got, want := len(options), 5; got != want {
		t.Fatalf("len(options) = %d, want %d", got, want)
	}
	wants := []struct {
		label  string
		budget int
	}{
		{label: "Think", budget: 4096},
		{label: "Think Hard", budget: 10000},
		{label: "Think Harder", budget: 20000},
		{label: "Ultrathink", budget: 32000},
	}
	if got := options[0].Label; got != "Disabled" {
		t.Fatalf("options[0].Label = %q, want Disabled", got)
	}
	disabled := options[0].Reasoning["thinking"].(map[string]any)
	if got := disabled["type"]; got != "disabled" {
		t.Fatalf("disabled thinking.type = %#v, want disabled", got)
	}
	for i, want := range wants {
		opt := options[i+1]
		if got := opt.Label; got != want.label {
			t.Fatalf("options[%d].Label = %q, want %q", i+1, got, want.label)
		}
		thinking := opt.Reasoning["thinking"].(map[string]any)
		if got := thinking["type"]; got != "enabled" {
			t.Fatalf("%s thinking.type = %#v, want enabled", want.label, got)
		}
		if got := thinking["budget_tokens"]; got != want.budget {
			t.Fatalf("%s budget_tokens = %#v, want %d", want.label, got, want.budget)
		}
	}
}

func TestReasoningLabelMatch(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), config: tuiconfig.Model{DetailRef: "deepseek/deepseek-v4-pro"}, configState: testReasoningConfig("deepseek", "deepseek-v4-pro")}
	mc := tui.configModelsSnapshot()[0]
	mc.Reasoning = deepSeekReasoning("max")
	if got := tui.reasoningDisplay(mc); got != "DeepSeek V4 / Max" {
		t.Fatalf("reasoningDisplay() = %q, want %q", got, "DeepSeek V4 / Max")
	}
}

func TestMiniMaxReasoningLabelMatch(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), config: tuiconfig.Model{DetailRef: "DF/MiniMax-M3"}, configState: testReasoningConfig("DF", "MiniMax-M3")}
	mc := tui.configModelsSnapshot()[0]
	mc.Reasoning = tuiconfig.MiniMaxReasoning()
	if got := tui.reasoningDisplay(mc); got != "MiniMax M3 / Split" {
		t.Fatalf("reasoningDisplay() = %q, want %q", got, "MiniMax M3 / Split")
	}
	if got := mc.Reasoning["reasoning_split"]; got != true {
		t.Fatalf("MiniMaxReasoning()[reasoning_split] = %#v, want true", got)
	}
}

func TestSaveReasoningUpdatesDetailStateImmediately(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), config: tuiconfig.Model{DetailRef: "deepseek/deepseek-v4-pro"}, configState: testReasoningConfig("deepseek", "deepseek-v4-pro")}

	tui.saveReasoning(deepSeekReasoning("max"))
	mc, ok := tui.modelByRef("deepseek/deepseek-v4-pro")
	if !ok {
		t.Fatalf("modelByRef(%q) ok = false, want true", "deepseek/deepseek-v4-pro")
	}
	if got := tui.reasoningDisplay(mc); got != "DeepSeek V4 / Max" {
		t.Fatalf("reasoningDisplay() after save = %q, want %q", got, "DeepSeek V4 / Max")
	}

	tui.saveReasoning(nil)
	mc, ok = tui.modelByRef("deepseek/deepseek-v4-pro")
	if !ok {
		t.Fatalf("modelByRef(%q) after clear ok = false, want true", "deepseek/deepseek-v4-pro")
	}
	if got := tui.reasoningDisplay(mc); got != "" {
		t.Fatalf("reasoningDisplay() after clear = %q, want empty", got)
	}
}

func testReasoningConfig(provider, model string) protocol.ConfigParams {
	return testReasoningConfigWithProtocol(provider, model, "openai_chat")
}

func testReasoningConfigWithProtocol(provider, model, modelProtocol string) protocol.ConfigParams {
	return protocol.ConfigParams{Models: []protocol.ConfigModel{{Provider: provider, Protocol: modelProtocol, Model: model, ContextWindow: 128000, MaxOutputTokens: 8192}}}
}
