package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/alanchenchen/suna/internal/ipc"
)

func (t *TUI) handleCommand(input string) tea.Cmd {
	if t.ipcCli == nil {
		t.messages = append(t.messages, chatMsg{role: "error", content: t.i18n.T("error.not_connected")})
		return nil
	}

	parts := strings.Fields(input)
	if len(parts) == 0 {
		return nil
	}
	cmd := parts[0]

	switch cmd {
	case "/new":
		t.ipcCli.NewSession()
		t.messages = []chatMsg{}
		t.resetConversationStats()
		t.resetPhase()
		t.lastAssistantText = ""
		return nil
	case "/model":
		if len(parts) > 1 {
			return t.switchModelRef(parts[1])
		}
		t.openModelPicker()
		t.syncContent()
		return nil
	case "/memory":
		t.handleMemory(parts)
		return nil
	case "/compact":
		t.messages = append(t.messages, chatMsg{role: "system", content: t.i18n.T("compact.running")})
		t.ipcCli.Compact()
		return nil
	case "/config":
		t.mode = "config"
		t.configFromMode = "chat"
		t.configSetupMode = false
		t.configFormOpen = false
		t.configPage = "home"
		return nil
	case "/help":
		t.prevMode = "chat"
		t.mode = "help"
		t.initHelpPage()
		return nil
	default:
		t.messages = append(t.messages, chatMsg{role: "error", content: t.i18n.Tf("cmd.unknown", cmd)})
	}
	return nil
}

func (t *TUI) switchModelRef(ref string) tea.Cmd {
	if !strings.Contains(ref, "/") && t.providerName != "" {
		ref = t.providerName + "/" + ref
	}
	if _, ok := t.modelByRef(ref); !ok {
		t.messages = append(t.messages, chatMsg{role: "error", content: t.i18n.Tf("cmd.model_not_found", ref)})
		return nil
	}
	t.configState.ActiveModel = ref
	t.modelPickerOpen = false
	t.messages = append(t.messages, chatMsg{role: "system", content: t.i18n.Tf("cmd.model_switched", ref)})
	return t.sendConfigSet(ipc.ConfigSetParams{Action: ipc.ConfigActionActivateModel, ActiveModel: ref})
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
	case "up", "k":
		if t.modelPickerCursor > 0 {
			t.modelPickerCursor--
		}
	case "down", "j":
		if t.modelPickerCursor < len(models)-1 {
			t.modelPickerCursor++
		}
	case "enter":
		return t, t.switchModelRef(models[t.modelPickerCursor].Ref())
	}
	t.syncContent()
	return t, nil
}

func (t *TUI) handleMemory(parts []string) {
	if len(parts) >= 2 && parts[1] == "search" {
		query := strings.Join(parts[2:], " ")
		t.ipcCli.SearchMemory(query, 5)
	} else {
		t.messages = append(t.messages, chatMsg{role: "system", content: t.i18n.T("memory.search_hint")})
	}
}
