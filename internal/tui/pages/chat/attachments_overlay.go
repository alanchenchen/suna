package chat

// 附件浮层是会话级资源的只读列表：展示数量与总大小，唯一操作是一键清空。
// 附件文件名是内容哈希，同一文件可能被多条历史消息引用，因此不做单个删除，
// 避免破坏历史消息中的图片引用（协议也只提供 status/clear 两个方法）。

func (m *Model) OpenAttachmentsOverlay() {
	m.AttachmentsOverlayOpen = true
	m.AttachmentsConfirm = false
	m.SkillsOverlayOpen = false
	m.MCPOverlayOpen = false
	m.MemoryOverlayOpen = false
	m.SessionsOverlayOpen = false
}

func (m *Model) CloseAttachmentsOverlay() {
	m.AttachmentsOverlayOpen = false
	m.AttachmentsConfirm = false
}

func (m *Model) BeginAttachmentsClear() {
	m.AttachmentsConfirm = true
}

func (m *Model) CancelAttachmentsConfirm() {
	m.AttachmentsConfirm = false
}

func (m *Model) ConfirmAttachmentsClear() bool {
	if !m.AttachmentsConfirm {
		return false
	}
	m.AttachmentsConfirm = false
	return true
}
