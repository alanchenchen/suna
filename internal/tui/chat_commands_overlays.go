package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/alanchenchen/suna/internal/protocol"
	textutil "github.com/alanchenchen/suna/internal/tui/components/text"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
)

func (t *TUI) handleCommand(input string) tea.Cmd {
	if t.localCli == nil {
		t.appendNonToolMessage(chatMsg{Role: "error", Content: t.i18n.T("error.not_connected")})
		t.scrollToBottomOnNextSync()
		return nil
	}

	parts := strings.Fields(input)
	if len(parts) == 0 {
		return nil
	}
	cmd := parts[0]
	t.scrollToBottomOnNextSync()

	switch cmd {
	case "/new":
		t.chat.Messages = []chatMsg{}
		t.chat.DisplayDiscard = chatpage.DisplayDiscardSummary{}
		t.chat.Attachments = nil
		t.chat.ResumeAvailable = false
		t.resetConversationStats()
		t.resetPhase()
		t.chat.LastAssistantText = ""
		return t.newSessionCmd()
	case "/model":
		if len(parts) > 1 {
			return t.switchModelRef(parts[1])
		}
		t.openModelPicker()
		t.syncContent()
		return nil
	case "/memory":
		return t.handleMemory(parts)
	case "/compact":
		t.compactAuto = false
		t.chat.Compacting = true
		t.chat.Loading = true
		t.chat.Phase = phaseFirstLLM
		t.chat.PhaseStart = time.Now()
		t.chat.Textarea.Blur()
		t.syncContent()
		return tea.Batch(deferManualCompactRequestCmd(), t.chat.Spinner.Tick)
	case "/config":
		t.mode = uipage.Config
		t.config.FromMode = uipage.Chat
		t.config.SetupMode = false
		t.config.FormOpen = false
		t.config.Page = "home"
		return nil
	case "/skills":
		return t.handleSkills(parts)
	case "/mcp":
		return t.handleMCP(parts)
	case "/help":
		t.prevMode = uipage.Chat
		t.mode = uipage.Help
		t.initHelpPage()
		return nil
	default:
		t.appendNonToolMessage(chatMsg{Role: "error", Content: t.i18n.Tf("cmd.unknown", cmd)})
	}
	return nil
}

func (t *TUI) switchModelRef(ref string) tea.Cmd {
	if !strings.Contains(ref, "/") && t.providerName != "" {
		ref = t.providerName + "/" + ref
	}
	if _, ok := t.modelByRef(ref); !ok {
		t.appendNonToolMessage(chatMsg{Role: "error", Content: t.i18n.Tf("cmd.model_not_found", ref)})
		return nil
	}
	t.setActiveModelRef(ref)
	t.chat.ModelPickerOpen = false
	t.appendNonToolMessage(chatMsg{Role: "system", Content: t.i18n.Tf("cmd.model_switched", ref)})
	return t.sendConfigSet(protocol.ConfigSetParams{Action: protocol.ConfigActionActivateModel, ActiveModel: ref})
}

func (t *TUI) openModelPicker() {
	t.chat.OpenModelPicker(chatpage.ModelRefs(t.configModelsSnapshot()), t.configState.ActiveModel)
}

func (t *TUI) updateModelPicker(key string) (tea.Model, tea.Cmd) {
	models := t.configModelsSnapshot()
	refs := chatpage.ModelRefs(models)
	if len(refs) == 0 {
		t.chat.CloseModelPicker()
		return t, nil
	}
	switch key {
	case "esc":
		t.chat.CloseModelPicker()
	case "up":
		t.chat.MoveModelPicker(-1, len(refs))
	case "down":
		t.chat.MoveModelPicker(1, len(refs))
	case "enter":
		if ref, ok := t.chat.SelectedModelRef(refs); ok {
			return t, t.switchModelRef(ref)
		}
	}
	t.syncContent()
	return t, nil
}

func (t *TUI) handleMemory(parts []string) tea.Cmd {
	if len(parts) != 1 {
		t.appendNonToolMessage(chatMsg{Role: "system", Content: t.i18n.T("memory.list_hint")})
		return nil
	}
	t.chat.OpenMemoryOverlay()
	return t.listMemoryCmd()
}

