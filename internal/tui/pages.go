package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/alanchenchen/suna/internal/protocol"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
	tuiconfig "github.com/alanchenchen/suna/internal/tui/pages/config"
	helppage "github.com/alanchenchen/suna/internal/tui/pages/help"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
	welcomepage "github.com/alanchenchen/suna/internal/tui/pages/welcome"
)

func (t *TUI) updateWelcome(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		t.width, t.height, t.ready = m.Width, m.Height, true
		t.initWelcomeList()
		return t, nil
	case tea.KeyPressMsg:
		items := t.welcomeMenuItems()
		t.initWelcomeList()
		switch m.String() {
		case "ctrl+c":
			t.doQuit()
			return t, tea.Quit
		case "esc":
			if t.welcomeNewConfirm {
				t.welcomeNewConfirm = false
				t.welcomeNotice = ""
				t.initWelcomeList()
				return t, nil
			}
			if t.welcomeActivePicker {
				t.welcomeActivePicker = false
				t.initWelcomeList()
			}
			return t, nil
		}
		action, handled := t.menu.UpdateKey(m.String(), items)
		if handled {
			return t, t.handleWelcomeAction(action)
		}
	}
	return t, nil
}

func (t *TUI) initWelcomeList() {
	if !t.menu.HasItems() {
		t.menu = welcomepage.New(welcomepage.Deps{Tr: func(key string) string { return t.tr(key) }, Styles: welcomepage.Styles{Cursor: styleCursor, Dim: styleDim, HL: styleHL, Brand: styleBrand}})
	}
	t.menu.SetItems(t.welcomeMenuItems(), t.width)
}

func (t *TUI) handleWelcomeAction(action welcomepage.Action) tea.Cmd {
	switch action {
	case welcomepage.ActionNew:
		if !t.hasConfiguredModel() {
			t.mode = uipage.Config
			t.config.FromMode = uipage.Welcome
			t.config.SetupMode = true
			t.openProviderForm("", nil)
			return t.config.Inputs[t.config.InputFocus].Focus()
		}
		if t.cwdHasActiveSession() {
			t.welcomeNotice = t.tr("tui.welcome.new_active_blocked")
			return nil
		}
		if count := len(t.replaceableCWDSessions()); count > 0 {
			t.welcomeNewConfirm = true
			t.welcomeNotice = t.i18n.Tf("tui.welcome.new_confirm_notice", count)
			t.initWelcomeList()
			return nil
		}
		return t.startNewSession(t.newSessionCmd())
	case welcomepage.ActionConfirmNew:
		t.welcomeNewConfirm = false
		t.welcomeNotice = ""
		return t.startNewSession(t.newReplacingCWDSessionsCmd())
	case welcomepage.ActionCancelNew:
		t.welcomeNewConfirm = false
		t.welcomeNotice = ""
		t.initWelcomeList()
		return nil
	case welcomepage.ActionResume:
		if t.resumeSessionID == "" {
			return nil
		}
		t.mode = uipage.Chat
		t.chat.Messages = []chatMsg{}
		t.chat.DisplayDiscard = chatpage.DisplayDiscardSummary{}
		t.chat.Attachments = nil
		t.currentRunCanControl = false
		t.handoffRole = handoffRoleHost
		t.attachmentStatus = protocol.AttachmentStatusResult{}
		t.resetConversationStats()
		cmd := t.initChatComponents()
		t.resetPhase()
		return tea.Batch(cmd, t.attachSessionCmd(t.resumeSessionID, false))
	case welcomepage.ActionJoinPicker:
		t.welcomeActivePicker = true
		t.initWelcomeList()
		return nil
	case welcomepage.ActionJoin:
		selected := t.menu.SelectedItem()
		sessionID := selected.SessionID
		if sessionID == "" {
			return nil
		}
		t.chat.Messages = []chatMsg{}
		t.chat.DisplayDiscard = chatpage.DisplayDiscardSummary{}
		t.chat.Attachments = nil
		t.currentRunCanControl = false
		t.handoffRole = handoffRoleGuest
		t.attachmentStatus = protocol.AttachmentStatusResult{}
		t.resetConversationStats()
		cmd := t.initChatComponents()
		t.resetPhase()
		return tea.Batch(cmd, t.attachSessionCmd(sessionID, true))
	case welcomepage.ActionBack:
		t.welcomeActivePicker = false
		t.initWelcomeList()
		return nil
	case welcomepage.ActionConfig:
		t.mode = uipage.Config
		t.config.FromMode = uipage.Welcome
		t.config.SetupMode = false
		t.config.FormOpen = false
		t.config.Cursor = 0
		return nil
	case welcomepage.ActionHelp:
		t.prevMode = uipage.Welcome
		t.mode = uipage.Help
		t.initHelpPage()
	}
	return nil
}

