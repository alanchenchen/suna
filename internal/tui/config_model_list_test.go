package tui

import (
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/protocol"
	tuiconfig "github.com/alanchenchen/suna/internal/tui/pages/config"
)

func TestAddProviderModelOpensFormDirectly(t *testing.T) {
	tui := &TUI{
		i18n:   newTranslator(LocaleEN),
		config: tuiconfig.Model{Page: "models"},
	}
	rows := []tuiconfig.Row{{Kind: "add_provider_model"}}

	if cmd := tui.handleConfigAction(rows); cmd == nil {
		t.Fatalf("handleConfigAction returned nil command")
	}
	if !tui.config.FormOpen {
		t.Fatalf("provider form is not open")
	}
	if got := tui.config.Inputs[tuiconfig.ProviderFormProtocolIndex].Value(); got != "openai_chat" {
		t.Fatalf("protocol = %q, want openai_chat", got)
	}
}

func TestConfigModelDefaultMarkerDoesNotUseSelectionRail(t *testing.T) {
	applyTheme(ThemeDark)
	t.Cleanup(func() { applyTheme(ThemeDark) })

	tui := &TUI{
		i18n:   newTranslator(LocaleEN),
		width:  100,
		config: tuiconfig.Model{Page: "models", Cursor: 0},
		configState: protocol.ConfigParams{
			ActiveModel: "openai/gpt-default",
			Models: []protocol.ConfigModel{
				{Provider: "openai", Model: "gpt-default", Protocol: "openai_chat", BaseURL: "https://example.test", ContextWindow: 128000, MaxOutputTokens: 8192, HasAPIKey: true},
				{Provider: "openai", Model: "gpt-other", Protocol: "openai_chat", BaseURL: "https://example.test", ContextWindow: 128000, MaxOutputTokens: 8192, HasAPIKey: true},
			},
		},
	}

	view := tui.viewConfigPage()
	plain := stripANSIForTest(view)
	if !strings.Contains(plain, "active") {
		t.Fatalf("view = %q, want inline active badge", plain)
	}
	if strings.Contains(plain, "▌") {
		t.Fatalf("view = %q, must not render an active-model rail", plain)
	}
}
