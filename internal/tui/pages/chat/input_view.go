package chat

import "strings"

type InputAreaView struct {
	Textarea          string
	LockedPlaceholder string
	Locked            bool
	HasDraft          bool
	Confirm           string
	Hint              string
	AttachmentPanel   string
	Separator         string
}

// InputArea 负责 composer 区域结构：附件面板、分隔线、输入框和丢弃草稿确认。
func (m Model) InputArea(view InputAreaView) string {
	text := strings.TrimRight(view.Textarea, "\n")
	if text == "" {
		text = "> "
	}
	if view.Locked && !view.HasDraft {
		text = "> " + view.LockedPlaceholder
	}
	if view.AttachmentPanel != "" {
		body := indentLines(view.AttachmentPanel, "  ") + "\n" + view.Separator + "\n"
		if view.Hint != "" {
			body += "  " + view.Hint + "\n"
		}
		body += "  " + strings.ReplaceAll(text, "\n", "\n  ")
		if view.Confirm != "" {
			body += "\n  " + view.Confirm
		}
		return body
	}
	body := "  " + strings.ReplaceAll(text, "\n", "\n  ")
	if view.Hint != "" {
		body = "  " + view.Hint + "\n" + body
	}
	if view.Confirm != "" {
		body += "\n  " + view.Confirm
	}
	return body
}

func indentLines(s, prefix string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}
