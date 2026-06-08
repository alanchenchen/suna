package chat

// KeyTarget 描述 Chat key event 应先交给哪个 modal/区域处理。
type KeyTarget int

const (
	KeyTargetNormal KeyTarget = iota
	KeyTargetDiscardDraft
	KeyTargetGuard
	KeyTargetModelPicker
	KeyTargetSkills
	KeyTargetMCP
	KeyTargetPendingImagePaste
	KeyTargetAttachment
	KeyTargetBlocked
)

// RouteKey 按 overlay/modal 优先级路由 key。root adapter 根据返回值执行对应 tea.Cmd glue。
func (m Model) RouteKey(key string, inputLocked bool, compacting bool) KeyTarget {
	if m.ConfirmDiscardDraft {
		return KeyTargetDiscardDraft
	}
	if m.PendingGuard != nil {
		return KeyTargetGuard
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
	if m.PendingImagePaste != nil {
		return KeyTargetPendingImagePaste
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
		case "ctrl+c", "?", "ctrl+t", "ctrl+r", "pgup", "pgdown", "up", "down":
			return true
		default:
			return false
		}
	}
	switch key {
	case "ctrl+c", "?", "esc", "enter", "ctrl+j", "ctrl+t", "ctrl+r", "pgup", "pgdown", "up", "down":
		return true
	default:
		return false
	}
}

func (m *Model) CancelDiscardDraft() {
	m.ConfirmDiscardDraft = false
}

func (m *Model) InsertNewline() {
	m.ConfirmDiscardDraft = false
	m.Textarea.InsertString("\n")
}

func (m *Model) ToggleReasoningDetail() {
	m.ShowReasoningDetail = !m.ShowReasoningDetail
}

func (m *Model) ToggleToolDetail(visibleIDs []string) {
	m.ShowToolDetail = !m.ShowToolDetail
	m.ToolDetailScroll = 0
	if m.ShowToolDetail && m.SelectedToolID == "" && len(visibleIDs) > 0 {
		m.SelectedToolID = visibleIDs[0]
	}
}
