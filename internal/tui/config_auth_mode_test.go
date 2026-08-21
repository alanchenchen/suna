package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	coreconfig "github.com/alanchenchen/suna/internal/config"
	tuiconfig "github.com/alanchenchen/suna/internal/tui/pages/config"
)

func TestProviderFormAuthModeIsAControlledAnthropicChoice(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 100}
	tui.openProviderForm("provider-a/model", &tuiconfig.ModelConfig{
		Provider:  "provider-a",
		Protocol:  coreconfig.ModelProtocolAnthropic,
		AuthMode:  coreconfig.AuthModeBearer,
		Model:     "model",
		BaseURL:   "https://api.example.com",
		HasAPIKey: true,
	})
	tui.config.InputFocus = tuiconfig.ProviderFormAuthModeIndex

	plain := stripANSIForTest(tui.viewProviderForm())
	if !strings.Contains(plain, "Auth Mode: ‹ Bearer ›") {
		t.Fatalf("provider form = %q, want controlled Bearer choice", plain)
	}

	tui.updateProviderForm(tea.KeyPressMsg{Code: tea.KeyRight})
	if got := tui.config.Inputs[tuiconfig.ProviderFormAuthModeIndex].Value(); got != string(coreconfig.AuthModeBoth) {
		t.Fatalf("auth mode after Right = %q, want both", got)
	}
	plain = stripANSIForTest(tui.viewProviderForm())
	if !strings.Contains(plain, "Auth Mode: ‹ Both ›") {
		t.Fatalf("provider form = %q, want Both choice", plain)
	}
}

func TestProviderFormHidesAndClearsAuthModeOutsideAnthropic(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 100}
	tui.openProviderForm("provider-a/model", &tuiconfig.ModelConfig{
		Provider: "provider-a",
		Protocol: coreconfig.ModelProtocolAnthropic,
		AuthMode: coreconfig.AuthModeBoth,
		Model:    "model",
		BaseURL:  "https://api.example.com",
	})
	tui.config.InputFocus = tuiconfig.ProviderFormAuthModeIndex

	tui.cycleProviderProtocol(1)
	if got := tui.config.Inputs[tuiconfig.ProviderFormProtocolIndex].Value(); got != string(coreconfig.ModelProtocolOpenAIChat) {
		t.Fatalf("protocol after cycle = %q, want openai_chat", got)
	}
	if got := tui.providerFormValues().AuthMode; got != "" {
		t.Fatalf("saved auth mode = %q, want default outside anthropic", got)
	}
	if got := tui.config.InputFocus; got == tuiconfig.ProviderFormAuthModeIndex {
		t.Fatalf("InputFocus = %d, should move away from hidden auth mode", got)
	}
	plain := stripANSIForTest(tui.viewProviderForm())
	if strings.Contains(plain, "Auth Mode:") {
		t.Fatalf("provider form = %q, should hide auth mode outside anthropic", plain)
	}
}
