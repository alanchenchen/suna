package tui

import (
	"fmt"
	"io"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type welcomeAction int

const (
	actionNew welcomeAction = iota
	actionResume
	actionConfig
	actionHelp
)

type welcomeItem struct {
	labelKey string
	key      string
	action   welcomeAction
	disabled bool
}

func (i welcomeItem) FilterValue() string { return i.labelKey }

type welcomeDelegate struct{ t *TUI }

func (d welcomeDelegate) Height() int                         { return 1 }
func (d welcomeDelegate) Spacing() int                        { return 0 }
func (d welcomeDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }
func (d welcomeDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	wi, ok := item.(welcomeItem)
	if !ok {
		return
	}
	cursor := "  "
	st := lipgloss.NewStyle()
	if wi.disabled {
		st = styleDim
	}
	if index == m.Index() {
		cursor = styleCursor.Render("▶ ")
		if !wi.disabled {
			st = styleHL
		}
	}
	line := cursor + st.Render(d.t.tr(wi.labelKey))
	if wi.key != "" {
		line += styleDim.Render("  [" + wi.key + "]")
	}
	fmt.Fprint(w, line)
}

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
			return t, nil
		case "up", "k":
			if t.welcomeCursor > 0 {
				t.welcomeCursor--
			}
			t.menu.Select(t.welcomeCursor)
			return t, nil
		case "down", "j":
			if t.welcomeCursor < len(items)-1 {
				t.welcomeCursor++
			}
			t.menu.Select(t.welcomeCursor)
			return t, nil
		case "enter":
			if t.welcomeCursor >= 0 && t.welcomeCursor < len(items) && !items[t.welcomeCursor].disabled {
				return t, t.handleWelcomeAction(items[t.welcomeCursor].action)
			}
		}
	}
	return t, nil
}

func (t *TUI) initWelcomeList() {
	items := t.welcomeMenuItems()
	listItems := make([]list.Item, 0, len(items))
	for _, item := range items {
		listItems = append(listItems, item)
	}
	w := max(20, min(max(54, t.width-14), 84)-6)
	h := max(3, len(items))
	t.menu = list.New(listItems, welcomeDelegate{t: t}, w, h)
	t.menu.SetShowTitle(false)
	t.menu.SetShowStatusBar(false)
	t.menu.SetShowPagination(false)
	t.menu.SetFilteringEnabled(false)
	t.menu.SetShowHelp(false)
	if t.welcomeCursor >= len(items) {
		t.welcomeCursor = max(0, len(items)-1)
	}
	t.menu.Select(t.welcomeCursor)
}

func (t *TUI) handleWelcomeAction(action welcomeAction) tea.Cmd {
	switch action {
	case actionNew:
		if !t.hasConfiguredModel() {
			t.mode = "config"
			t.configFromMode = "welcome"
			t.configSetupMode = true
			t.openProviderForm("", nil)
			return t.configInputs[t.configInputFocus].Focus()
		}
		t.mode = "chat"
		t.messages = []chatMsg{}
		t.resetConversationStats()
		t.resetPhase()
		if t.ipcCli != nil {
			go t.ipcCli.NewSession()
		}
		return t.initChatComponents()
	case actionResume:
		if t.daemonStatus.Sessions == nil || t.daemonStatus.Sessions.LastID == "" {
			return nil
		}
		t.mode = "chat"
		t.messages = []chatMsg{}
		t.resetConversationStats()
		t.resetPhase()
		go func() { t.ipcCli.RestoreSession() }()
		return t.initChatComponents()
	case actionConfig:
		t.mode = "config"
		t.configFromMode = "welcome"
		t.configSetupMode = false
		t.configFormOpen = false
		t.configCursor = 0
		return nil
	case actionHelp:
		t.prevMode = "welcome"
		t.mode = "help"
		t.initHelpPage()
	}
	return nil
}