func (t *TUI) startNewSession(sessionCmd tea.Cmd) tea.Cmd {
	t.mode = uipage.Chat
	t.chat.Messages = []chatMsg{}
	t.chat.DisplayDiscard = chatpage.DisplayDiscardSummary{}
	t.chat.Attachments = nil
	t.currentRunCanControl = false
	t.handoffRole = handoffRoleHost
	t.attachmentStatus = protocol.AttachmentStatusResult{}
	t.resetConversationStats()
	cmd := t.initChatComponents()
	t.resetPhase()
	return tea.Batch(cmd, sessionCmd)
}

func (t *TUI) viewWelcome() string {
	if !t.menu.HasItems() {
		t.initWelcomeList()
	}
	return welcomepage.RenderView(welcomepage.ViewData{
		Width:         t.width,
		Pet:           renderPet(petIdle, t.width),
		Info:          t.renderWelcomeInfo(),
		Menu:          t.menu.View(),
		HasConfigured: t.hasConfiguredModel(),
	}, welcomepage.ViewDeps{
		Tr:          func(key string) string { return t.tr(key) },
		Brand:       styleBrand,
		Dim:         styleDim,
		HL:          styleHL,
		Box:         lipgloss.NewStyle(),
		BorderColor: ColorDim,
	})
}

func (t *TUI) welcomeMenuItems() []welcomepage.Item {
	noModel := !t.hasConfiguredModel()
	var items []welcomepage.Item
	if noModel {
		items = append(items, welcomepage.Item{LabelKey: "tui.welcome.new", Action: welcomepage.ActionNew})
		items = append(items, welcomepage.Item{LabelKey: "tui.welcome.config", Action: welcomepage.ActionConfig})
		items = append(items, welcomepage.Item{LabelKey: "tui.welcome.help_menu", Action: welcomepage.ActionHelp})
		return items
	}
	if t.welcomeNewConfirm {
		items = append(items, welcomepage.Item{LabelKey: "tui.welcome.new_confirm", Action: welcomepage.ActionConfirmNew})
		items = append(items, welcomepage.Item{LabelKey: "tui.welcome.new_cancel", Action: welcomepage.ActionCancelNew})
		return items
	}
	if t.welcomeActivePicker {
		items = append(items, welcomepage.Item{LabelKey: "tui.welcome.back", DetailKey: "tui.welcome.help", Action: welcomepage.ActionBack})
		for _, session := range t.activeWelcomeSessions() {
			items = append(items, welcomepage.Item{
				LabelKey:  "tui.welcome.join_one",
				Key:       sessionTitle(session),
				CWD:       session.CWD,
				Action:    welcomepage.ActionJoin,
				SessionID: session.ID,
			})
		}
		return items
	}
	if t.resumeSessionID != "" {
		items = append(items, welcomepage.Item{LabelKey: "tui.welcome.resume", Action: welcomepage.ActionResume, SessionID: t.resumeSessionID})
	}
	items = append(items, welcomepage.Item{LabelKey: "tui.welcome.new", Action: welcomepage.ActionNew})
	activeSessions := t.activeWelcomeSessions()
	if len(activeSessions) > 0 {
		items = append(items, welcomepage.Item{LabelKey: "tui.welcome.join", Key: fmt.Sprintf("%d", len(activeSessions)), Action: welcomepage.ActionJoinPicker})
	}
	items = append(items, welcomepage.Item{LabelKey: "tui.welcome.config", Action: welcomepage.ActionConfig})
	items = append(items, welcomepage.Item{LabelKey: "tui.welcome.help_menu", Action: welcomepage.ActionHelp})
	return items
}

func (t *TUI) activeWelcomeSessions() []protocol.SessionInfo {
	out := make([]protocol.SessionInfo, 0)
	for _, session := range t.sessions {
		if sessionActive(session) && session.ID != t.currentSession.ID {
			out = append(out, session)
		}
	}
	return out
}