func (t *TUI) handleSkills(parts []string) tea.Cmd {
	if len(parts) != 1 {
		t.appendNonToolMessage(chatMsg{Role: "system", Content: t.tr("tui.skills.usage")})
		return nil
	}
	t.chat.OpenSkillsOverlay()
	return t.listSkillsCmd()
}

func (t *TUI) updateSkillsOverlay(ks string) (tea.Model, tea.Cmd) {
	switch ks {
	case "esc":
		t.chat.CloseSkillsOverlay()
		return t, t.syncInputFocus()
	case "up":
		t.chat.MoveSkillsCursor(-1)
		return t, nil
	case "down":
		t.chat.MoveSkillsCursor(1)
		return t, nil
	case "enter", " ", "space":
		if action, ok := t.chat.SelectSkill(t.tr("tui.skills.cannot_toggle")); ok {
			return t, t.setSkillOverlayCmd(action.Name, action.Enabled)
		}
		return t, nil
	}
	return t, nil
}

func (t *TUI) setSkillOverlayCmd(name string, enabled bool) tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification(notifyConfigError, errNotConnected(t))
		}
		if err := t.localCli.SetSkill(protocol.SkillSetParams{Name: strings.TrimSpace(name), Enabled: enabled}); err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		result, err := t.localCli.ListSkills()
		if err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		return skillListResultMsg{Params: result}
	}
}

func (t *TUI) updateMemoryOverlay(ks string) (tea.Model, tea.Cmd) {
	if t.chat.MemoryConfirm != chatpage.MemoryConfirmNone {
		switch ks {
		case "esc":
			t.chat.CancelMemoryConfirm()
			return t, nil
		case "enter":
			if t.chat.MemoryConfirm == chatpage.MemoryConfirmDelete {
				if action, ok := t.chat.ConfirmMemoryDelete(); ok {
					return t, t.deleteMemoryOverlayCmd(action.ID)
				}
				return t, nil
			}
			if t.chat.ConfirmMemoryClear() {
				return t, t.clearMemoryOverlayCmd()
			}
			return t, nil
		default:
			t.chat.UpdateMemoryConfirmText(ks)
			return t, nil
		}
	}
	switch ks {
	case "esc":
		t.chat.CloseMemoryOverlay()
		return t, t.syncInputFocus()
	case "up":
		t.chat.MoveMemoryCursor(-1)
		return t, nil
	case "down":
		t.chat.MoveMemoryCursor(1)
		return t, nil
	case "delete", "backspace", "ctrl+h":
		if t.chat.BeginMemoryDelete() {
			return t, nil
		}
		return t, nil
	case "enter":
		if t.chat.MemorySelectionIsClear() {
			t.chat.BeginMemoryClear()
		}
		return t, nil
	}
	return t, nil
}

func (t *TUI) deleteMemoryOverlayCmd(id string) tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification(notifyConfigError, errNotConnected(t))
		}
		if err := t.localCli.DeleteMemory(id); err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		return nil
	}
}

func (t *TUI) clearMemoryOverlayCmd() tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification(notifyConfigError, errNotConnected(t))
		}
		if err := t.localCli.ClearMemory(); err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		return nil
	}
}

func (t *TUI) handleMCP(parts []string) tea.Cmd {
	if len(parts) != 1 {
		t.appendNonToolMessage(chatMsg{Role: "system", Content: t.tr("tui.mcp.usage")})
		return nil
	}
	t.chat.OpenMCPOverlay()
	return t.listMCPCmd()
}

func (t *TUI) updateMCPOverlay(ks string) (tea.Model, tea.Cmd) {
	switch ks {
	case "esc":
		t.chat.CloseMCPOverlay()
		return t, t.syncInputFocus()
	case "up":
		t.chat.MoveMCPCursor(-1)
		return t, nil
	case "down":
		t.chat.MoveMCPCursor(1)
		return t, nil
	case " ", "space":
		if action, ok := t.chat.SelectMCPForToggle(); ok {
			t.chat.SetMCPActionServer(action.Name)
			return t, t.setMCPOverlayCmd(action.Name, action.Active)
		}
		return t, nil
	case "enter":
		if name, ok := t.chat.SelectMCPForReload(); ok {
			t.chat.SetMCPActionServer(name)
			return t, t.reloadMCPOverlayCmd(name)
		}
		return t, nil
	}
	return t, nil
}

