package chat

import "github.com/alanchenchen/suna/internal/protocol"

type SkillAction struct {
	Name    string
	Enabled bool
}

func (m *Model) OpenSkillsOverlay() {
	m.SkillsOverlayOpen = true
	m.SkillsLoading = true
	m.SkillsError = ""
	m.SkillsCursor = ClampSkillCursor(m.SkillsCursor, len(m.Skills))
	m.MCPOverlayOpen = false
}

func (m *Model) CloseSkillsOverlay() {
	m.SkillsOverlayOpen = false
	m.SkillsError = ""
}

func (m *Model) MoveSkillsCursor(delta int) {
	m.SkillsCursor = ClampSkillCursor(m.SkillsCursor+delta, len(m.Skills))
}

func (m *Model) SelectSkill(cannotToggleMessage string) (SkillAction, bool) {
	if len(m.Skills) == 0 || m.SkillsCursor < 0 || m.SkillsCursor >= len(m.Skills) {
		return SkillAction{}, false
	}
	item := m.Skills[m.SkillsCursor]
	if !SkillCanToggle(item) {
		m.SkillsError = cannotToggleMessage
		return SkillAction{}, false
	}
	return SkillAction{Name: item.Name, Enabled: !SkillIsActive(item)}, true
}

func (m *Model) SetSkills(skills []protocol.SkillInfo) {
	m.Skills = skills
	m.SkillsLoading = false
	m.SkillsError = ""
	m.SkillsCursor = ClampSkillCursor(m.SkillsCursor, len(m.Skills))
	if m.SkillsCursor < m.SkillsScroll {
		m.SkillsScroll = m.SkillsCursor
	}
}

func SkillSummaryCounts(skills []protocol.SkillInfo) (active, issues int) {
	for _, s := range skills {
		if SkillIsActive(s) {
			active++
		}
		if SkillHasIssue(s) {
			issues++
		}
	}
	return
}

func SkillIsActive(s protocol.SkillInfo) bool {
	return s.Enabled && s.Valid
}

func SkillCanToggle(s protocol.SkillInfo) bool {
	return SkillIsActive(s) || s.Valid
}

func SkillHasIssue(s protocol.SkillInfo) bool {
	return len(s.Reasons) > 0 || s.Error != "" || !s.Valid
}

func ClampSkillCursor(cursor, n int) int {
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
