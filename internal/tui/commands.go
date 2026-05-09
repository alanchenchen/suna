package tui

import "strings"

func (t *TUI) handleCommand(input string) {
	if t.ipcCli == nil {
		t.messages = append(t.messages, chatMsg{role: "error", content: t.i18n.T("error.not_connected")})
		return
	}

	parts := strings.Fields(input)
	if len(parts) == 0 {
		return
	}
	cmd := parts[0]

	switch cmd {
	case "/new":
		t.ipcCli.NewSession()
		t.messages = []chatMsg{}
		t.resetPhase()
		t.lastAssistantText = ""
	case "/model":
		if len(parts) > 1 {
			t.messages = append(t.messages, chatMsg{role: "system", content: t.i18n.T("cmd.model_switch_hint")})
		} else {
			t.messages = append(t.messages, chatMsg{role: "system", content: t.i18n.T("status.waiting_llm")})
		}
	case "/memory":
		t.handleMemory(parts)
	case "/compact":
		t.ipcCli.Compact()
	case "/config":
		t.mode = "config"
		t.configFromMode = "chat"
		t.configSetupMode = false
		t.configFormOpen = false
	case "/help":
		t.prevMode = "chat"
		t.mode = "help"
		t.initHelpPage()
	default:
		t.messages = append(t.messages, chatMsg{role: "error", content: t.i18n.Tf("cmd.unknown", cmd)})
	}
}

func (t *TUI) handleMemory(parts []string) {
	if len(parts) >= 2 && parts[1] == "search" {
		query := strings.Join(parts[2:], " ")
		t.ipcCli.SearchMemory(query, 5)
	} else {
		t.messages = append(t.messages, chatMsg{role: "system", content: t.i18n.T("memory.search_hint")})
	}
}