func (t *TUI) viewWelcome() string {
	var sb strings.Builder
	w := min(max(54, t.width-14), 84)
	leftPad := max(2, (t.width-w)/2)
	pad := strings.Repeat(" ", leftPad)

	sb.WriteString("\n")
	pet := strings.Split(renderPet(petIdle, t.width), "\n")
	info := strings.Split(t.renderWelcomeInfo(), "\n")
	rows := max(len(pet), len(info))
	for i := 0; i < rows; i++ {
		left, right := "", ""
		if i < len(pet) {
			left = pet[i]
		}
		if i < len(info) {
			right = info[i]
		}
		sb.WriteString(pad + left + strings.Repeat(" ", max(8, 24-lipgloss.Width(left))) + right + "\n")
	}
	sb.WriteString("\n")
	sb.WriteString(pad + styleBrand.Render("Suna") + "\n")
	sb.WriteString(pad + styleDim.Render(t.tr("tui.welcome.subtitle")) + "\n")
	if !t.hasConfiguredModel() {
		sb.WriteString("\n" + pad + styleHL.Render(t.tr("tui.welcome.setup_hint")) + "\n")
	}
	sb.WriteString("\n")

	if len(t.menu.Items()) == 0 {
		t.initWelcomeList()
	}
	sb.WriteString(indentLines(welcomeBoxStyle(w).Render(strings.TrimRight(t.menu.View(), "\n")), pad) + "\n\n")
	sb.WriteString(pad + styleDim.Render(t.tr("tui.welcome.help")) + "\n")
	return sb.String()
}

func welcomeBoxStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().Width(width).Padding(1, 2).Border(lipgloss.RoundedBorder()).BorderForeground(ColorDim)
}

func (t *TUI) welcomeMenuItems() []welcomeItem {
	noModel := !t.hasConfiguredModel()
	items := []welcomeItem{{"tui.welcome.new", "", actionNew, false}}
	if noModel {
		items = append(items, welcomeItem{"tui.welcome.config", "", actionConfig, false})
		items = append(items, welcomeItem{"tui.welcome.help_menu", "", actionHelp, false})
		return items
	}
	if !noModel && t.daemonStatus.Sessions != nil && t.daemonStatus.Sessions.LastID != "" {
		items = append(items, welcomeItem{"tui.welcome.resume", "", actionResume, false})
	}
	items = append(items, welcomeItem{"tui.welcome.config", "", actionConfig, false})
	items = append(items, welcomeItem{"tui.welcome.help_menu", "", actionHelp, false})
	return items
}

func (t *TUI) renderWelcomeInfo() string {
	s := t.daemonStatus
	provider, model := t.activeProviderModel()
	if provider == "" {
		provider = "-"
	}
	if model == "" {
		model = "-"
	}
	rows := []string{
		fmt.Sprintf("%-8s %s", t.tr("tui.status.version"), styleHL.Render(appVersion)),
		fmt.Sprintf("%-8s %s", t.tr("tui.status.model"), styleHL.Render(provider+"/"+model)),
	}
	if s.UsageToday != nil {
		usage := fmt.Sprintf("↑%s ↓%s", fmtTok(s.UsageToday.InputTokens), fmtTok(s.UsageToday.OutputTokens))
		rows = append(rows, fmt.Sprintf("%-8s %s", t.tr("tui.status.usage"), styleHL.Render(usage)))
	}
	if s.Uptime != "" {
		rows = append(rows, fmt.Sprintf("%-8s %s", t.tr("tui.status.uptime"), styleHL.Render(s.Uptime)))
	}
	if s.Memory != nil {
		rows = append(rows, fmt.Sprintf("%-8s %s", t.tr("tui.status.memory"), styleHL.Render(fmt.Sprintf("%d ep · %d ent", s.Memory.Episodes, s.Memory.Entities))))
	}
	if s.Sessions != nil {
		rows = append(rows, fmt.Sprintf("%-8s %s", t.tr("tui.status.session"), styleHL.Render(fmt.Sprintf("%d active · %d done", s.Sessions.Active, s.Sessions.Completed))))
	}
	return strings.Join(rows, "\n")
}
