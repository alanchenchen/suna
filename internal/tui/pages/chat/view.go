package chat

import (
	"strings"

	"charm.land/lipgloss/v2"
)

type ViewDeps struct {
	Width int

	MiniPet string
	TopMeta string
	Conn    string

	Content            string
	Separator          string
	InputArea          string
	InputSeparator     string
	PreInputHint       string
	CommandSuggestions string
	StatusBar          string

	ToolDetailOverlay string
	HelpOverlay       string
	SkillsOverlay     string
	MCPOverlay        string
	MemoryOverlay     string
	SessionsOverlay   string
	GuardOverlay      string
	Overlay           func(base, overlay string) string
}

// View 负责 Chat 页面主布局和 overlay 叠放顺序；具体样式和子组件渲染由 root adapter 注入。
func (m Model) View(deps ViewDeps) string {
	if deps.Width == 0 {
		return ""
	}
	var sb strings.Builder
	pet := strings.Split(deps.MiniPet, "\n")
	for len(pet) < 3 {
		pet = append(pet, "")
	}
	gap := 2
	used := lipgloss.Width(pet[1]) + gap + lipgloss.Width(deps.TopMeta) + gap + lipgloss.Width(deps.Conn)
	pad := maxInt(gap, deps.Width-used)

	sb.WriteString(pet[0] + "\n")
	sb.WriteString(pet[1])
	sb.WriteString(strings.Repeat(" ", gap) + deps.TopMeta + strings.Repeat(" ", pad) + deps.Conn + "\n")
	sb.WriteString(pet[2] + "\n")
	sb.WriteString(deps.Separator + "\n")

	content := deps.Content
	if m.ShowToolDetail && deps.ToolDetailOverlay != "" {
		content = overlay(content, deps.ToolDetailOverlay, deps.Overlay)
	}
	if deps.HelpOverlay != "" {
		content = overlay(content, deps.HelpOverlay, deps.Overlay)
	}
	if m.SkillsOverlayOpen && deps.SkillsOverlay != "" {
		content = overlay(content, deps.SkillsOverlay, deps.Overlay)
	}
	if m.MCPOverlayOpen && deps.MCPOverlay != "" {
		content = overlay(content, deps.MCPOverlay, deps.Overlay)
	}
	if m.MemoryOverlayOpen && deps.MemoryOverlay != "" {
		content = overlay(content, deps.MemoryOverlay, deps.Overlay)
	}
	if m.SessionsOverlayOpen && deps.SessionsOverlay != "" {
		content = overlay(content, deps.SessionsOverlay, deps.Overlay)
	}
	if m.ActiveInteractionKind() == InteractionGuardConfirm && deps.GuardOverlay != "" {
		content = overlay(content, deps.GuardOverlay, deps.Overlay)
	}
	sb.WriteString(content)
	if content != "" && !strings.HasSuffix(content, "\n") {
		// Content 通常来自 viewport，部分场景不会带尾部换行；输入分割线必须单独成行，
		// 否则会被拼到最后一行消息后面，视觉上像是没有显示。
		sb.WriteString("\n")
	}
	if deps.PreInputHint != "" {
		sb.WriteString(deps.PreInputHint + "\n")
	}
	inputSeparator := deps.InputSeparator
	if inputSeparator == "" {
		inputSeparator = deps.Separator
	}
	sb.WriteString(inputSeparator + "\n")
	sb.WriteString(deps.InputArea)
	if deps.CommandSuggestions != "" {
		sb.WriteString("\n" + deps.CommandSuggestions)
	}
	sb.WriteString("\n" + deps.StatusBar)
	return sb.String()
}

func overlay(base, block string, fn func(string, string) string) string {
	if fn == nil {
		return base
	}
	return fn(base, block)
}