func (t *TUI) renderWelcomeInfo() string {
	s := t.daemonStatus
	provider, model := "-", "-"
	// Welcome advertises the configured default for new sessions. It must not
	// borrow the current session's model, which can legitimately differ.
	if mc, ok := t.activeConfigModel(); ok {
		provider, model = mc.Provider, mc.Model
	}
	rows := []string{
		fmt.Sprintf("%-8s %s", t.tr("tui.status.version"), styleHL.Render(appVersion())),
		fmt.Sprintf("%-8s %s", t.tr("tui.status.model"), styleHL.Render(provider+"/"+model)),
	}
	if mc, ok := t.activeConfigModel(); ok {
		if reasoning := t.reasoningDisplay(mc); reasoning != "" {
			rows = append(rows, fmt.Sprintf("%-8s %s", t.tr("tui.config.reasoning"), styleHL.Render(reasoning)))
		}
	}
	if s.UsageToday != nil {
		usage := fmt.Sprintf("↑%s ↓%s", fmtTok(s.UsageToday.InputTokens), fmtTok(s.UsageToday.OutputTokens))
		rows = append(rows, fmt.Sprintf("%-8s %s", t.tr("tui.status.usage"), styleHL.Render(usage)))
	}
	if s.Uptime != "" {
		rows = append(rows, fmt.Sprintf("%-8s %s", t.tr("tui.status.uptime"), styleHL.Render(s.Uptime)))
	}
	if s.Memory != nil {
		rows = append(rows, fmt.Sprintf("%-8s %s", t.tr("tui.status.memory"), styleHL.Render(fmt.Sprintf("%d active · %d core · %d queued", s.Memory.Active, s.Memory.Core, s.Memory.Queued))))
	}
	rows = append(rows, fmt.Sprintf("%-8s %s", t.tr("tui.status.guard"), styleHL.Render(t.welcomeGuardStatus())))
	rows = append(rows, fmt.Sprintf("%-8s %s", t.tr("tui.status.workspace"), styleHL.Render(t.welcomeWorkspaceStatus())))
	if t.welcomeNotice != "" {
		rows = append(rows, fmt.Sprintf("%-8s %s", t.tr("tui.status.notice"), styleError.Render(t.welcomeNotice)))
	}
	return strings.Join(rows, "\n")
}

func (t *TUI) welcomeGuardStatus() string {
	mode := tuiconfig.NormalizeGuardMode(t.configState.GuardMode)
	return mode
}

func (t *TUI) welcomeWorkspaceStatus() string {
	if strings.TrimSpace(t.configState.Workspace) == "" {
		return t.tr("tui.config.disabled")
	}
	return t.tr("tui.config.configured")
}

func (t *TUI) initHelpPage() {
	t.help = helppage.New()
	t.layoutHelp()
	t.help.SetContent(t.renderHelpContent())
}

func (t *TUI) updateHelp(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(tea.WindowSizeMsg); ok {
		t.width, t.height, t.ready = m.Width, m.Height, true
		t.layoutHelp()
		t.help.SetContent(t.renderHelpContent())
		return t, nil
	}
	action, cmd := t.help.Update(msg)
	switch action {
	case helppage.ActionQuit:
		t.doQuit()
		return t, tea.Quit
	case helppage.ActionBack:
		t.mode = t.prevMode
		if t.mode == uipage.None {
			t.mode = uipage.Welcome
		}
		return t, nil
	default:
		return t, cmd
	}
}

func (t *TUI) layoutHelp() {
	if t.width <= 0 || t.height <= 0 {
		return
	}
	t.help.SetSize(t.width, t.height)
}

func (t *TUI) viewHelp() string {
	if !t.help.Initialized() {
		t.initHelpPage()
	}
	header := renderHeader(t.tr("tui.help.title"), "[Esc] "+t.tr("tui.key.back"), t.width)
	return header + "\n" + t.help.View()
}

func (t *TUI) renderHelpOverlay(width int) string {
	return helppage.RenderOverlay(width, t.helpCommands(), t.helpRenderDeps())
}

func (t *TUI) renderHelpContent() string {
	return helppage.RenderContent(t.helpCommands(), t.helpRenderDeps())
}

func (t *TUI) helpCommands() []helppage.Command {
	commands := chatpage.AllCommands()
	out := make([]helppage.Command, 0, len(commands))
	for _, c := range commands {
		out = append(out, helppage.Command{Cmd: c.Cmd, DescKey: c.DescKey})
	}
	return out
}

func (t *TUI) helpRenderDeps() helppage.RenderDeps {
	return helppage.RenderDeps{
		Tr:    func(key string) string { return t.tr(key) },
		HL:    styleHL,
		Brand: styleBrand,
		Dim:   styleDim,
		Box:   boxStyle,
	}
}
