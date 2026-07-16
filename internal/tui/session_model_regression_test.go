package tui

import (
	"testing"

	"github.com/alanchenchen/suna/internal/protocol"
)

func TestSwitchModelRefShortNameUsesCurrentSessionProvider(t *testing.T) {
	tui := &TUI{
		i18n:           newTranslator(LocaleEN),
		providerName:   "openai",
		currentSession: protocol.SessionInfo{ID: "session-1", ModelRef: "anthropic/claude-opus"},
		configState: protocol.ConfigParams{Models: []protocol.ConfigModel{
			{Provider: "openai", Model: "gpt-4o"},
			{Provider: "anthropic", Model: "claude-sonnet"},
		}},
	}

	if cmd := tui.switchModelRef("claude-sonnet"); cmd == nil {
		t.Fatal("switchModelRef short name returned nil; want session provider model update command")
	}
}

func TestAttachedSessionModelConfigSurvivesDefaultModelUpdates(t *testing.T) {
	defaultModel := protocol.ConfigModel{Provider: "openai", Model: "gpt-default", ContextWindow: 128000}
	sessionModel := protocol.ConfigModel{Provider: "anthropic", Model: "claude-session", ContextWindow: 200000}
	tui := &TUI{
		i18n: newTranslator(LocaleEN),
		configState: protocol.ConfigParams{
			ActiveModel: "openai/gpt-default",
			Models:      []protocol.ConfigModel{defaultModel, sessionModel},
		},
	}

	tui.applySessionSnapshot(protocol.SessionSnapshot{Session: protocol.SessionInfo{ID: "session-1", ModelRef: "anthropic/claude-session"}})
	if got := tui.contextWindow; got != sessionModel.ContextWindow {
		t.Fatalf("snapshot context window = %d, want session model %d", got, sessionModel.ContextWindow)
	}

	tui.handleDaemonFullStatusNotification(protocol.DaemonStatusParams{
		Provider:      defaultModel.Provider,
		Model:         defaultModel.Model,
		ContextWindow: defaultModel.ContextWindow,
	})
	if got := tui.contextWindow; got != sessionModel.ContextWindow {
		t.Fatalf("daemon status context window = %d, want session model %d", got, sessionModel.ContextWindow)
	}

	tui.setActiveModelRef("openai/gpt-default")
	if got := tui.contextWindow; got != sessionModel.ContextWindow {
		t.Fatalf("active default switch context window = %d, want session model %d", got, sessionModel.ContextWindow)
	}

	tui.handleConfigStateNotification(protocol.ConfigParams{
		ActiveModel: "openai/gpt-default",
		Models:      []protocol.ConfigModel{defaultModel, sessionModel},
	})
	if got := tui.contextWindow; got != sessionModel.ContextWindow {
		t.Fatalf("config update context window = %d, want session model %d", got, sessionModel.ContextWindow)
	}
	if got := tui.providerName + "/" + tui.modelName; got != "anthropic/claude-session" {
		t.Fatalf("display model = %q, want current session model", got)
	}
}
