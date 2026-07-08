package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
)

func TestChatPrintableKeyUpdatesTextarea(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24, ready: true}
	tui.initChatComponents()

	_, cmd := tui.updateChat(tea.KeyPressMsg(tea.Key{Text: "你", Code: '你'}))
	if got := tui.chat.Textarea.Value(); got != "你" {
		t.Fatalf("textarea.Value() = %q, want %q", got, "你")
	}
	if cmd == nil {
		t.Fatalf("cmd = nil, want textarea update command")
	}
}

func TestChatPrintableKeyUpdatesCommandSuggestions(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24, ready: true}
	tui.initChatComponents()

	tui.updateChat(tea.KeyPressMsg(tea.Key{Text: "/", Code: '/'}))
	if got := tui.chat.Textarea.Value(); got != "/" {
		t.Fatalf("textarea.Value() = %q, want /", got)
	}
	if len(tui.chat.CmdSuggestions) == 0 {
		t.Fatalf("CmdSuggestions empty after typing slash")
	}
}

func TestSelectionModeIgnoresWheelAsHistoryKeys(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), mode: uipage.Chat, ready: true, width: 80, height: 24}
	tui.initChatComponents()
	tui.chat.Messages = []chatpage.Msg{{Role: "user", Content: "previous prompt"}}
	tui.selectionMode = true

	_, _ = tui.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyUp}))
	if got := tui.chat.Textarea.Value(); got != "" {
		t.Fatalf("Textarea.Value() = %q, want empty while selection mode ignores up", got)
	}
	if tui.selectionMode != true {
		t.Fatal("selectionMode changed after ignored up key")
	}

	_, _ = tui.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if tui.selectionMode {
		t.Fatal("selectionMode = true after Esc, want false")
	}
}
