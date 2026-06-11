package chat

import "github.com/alanchenchen/suna/internal/tui/components/attachment"

type AttachmentPanelView struct {
	Pending *attachment.PendingImagePaste
	Items   []attachment.Item
	Cursor  int
	Mode    bool
	Help    string
	Visible bool
}

func (m Model) AttachmentPanelView(help string) AttachmentPanelView {
	if p := m.ActiveImagePaste(); p != nil {
		return AttachmentPanelView{Pending: p, Visible: true}
	}
	if len(m.Attachments) == 0 {
		return AttachmentPanelView{}
	}
	return AttachmentPanelView{Items: m.Attachments, Cursor: m.AttachmentCursor, Mode: m.AttachmentMode, Help: help, Visible: true}
}

type CommandSuggestionsView struct {
	Items    []CommandSpec
	Selected int
	Visible  bool
}

func (m Model) CommandSuggestionsView() CommandSuggestionsView {
	if len(m.CmdSuggestions) == 0 {
		return CommandSuggestionsView{}
	}
	selected := m.CmdSuggestionIdx
	if selected < 0 {
		selected = 0
	}
	if selected >= len(m.CmdSuggestions) {
		selected = len(m.CmdSuggestions) - 1
	}
	return CommandSuggestionsView{Items: m.CmdSuggestions, Selected: selected, Visible: true}
}
