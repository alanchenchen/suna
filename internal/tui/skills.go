package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/alanchenchen/suna/internal/protocol"
)

func (t *TUI) handleSkills(parts []string) tea.Cmd {
	if len(parts) != 1 {
		t.appendNonToolMessage(chatMsg{role: "system", content: t.tr("tui.skills.usage")})
		return nil
	}
	t.skillsOverlayOpen = true
	t.skillsLoading = true
	t.skillsError = ""
	t.skillsCursor = clampSkillCursor(t.skillsCursor, len(t.skills))
	return t.listSkillsCmd()
}

func (t *TUI) updateSkillsOverlay(ks string) (tea.Model, tea.Cmd) {
	switch ks {
	case "esc":
		t.skillsOverlayOpen = false
		t.skillsError = ""
		return t, t.syncInputFocus()
	case "up":
		if t.skillsCursor > 0 {
			t.skillsCursor--
		}
		return t, nil
	case "down":
		if t.skillsCursor < len(t.skills)-1 {
			t.skillsCursor++
		}
		return t, nil
	case "enter", " ":
		if len(t.skills) == 0 || t.skillsCursor < 0 || t.skillsCursor >= len(t.skills) {
			return t, nil
		}
		item := t.skills[t.skillsCursor]
		if !skillCanToggle(item) {
			t.skillsError = t.tr("tui.skills.cannot_toggle")
			return t, nil
		}
		return t, t.setSkillOverlayCmd(item.Name, !skillIsActive(item))
	}
	return t, nil
}

func (t *TUI) setSkillOverlayCmd(name string, enabled bool) tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification("config.error", errNotConnected(t))
		}
		if err := t.localCli.SetSkill(protocol.SkillSetParams{Name: strings.TrimSpace(name), Enabled: enabled}); err != nil {
			return ipcErrorNotification("config.error", err)
		}
		if err := t.localCli.ListSkills(); err != nil {
			return ipcErrorNotification("config.error", err)
		}
		return nil
	}
}

func (t *TUI) renderSkillsOverlay(width int) string {
	w := max(48, min(82, width-4))
	inner := max(28, w-8)
	bodyHeight := max(4, min(14, t.overlayMaxHeight()-8))
	var body []string
	if t.skillsLoading && len(t.skills) == 0 {
		body = append(body, styleDim.Render(t.tr("tui.skills.loading")))
	} else if len(t.skills) == 0 {
		body = append(body, styleDim.Render(t.tr("tui.skills.empty")))
	} else {
		for i, s := range t.skills {
			body = append(body, t.renderSkillRow(i, s, inner))
		}
	}
	body, start, total := scrollWindow(body, bodyHeight, &t.skillsScroll)
	active, issues := skillSummaryCounts(t.skills)
	title := t.tr("tui.skills.title", active, len(t.skills), issues)
	lines := []string{styleHL.Render(title), ""}
	lines = append(lines, body...)
	if t.skillsError != "" {
		lines = append(lines, "", styleError.Render(t.skillsError))
	}
	lines = append(lines, "", styleDim.Render(t.skillsHelpText(start, bodyHeight, total)))
	return boxStyle.Width(w).Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func (t *TUI) renderSkillRow(i int, s protocol.SkillInfo, width int) string {
	cursor := "  "
	nameStyle := lipgloss.NewStyle()
	if i == t.skillsCursor {
		cursor = styleCursor.Render("▶ ")
		nameStyle = styleHL
	}
	active := skillIsActive(s)
	mark := skillActiveMark(active)
	status := t.tr("tui.skills.inactive")
	statusStyle := styleDim
	if active {
		status = t.tr("tui.skills.active")
		statusStyle = styleToolOk
	}
	name := truncateDisplay(s.Name, max(12, width-24))
	line := fmt.Sprintf("%s%s %-24s %-10s", cursor, mark, nameStyle.Render(name), statusStyle.Render(status))
	if skillHasIssue(s) {
		line += "  " + styleTool.Render(skillIssueText(t, s))
	}
	return line
}

func (t *TUI) skillsHelpText(start, height, total int) string {
	text := t.tr("tui.skills.help")
	if total > height {
		text += fmt.Sprintf(" · %d-%d/%d", start+1, min(total, start+height), total)
	}
	return text
}

func skillSummaryCounts(skills []protocol.SkillInfo) (active, issues int) {
	for _, s := range skills {
		if skillIsActive(s) {
			active++
		}
		if skillHasIssue(s) {
			issues++
		}
	}
	return
}

func skillIsActive(s protocol.SkillInfo) bool {
	return s.Enabled && s.Valid
}

func skillCanToggle(s protocol.SkillInfo) bool {
	return skillIsActive(s) || s.Valid
}

func skillHasIssue(s protocol.SkillInfo) bool {
	return len(s.Reasons) > 0 || strings.TrimSpace(s.Error) != "" || !s.Valid
}

func skillIssueText(t *TUI, s protocol.SkillInfo) string {
	if strings.TrimSpace(s.Error) != "" {
		return t.tr("tui.skills.issue_error")
	}
	if len(s.Reasons) > 0 {
		return t.tr("tui.skills.issue_reasons", len(s.Reasons))
	}
	return t.tr("tui.skills.issue_review")
}

func skillActiveMark(active bool) string {
	if active {
		return styleToolOk.Render("●")
	}
	return styleDim.Render("○")
}

func truncateDisplay(s string, maxWidth int) string {
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	if maxWidth <= 1 {
		return "…"
	}
	out := ""
	for _, r := range s {
		if lipgloss.Width(out+string(r)+"…") > maxWidth {
			break
		}
		out += string(r)
	}
	return out + "…"
}

func clampSkillCursor(cursor, n int) int {
	if n <= 0 {
		return 0
	}
	if cursor < 0 {
		return 0
	}
	if cursor >= n {
		return n - 1
	}
	return cursor
}