func (t *TUI) setMCPOverlayCmd(name string, active bool) tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification(notifyMCPError, errNotConnected(t))
		}
		if err := t.localCli.ToggleMCP(protocol.MCPSetParams{Name: strings.TrimSpace(name), Active: active}); err != nil {
			return ipcErrorNotification(notifyMCPError, err)
		}
		result, err := t.localCli.ListMCP()
		if err != nil {
			return ipcErrorNotification(notifyMCPError, err)
		}
		return mcpListResultMsg{Params: result}
	}
}

func (t *TUI) reloadMCPOverlayCmd(name string) tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification(notifyMCPError, errNotConnected(t))
		}
		if err := t.localCli.ReloadMCP(protocol.MCPReloadParams{Name: strings.TrimSpace(name)}); err != nil {
			return ipcErrorNotification(notifyMCPError, err)
		}
		result, err := t.localCli.ListMCP()
		if err != nil {
			return ipcErrorNotification(notifyMCPError, err)
		}
		return mcpListResultMsg{Params: result}
	}
}

func (t *TUI) renderMCPOverlay(width int) string {
	view := t.chat.MCPOverlayView(width, t.overlayMaxHeight())
	var body []string
	if view.Loading {
		body = append(body, styleDim.Render(t.tr("tui.mcp.loading")))
	} else if view.Empty {
		body = append(body, styleDim.Render(t.tr("tui.mcp.empty")))
	} else {
		for _, row := range view.Rows {
			body = append(body, t.renderMCPRowView(row, view.Inner))
		}
	}
	body, start, total := scrollWindow(body, view.Height, &t.chat.MCPScroll)
	title := t.tr("tui.mcp.title", view.Active, view.Total, view.Tools, view.Issues)
	lines := []string{styleHL.Render(title), ""}
	lines = append(lines, body...)
	if view.Error != "" {
		lines = append(lines, "", styleError.Render(view.Error))
	}
	lines = append(lines, "", styleDim.Render(t.mcpHelpText(start, view.Height, total)))
	return boxStyle.Width(view.Width).Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func (t *TUI) renderMCPRowView(row chatpage.MCPRowView, width int) string {
	cursor := "  "
	nameStyle := lipgloss.NewStyle()
	if row.Selected {
		cursor = styleCursor.Render("▶ ")
		nameStyle = styleHL
	}
	mark := mcpActiveMark(row)
	name := truncateDisplay(row.Server.Name, max(12, width/3))
	transport := strings.TrimSpace(row.Server.Transport)
	if transport == "" {
		transport = "stdio"
	}
	status := fmt.Sprintf("%s · %s %d", transport, t.tr("tui.mcp.tools"), row.Server.ToolCount)
	if row.Loading {
		status = t.tr("tui.mcp.reloading")
	} else if row.Issue {
		status = t.tr("tui.mcp.error")
	} else if !row.Active {
		status = t.tr("tui.mcp.inactive")
	}
	line := fmt.Sprintf("%s%s %-22s %s", cursor, mark, nameStyle.Render(name), mcpStatusStyle(row).Render(truncateDisplay(status, max(10, width-30))))
	cmd := strings.TrimSpace(row.Server.Command)
	if cmd != "" {
		line += "  " + styleToolDim.Render(truncateDisplay(cmd, max(8, width-lipgloss.Width(line)-2)))
	}
	if row.Issue && row.Server.Error != "" {
		line += "  " + styleToolErr.Render(truncateDisplay(row.Server.Error, max(8, width-lipgloss.Width(line)-2)))
	}
	return line
}

func (t *TUI) mcpHelpText(start, height, total int) string {
	text := t.tr("tui.mcp.help")
	if total > height {
		text += fmt.Sprintf(" · %d-%d/%d", start+1, min(total, start+height), total)
	}
	return text
}

func mcpActiveMark(row chatpage.MCPRowView) string {
	if row.Loading {
		return styleToolRun.Render("◌")
	}
	if row.Issue {
		return styleToolErr.Render("!")
	}
	if row.Active {
		return styleToolOk.Render("●")
	}
	return styleDim.Render("○")
}

func mcpStatusStyle(row chatpage.MCPRowView) lipgloss.Style {
	if row.Loading {
		return styleToolRun
	}
	if row.Issue {
		return styleToolErr
	}
	if row.Active {
		return styleToolOk
	}
	return styleDim
}

func (t *TUI) renderSkillsOverlay(width int) string {
	view := t.chat.SkillsOverlayView(width, t.overlayMaxHeight())
	var body []string
	if view.Loading {
		body = append(body, styleDim.Render(t.tr("tui.skills.loading")))
	} else if view.Empty {
		body = append(body, styleDim.Render(t.tr("tui.skills.empty")))
	} else {
		for _, row := range view.Rows {
			body = append(body, t.renderSkillRowView(row, view.Inner))
		}
	}
	body, start, total := scrollWindow(body, view.Height, &t.chat.SkillsScroll)
	title := t.tr("tui.skills.title", view.Active, view.Total, view.Issues)
	lines := []string{styleHL.Render(title), ""}
	lines = append(lines, body...)
	if view.Error != "" {
		lines = append(lines, "", styleError.Render(view.Error))
	}
	lines = append(lines, "", styleDim.Render(t.skillsHelpText(start, view.Height, total)))
	return boxStyle.Width(view.Width).Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func (t *TUI) renderSkillRowView(row chatpage.SkillRowView, width int) string {
	cursor := "  "
	nameStyle := lipgloss.NewStyle()
	if row.Selected {
		cursor = styleCursor.Render("▶ ")
		nameStyle = styleHL
	}
	mark := skillActiveMark(row.Active)
	status := t.tr("tui.skills.inactive")
	statusStyle := styleDim
	if row.Active {
		status = t.tr("tui.skills.active")
		statusStyle = styleToolOk
	}
	name := truncateDisplay(row.Skill.Name, max(12, width-24))
	line := fmt.Sprintf("%s%s %-24s %-10s", cursor, mark, nameStyle.Render(name), statusStyle.Render(status))
	if row.Issue {
		line += "  " + styleTool.Render(skillIssueText(t, row.Skill))
	}
	return line
}

func (t *TUI) renderMemoryOverlay(width int) string {
	view := t.chat.MemoryOverlayView(width, t.overlayMaxHeight())
	if view.Confirm != chatpage.MemoryConfirmNone {
		return t.renderMemoryConfirmOverlay(view)
	}
	var body []string
	body = append(body, styleDim.Render(t.tr("tui.memory.description")), "")
	if view.Loading {
		body = append(body, styleDim.Render(t.tr("tui.memory.loading")))
	} else {
		for _, row := range view.Rows {
			body = append(body, t.renderMemoryRowView(row, view.Inner)...)
		}
	}
	body, start, total := scrollWindow(body, view.Height, &t.chat.MemoryScroll)
	title := t.tr("tui.memory.title", view.Total)
	lines := []string{styleHL.Render(title), ""}
	lines = append(lines, body...)
	if view.Error != "" {
		lines = append(lines, "", styleError.Render(view.Error))
	}
	lines = append(lines, "", styleDim.Render(t.memoryHelpText(start, view.Height, total)))
	return boxStyle.Width(view.Width).Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func (t *TUI) renderMemoryRowView(row chatpage.MemoryRowView, width int) []string {
	cursor := "  "
	contentStyle := styleToolDim
	if row.Selected {
		cursor = styleCursor.Render("▶ ")
		contentStyle = styleHL
	}
	if row.Kind == chatpage.MemoryRowClear {
		return []string{"", cursor + styleError.Render(t.tr("tui.memory.clear_item"))}
	}
	badge := row.Memory.Kind
	if row.Memory.IsCore {
		badge = "core " + badge
	}
	content := strings.TrimSpace(row.Memory.Content)
	if content == "" {
		content = "-"
	}
	wrapped := textutil.WrapLine(content, max(12, width-12))
	if len(wrapped) == 0 {
		wrapped = []string{""}
	}
	lines := []string{fmt.Sprintf("%s%s %s", cursor, styleTool.Render("["+badge+"]"), contentStyle.Render(wrapped[0]))}
	for _, line := range wrapped[1:] {
		lines = append(lines, "    "+contentStyle.Render(line))
	}
	return lines
}

func (t *TUI) renderMemoryConfirmOverlay(view chatpage.MemoryOverlayView) string {
	var lines []string
	switch view.Confirm {
	case chatpage.MemoryConfirmDelete:
		lines = append(lines, styleHL.Render(t.tr("tui.memory.delete_confirm_title")), "")
		if t.chat.MemoryCursor >= 0 && t.chat.MemoryCursor < len(t.chat.Memories) {
			lines = append(lines, styleToolDim.Render(t.chat.Memories[t.chat.MemoryCursor].Content))
		}
		lines = append(lines, "", styleDim.Render(t.tr("tui.memory.delete_confirm_help")))
	case chatpage.MemoryConfirmClear:
		lines = append(lines, styleHL.Render(t.tr("tui.memory.clear_confirm_title")), "")
		lines = append(lines, styleDim.Render(t.tr("tui.memory.clear_confirm_body", view.Total)), "")
		lines = append(lines, t.tr("tui.memory.clear_confirm_input", view.ConfirmText))
		lines = append(lines, "", styleDim.Render(t.tr("tui.memory.clear_confirm_help")))
	}
	return boxStyle.Width(view.Width).Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func (t *TUI) memoryHelpText(start, height, total int) string {
	text := t.tr("tui.memory.help")
	if total > height {
		text += fmt.Sprintf(" · %d-%d/%d", start+1, min(total, start+height), total)
	}
	return text
}

func (t *TUI) renderSkillRow(i int, s protocol.SkillInfo, width int) string {
	return t.renderSkillRowView(chatpage.SkillRowView{Skill: s, Selected: i == t.chat.SkillsCursor, Active: chatpage.SkillIsActive(s), Issue: chatpage.SkillHasIssue(s)}, width)
}

func (t *TUI) skillsHelpText(start, height, total int) string {
	text := t.tr("tui.skills.help")
	if total > height {
		text += fmt.Sprintf(" · %d-%d/%d", start+1, min(total, start+height), total)
	}
	return text
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
	var out strings.Builder
	width := 0
	ellipsisWidth := lipgloss.Width("…")
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if width+rw+ellipsisWidth > maxWidth {
			break
		}
		out.WriteRune(r)
		width += rw
	}
	return out.String() + "…"
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

func (t *TUI) renderMemoryList(memories []protocol.MemoryItem) string {
	width := max(36, min(t.width-6, 92))
	inner := max(24, width-8)
	var lines []string
	lines = append(lines, styleHL.Render(t.tr("tui.memory.active_title")))
	for _, m := range memories {
		lines = append(lines, renderMemoryItem(m, inner)...)
	}
	return boxStyle.Width(width).Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func renderMemoryItem(m protocol.MemoryItem, width int) []string {
	badge := fmt.Sprintf("%s:%d", m.Kind, m.Priority)
	if m.IsCore {
		badge = "core " + badge
	}
	head := styleTool.Render("[" + badge + "]")
	content := strings.TrimSpace(m.Content)
	if content == "" {
		content = "-"
	}
	wrapped := textutil.WrapLine(content, max(12, width-4))
	if len(wrapped) == 0 {
		wrapped = []string{""}
	}
	lines := []string{"  " + styleDim.Render("• ") + head}
	for _, line := range wrapped {
		lines = append(lines, "    "+styleToolDim.Render(line))
	}
	return lines
}

func lipglossWidthPlain(s string) int {
	return lipgloss.Width(s)
}
