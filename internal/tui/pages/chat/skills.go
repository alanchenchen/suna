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
	m.MCPOverlayOpen = false
	m.ModelPickerOpen = false
}

func (m *Model) CloseSkillsOverlay() {
	m.SkillsOverlayOpen = false
	m.SkillsError = ""
}

func (m *Model) SelectSkill(cannotToggleMessage string) (SkillAction, bool) {
	selected, ok := m.SkillsList.Selected()
	if !ok {
		return SkillAction{}, false
	}
	row, ok := selected.(skillItem)
	if !ok || !SkillCanToggle(row.skill) {
		m.SkillsError = cannotToggleMessage
		return SkillAction{}, false
	}
	return SkillAction{Name: row.skill.Name, Enabled: !SkillIsActive(row.skill)}, true
}

func (m *Model) SetSkills(skills []protocol.SkillInfo) {
	m.Skills = skills
	m.SkillsLoading = false
	m.SkillsError = ""
	// 初始通知可能在 Chat 列表组件创建前抵达；原始数据已保存，初始化后会统一构建列表。
	if m.SkillsList.Initialized() {
		m.SkillsList.SetItems(skillItems(skills))
	}
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
