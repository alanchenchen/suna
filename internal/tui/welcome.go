package tui

import (
	"fmt"
	"strings"

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

func (t *TUI) updateWelcome(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		t.width, t.height, t.ready = m.Width, m.Height, true
		return t, nil
	case tea.KeyPressMsg:
		items := t.welcomeMenuItems()
		switch m.String() {
		case "ctrl+c":
			t.doQuit()
			return t, tea.Quit
		case "q":
			return t, nil
		case "esc":
			return t, nil
		case "up", "k":
			if t.welcomeCursor > 0 {
				t.welcomeCursor--
			}
			return t, nil
		case "down", "j":
			if t.welcomeCursor < len(items)-1 {
				t.welcomeCursor++
			}
			return t, nil
		case "enter":
			if t.welcomeCursor >= 0 && t.welcomeCursor < len(items) && !items[t.welcomeCursor].disabled {
				return t, t.handleWelcomeAction(items[t.welcomeCursor].action)
			}
		case "n":
			return t, t.handleWelcomeAction(actionNew)
		case "r":
			return t, t.handleWelcomeAction(actionResume)
		case "ctrl+o":
			return t, t.handleWelcomeAction(actionConfig)
		case "?", "f1":
			return t, t.handleWelcomeAction(actionHelp)
		}
	}
	return t, nil
}

func (t *TUI) handleWelcomeAction(action welcomeAction) tea.Cmd {
	switch action {
	case actionNew:
		t.mode = "chat"
		t.messages = []chatMsg{}
		t.resetPhase()
		return t.initChatComponents()
	case actionResume:
		if t.daemonStatus.Sessions == nil || t.daemonStatus.Sessions.LastID == "" {
			return nil
		}
		t.mode = "chat"
		t.messages = []chatMsg{}
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
	w := min(max(54, t.width-12), 78)

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
		sb.WriteString("    " + left + strings.Repeat(" ", max(6, 20-lipgloss.Width(left))) + right + "\n")
	}
	sb.WriteString("\n")
	sb.WriteString("    " + styleBrand.Render("Suna") + "\n")
	sb.WriteString("    " + styleDim.Render(t.tr("tui.welcome.subtitle")) + "\n\n")

	var itemLines []string
	for i, item := range t.welcomeMenuItems() {
		cursor := "  "
		st := lipgloss.NewStyle()
		if item.disabled {
			st = styleDim
		}
		if i == t.welcomeCursor {
			cursor = styleCursor.Render("▶ ")
			if !item.disabled {
				st = styleHL
			}
		}
		line := cursor + st.Render(t.tr(item.labelKey))
		if item.key != "" {
			line += styleDim.Render("  [" + item.key + "]")
		}
		itemLines = append(itemLines, line)
	}
	sb.WriteString("    " + welcomeBoxStyle(w).Render(strings.Join(itemLines, "\n")) + "\n\n")
	sb.WriteString("    " + styleDim.Render(t.tr("tui.welcome.help")) + "\n")
	return sb.String()
}

func welcomeBoxStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().Width(width).Padding(1, 2).Border(lipgloss.RoundedBorder()).BorderForeground(ColorDim)
}

func (t *TUI) welcomeMenuItems() []welcomeItem {
	items := []welcomeItem{{"tui.welcome.new", "N", actionNew, false}}
	if t.daemonStatus.Sessions != nil && t.daemonStatus.Sessions.LastID != "" {
		items = append(items, welcomeItem{"tui.welcome.resume", "R", actionResume, false})
	}
	items = append(items, welcomeItem{"tui.welcome.config", "", actionConfig, false})
	items = append(items, welcomeItem{"tui.welcome.help_menu", "?", actionHelp, false})
	return items
}

func (t *TUI) renderWelcomeInfo() string {
	s := t.daemonStatus
	provider, model := s.Provider, s.Model
	if provider == "" {
		provider = t.providerName
	}
	if model == "" {
		model = t.modelName
	}
	if provider == "" {
		provider = "-"
	}
	if model == "" {
		model = "-"
	}
	rows := []string{fmt.Sprintf("%-8s %s", t.tr("tui.status.model"), styleHL.Render(provider+"/"+model))}
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
