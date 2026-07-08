package chat

// KeyTarget 描述 Chat key event 应先交给哪个 modal/区域处理。
type KeyTarget int

const (
	KeyTargetNormal KeyTarget = iota
	KeyTargetDiscardDraft
	KeyTargetGuard
	KeyTargetAskUser
	KeyTargetImagePasteConfirm
	KeyTargetModelPicker
	KeyTargetSkills
	KeyTargetMCP
	KeyTargetMemory
	KeyTargetSessions
	KeyTargetAttachment
	KeyTargetBlocked
)

// RouteKey 按 interaction、overlay、普通输入的顺序路由 key。root adapter 根据返回值执行对应 tea.Cmd glue。
func (m Model) RouteKey(key string, inputLocked bool, compacting bool) KeyTarget {
	switch m.ActiveInteractionKind() {
	case InteractionDiscardDraft:
		return KeyTargetDiscardDraft
	case InteractionGuardConfirm:
		return KeyTargetGuard
	case InteractionAskUser:
		return KeyTargetAskUser
	case InteractionImagePasteConfirm:
		return KeyTargetImagePasteConfirm
	}
	if m.ModelPickerOpen {
		return KeyTargetModelPicker
	}
	if m.SkillsOverlayOpen {
		return KeyTargetSkills
	}
	if m.MCPOverlayOpen {
		return KeyTargetMCP
	}
	if m.MemoryOverlayOpen {
		return KeyTargetMemory
	}
	if m.SessionsOverlayOpen {
		return KeyTargetSessions
	}
	if m.AttachmentMode || m.AttachmentDelete {
		return KeyTargetAttachment
	}
	if inputLocked && !AllowLockedInputKey(key, compacting) {
		return KeyTargetBlocked
	}
	return KeyTargetNormal
}

func AllowLockedInputKey(key string, compacting bool) bool {
	if compacting {
		switch key {
		case "ctrl+c", "ctrl+t", "ctrl+r", "pgup", "pgdown", "up", "down", "tab":
			return true
		default:
			return false
		}
	}
	switch key {
	case "ctrl+c", "esc", "enter", "ctrl+j", "ctrl+t", "ctrl+r", "pgup", "pgdown", "up", "down", "tab":
		return true
	default:
		return false
	}
}

func (m *Model) InsertNewline() {
	m.Textarea.InsertString("\n")
}

func (m *Model) ToggleReasoningDetail() {
	m.ShowReasoningDetail = !m.ShowReasoningDetail
}

func (m *Model) ToggleToolDetail(visibleIDs []string) {
	if len(visibleIDs) == 0 {
		m.ShowToolDetail = false
		m.SelectedToolID = ""
		m.ToolDetailScroll = 0
		return
	}
	m.ShowToolDetail = !m.ShowToolDetail
	m.ToolDetailScroll = 0
	if !m.ShowToolDetail {
		return
	}
	if !containsString(visibleIDs, m.SelectedToolID) {
		m.SelectedToolID = ""
	}
	if m.SelectedToolID == "" && len(visibleIDs) > 0 {
		m.SelectedToolID = visibleIDs[0]
	}
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
