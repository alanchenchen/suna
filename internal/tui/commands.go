package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/alanchenchen/suna/internal/protocol"
)

func (t *TUI) handleCommand(input string) tea.Cmd {
	if t.localCli == nil {
		t.appendNonToolMessage(chatMsg{role: "error", content: t.i18n.T("error.not_connected")})
		t.scrollToBottomOnNextSync()
		return nil
	}

	parts := strings.Fields(input)
	if len(parts) == 0 {
		return nil
	}
	cmd := parts[0]
	t.scrollToBottomOnNextSync()

	switch cmd {
	case "/new":
		t.messages = []chatMsg{}
		t.attachments = nil
		t.resetConversationStats()
		t.resetPhase()
		t.lastAssistantText = ""
		return t.newSessionCmd()
	case "/model":
		if len(parts) > 1 {
			return t.switchModelRef(parts[1])
		}
		t.openModelPicker()
		t.syncContent()
		return nil
	case "/memory":
		return t.handleMemory(parts)
	case "/compact":
		t.appendNonToolMessage(chatMsg{role: "system", content: t.i18n.T("compact.running")})
		return t.compactCmd()
	case "/config":
		t.mode = "config"
		t.configFromMode = "chat"
		t.configSetupMode = false
		t.configFormOpen = false
		t.configPage = "home"
		return nil
	case "/skills":
		return t.handleSkills(parts)
	case "/help":
		t.prevMode = "chat"
		t.mode = "help"
		t.initHelpPage()
		return nil
	default:
		t.appendNonToolMessage(chatMsg{role: "error", content: t.i18n.Tf("cmd.unknown", cmd)})
	}
	return nil
}

func (t *TUI) isRegisteredSlashCommand(input string) bool {
	input = strings.TrimSpace(input)
	if input == "" {
		return false
	}
	for _, spec := range t.allCommands() {
		if input == spec.cmd || strings.HasPrefix(input, spec.cmd+" ") {
			return true
		}
		if !strings.Contains(spec.cmd, " ") {
			continue
		}
		parts := strings.Fields(input)
		if len(parts) > 0 && parts[0] == strings.Fields(spec.cmd)[0] {
			return strings.HasPrefix(spec.cmd, input)
		}
	}
	return false
}

func (t *TUI) switchModelRef(ref string) tea.Cmd {
	if !strings.Contains(ref, "/") && t.providerName != "" {
		ref = t.providerName + "/" + ref
	}
	if _, ok := t.modelByRef(ref); !ok {
		t.appendNonToolMessage(chatMsg{role: "error", content: t.i18n.Tf("cmd.model_not_found", ref)})
		return nil
	}
	t.setActiveModelRef(ref)
	t.modelPickerOpen = false
	t.appendNonToolMessage(chatMsg{role: "system", content: t.i18n.Tf("cmd.model_switched", ref)})
	return t.sendConfigSet(protocol.ConfigSetParams{Action: protocol.ConfigActionActivateModel, ActiveModel: ref})
}

func (t *TUI) openModelPicker() {
	t.modelPickerOpen = true
	t.modelPickerCursor = 0
	for i, mc := range t.configModelsSnapshot() {
		if mc.Ref() == t.configState.ActiveModel {
			t.modelPickerCursor = i
			break
		}
	}
}

func (t *TUI) updateModelPicker(key string) (tea.Model, tea.Cmd) {
	models := t.configModelsSnapshot()
	if len(models) == 0 {
		t.modelPickerOpen = false
		return t, nil
	}
	switch key {
	case "esc":
		t.modelPickerOpen = false
	case "up":
		if t.modelPickerCursor > 0 {
			t.modelPickerCursor--
		}
	case "down":
		if t.modelPickerCursor < len(models)-1 {
			t.modelPickerCursor++
		}
	case "enter":
		return t, t.switchModelRef(models[t.modelPickerCursor].Ref())
	}
	t.syncContent()
	return t, nil
}

func (t *TUI) handleMemory(parts []string) tea.Cmd {
	if len(parts) == 1 {
		return t.listMemoryCmd()
	}
	t.appendNonToolMessage(chatMsg{role: "system", content: t.i18n.T("memory.list_hint")})
	return nil
}
