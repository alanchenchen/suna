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
	tuiconfig "github.com/alanchenchen/suna/internal/tui/pages/config"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
)

func (t *TUI) handleCommand(input string) tea.Cmd {
	if t.localCli == nil {
		t.appendNonToolMessage(chatMsg{Role: "error", Content: t.i18n.T("error.not_connected")})
		t.forceScrollToBottomOnNextSync()
		return nil
	}

	parts := strings.Fields(input)
	if len(parts) == 0 {
		return nil
	}
	cmd := parts[0]
	t.forceScrollToBottomOnNextSync()

	switch cmd {
	case "/new":
		if !t.canReplaceCurrentSession() {
			t.appendNonToolMessage(chatMsg{Role: "error", Content: t.tr("tui.command.new.unavailable")})
			return nil
		}
		// 新会话创建成功前不能清空当前 transcript；失败时用户仍留在原会话。
		return t.newSessionCmd(t.currentSession.ID)
	case "/model":
		if len(parts) > 1 {
			return t.switchModelRef(parts[1])
		}
		t.syncContent()
		t.openModelPicker()
		return nil
	case "/memory":
		return t.handleMemory(parts)
	case "/sessions":
		return t.handleSessions(parts)
	case "/compact":
		t.compactAuto = false
		t.chat.Compacting = true
		t.chat.Loading = true
		t.chat.Phase = phaseFirstLLM
		t.chat.PhaseStart = time.Now()
		t.chat.Textarea.Blur()
		t.syncContent()
		return tea.Batch(deferManualCompactRequestCmd(), t.startChatSpinner())
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
	if !strings.Contains(ref, "/") {
		provider := t.providerName
		if sessionProvider, _, ok := strings.Cut(t.currentSession.ModelRef, "/"); ok && sessionProvider != "" {
			provider = sessionProvider
		}
		if provider != "" {
			ref = provider + "/" + ref
		}
	}
	if _, ok := t.modelByRef(ref); !ok {
		t.appendNonToolMessage(chatMsg{Role: "error", Content: t.i18n.Tf("cmd.model_not_found", ref)})
		return nil
	}
	if t.currentSession.ID == "" {
		t.appendNonToolMessage(chatMsg{Role: "error", Content: t.i18n.T("error.not_connected")})
		return nil
	}
	t.chat.ModelPickerOpen = false
	return t.updateSessionModelCmd(t.currentSession.ID, ref)
}

func (t *TUI) updateSessionModelCmd(sessionID, modelRef string) tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification(notifyConfigError, errNotConnected(t))
		}
		updated, err := t.localCli.UpdateSession(protocol.SessionUpdateParams{SessionID: sessionID, ModelRef: &modelRef})
		if err != nil {
			return ipcErrorNotification(notifyConfigError, err)
		}
		return sessionSnapshotResultMsg{Params: updated}
	}
}

func (t *TUI) openModelPicker() tea.Cmd {
	models := t.configModelsSnapshot()
	rows := make([]chatpage.ModelPickerRow, 0, len(models))
	for _, model := range models {
		rows = append(rows, chatpage.ModelPickerRow{
			Ref:     model.Ref(),
			Summary: t.modelSummary(model),
			Mark:    tuiconfig.ModelStatusMark(model, t.isCurrentSessionModelRef(model.Ref())),
		})
	}
	t.chat.OpenModelPicker(rows, t.currentSession.ModelRef)
	t.chat.Textarea.Blur()
	return nil
}

func (t *TUI) updateModelPicker(key string, msg tea.Msg) (tea.Model, tea.Cmd) {
	if t.chat.ModelPickerFiltering() {
		switch key {
		case "up":
			t.chat.ModelList.MoveCursor(-1)
			return t, nil
		case "down":
			t.chat.ModelList.MoveCursor(1)
			return t, nil
		case "enter":
			if ref, ok := t.chat.SelectedModelRef(); ok {
				return t, t.switchModelRef(ref)
			}
			return t, nil
		}
	}
	if key == "esc" && !t.chat.ModelPickerFiltering() {
		t.chat.CloseModelPicker()
		return t, t.syncInputFocus()
	}
	if key == "enter" {
		if ref, ok := t.chat.SelectedModelRef(); ok {
			return t, t.switchModelRef(ref)
		}
	}
	return t, t.chat.UpdateModelPicker(msg)
}

func (t *TUI) handleMemory(parts []string) tea.Cmd {
	if len(parts) != 1 {
		t.appendNonToolMessage(chatMsg{Role: "system", Content: t.i18n.T("memory.list_hint")})
		return nil
	}
	t.chat.OpenMemoryOverlay()
	return t.listMemoryCmd()
}

func (t *TUI) handleSessions(parts []string) tea.Cmd {
	if len(parts) != 1 {
		t.appendNonToolMessage(chatMsg{Role: "system", Content: t.tr("tui.sessions.usage")})
		return nil
	}
	t.chat.OpenSessionsOverlay()
	return t.sessionListCmd()
}

func (t *TUI) handleSkills(parts []string) tea.Cmd {
	if len(parts) != 1 {
		t.appendNonToolMessage(chatMsg{Role: "system", Content: t.tr("tui.skills.usage")})
		return nil
	}
	t.chat.OpenSkillsOverlay()
	return t.listSkillsCmd()
}

func (t *TUI) updateSkillsOverlay(ks string, msg tea.Msg) (tea.Model, tea.Cmd) {
	if t.chat.SkillsList.Filtering() {
		switch ks {
		case "up":
			t.chat.SkillsList.MoveCursor(-1)
			return t, nil
		case "down":
			t.chat.SkillsList.MoveCursor(1)
			return t, nil
		case "enter":
			if action, ok := t.chat.SelectSkill(t.tr("tui.skills.cannot_toggle")); ok {
				return t, t.setSkillOverlayCmd(action.Name, action.Scope, action.Enabled)
			}
			return t, nil
		}
	}
	if ks == "esc" && !t.chat.SkillsList.Filtering() {
		t.chat.CloseSkillsOverlay()
		return t, t.syncInputFocus()
	}
	if ks == "enter" || ks == " " || ks == "space" {
		if action, ok := t.chat.SelectSkill(t.tr("tui.skills.cannot_toggle")); ok {
			return t, t.setSkillOverlayCmd(action.Name, action.Scope, action.Enabled)
		}
		return t, nil
	}
	return t, t.chat.UpdateSkillsList(msg)
}

func (t *TUI) setSkillOverlayCmd(name, scope string, enabled bool) tea.Cmd {
	return func() tea.Msg {
		if t.localCli == nil {
			return ipcErrorNotification(notifyConfigError, errNotConnected(t))
		}
		if err := t.localCli.SetSkill(protocol.SkillSetParams{Name: strings.TrimSpace(name), Scope: strings.TrimSpace(scope), Enabled: enabled}); err != nil {
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

func (t *TUI) updateSessionsOverlay(ks string) (tea.Model, tea.Cmd) {
	if t.chat.SessionConfirm != chatpage.SessionConfirmNone {
		switch ks {
		case "esc":
			t.chat.CancelSessionConfirm()
			return t, nil
		case "enter":
			if action, ok := t.chat.ConfirmSessionDelete(); ok {
				return t, t.deleteSessionCmd(action.ID)
			}
			return t, nil
		default:
			return t, nil
		}
	}
	switch ks {
	case "esc":
		t.chat.CloseSessionsOverlay()
		return t, t.syncInputFocus()
	case "up":
		t.chat.MoveSessionCursor(-1)
		return t, nil
	case "down":
		t.chat.MoveSessionCursor(1)
		return t, nil
	case "enter":
		// 只有 Active elsewhere 可 Join；Idle elsewhere 的 Enter 明确无动作。
		if sessionID, ok := t.chat.SelectedActiveSession(); ok {
			t.chat.CloseSessionsOverlay()
			return t, t.attachSessionCmd(sessionID, true)
		}
		return t, nil
	case "delete", "backspace", "ctrl+h":
		t.chat.BeginSessionDelete(t.currentSession.ID, t.tr("tui.sessions.cannot_delete_current"), t.tr("tui.sessions.cannot_delete_active"))
		return t, nil
	}
	return t, nil
}

func (t *TUI) handleMCP(parts []string) tea.Cmd {
	if len(parts) != 1 {
		t.appendNonToolMessage(chatMsg{Role: "system", Content: t.tr("tui.mcp.usage")})
		return nil
	}
	t.chat.OpenMCPOverlay()
	return t.listMCPCmd()
}

func (t *TUI) updateMCPOverlay(ks string, msg tea.Msg) (tea.Model, tea.Cmd) {
	if t.chat.MCPList.Filtering() {
		switch ks {
		case "up":
			t.chat.MCPList.MoveCursor(-1)
			return t, nil
		case "down":
			t.chat.MCPList.MoveCursor(1)
			return t, nil
		case "enter":
			if name, ok := t.chat.SelectMCPForReload(); ok {
				t.chat.SetMCPActionServer(name)
				return t, t.reloadMCPOverlayCmd(name)
			}
			return t, nil
		}
	}
	if ks == "esc" && !t.chat.MCPList.Filtering() {
		t.chat.CloseMCPOverlay()
		return t, t.syncInputFocus()
	}
	if ks == "space" || ks == " " {
		if action, ok := t.chat.SelectMCPForToggle(); ok {
			t.chat.SetMCPActionServer(action.Name)
			return t, t.setMCPOverlayCmd(action.Name, action.Active)
		}
		return t, nil
	}
	if ks == "enter" {
		if name, ok := t.chat.SelectMCPForReload(); ok {
			t.chat.SetMCPActionServer(name)
			return t, t.reloadMCPOverlayCmd(name)
		}
		return t, nil
	}
	return t, t.chat.UpdateMCPList(msg)
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
	return t.renderNativeListOverlay(chatpage.NativeListMCP, &t.chat.MCPList, width, t.nativeListText().Reload, "tui.mcp.empty", func() string {
		if t.chat.MCPLoading && len(t.chat.MCPServers) == 0 {
			return t.tr("tui.mcp.loading")
		}
		return ""
	}(), t.chat.MCPError)
}

func (t *TUI) renderSkillsOverlay(width int) string {
	action, actionable := t.chat.SkillActionText(
		t.nativeListText().Toggle,
		t.tr("tui.skills.project_managed"),
		t.tr("tui.skills.unavailable"),
	)
	return t.renderNativeListOverlay(chatpage.NativeListSkills, &t.chat.SkillsList, width, action, "tui.skills.empty", func() string {
		if t.chat.SkillsLoading && len(t.chat.Skills) == 0 {
			return t.tr("tui.skills.loading")
		}
		return ""
	}(), t.chat.SkillsError, actionable)
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

func nativeListWidth(viewport, preferred int) int {
	// 外层浮层还会增加左右边框与内边距，窄终端不能用固定最小宽度反向撑破视口。
	return max(1, min(preferred, max(1, viewport-6)))
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

func (t *TUI) renderSessionsOverlay(width int) string {
	w := max(56, min(96, width-4))
	inner := max(36, w-8)
	bodyHeight := max(5, min(16, t.height-12))
	if t.chat.SessionConfirm == chatpage.SessionConfirmDelete {
		return t.renderSessionDeleteConfirm(w)
	}
	var lines []string
	lines = append(lines, styleHL.Render(t.tr("tui.sessions.title")))
	if t.chat.SessionsLoading && len(t.chat.Sessions) == 0 {
		lines = append(lines, "", styleDim.Render(t.tr("tui.loading")))
	} else if t.chat.SessionsError != "" {
		lines = append(lines, "", styleErrLine.Render(t.chat.SessionsError))
	}
	if len(t.chat.Sessions) == 0 && !t.chat.SessionsLoading {
		lines = append(lines, "", styleDim.Render(t.tr("tui.sessions.empty")))
	}
	start := 0
	if t.chat.SessionCursor >= bodyHeight {
		start = t.chat.SessionCursor - bodyHeight + 1
	}
	end := min(len(t.chat.Sessions), start+bodyHeight)
	lastKind := chatpage.SessionRowKind(-1)
	for i := start; i < end; i++ {
		kind := t.chat.SessionRowKindAt(i)
		if kind != lastKind {
			lines = append(lines, "", styleDim.Render(t.sessionGroupLabel(kind)))
			lastKind = kind
		}
		lines = append(lines, t.renderSessionRow(i, t.chat.Sessions[i], inner)...)
	}
	lines = append(lines, "", styleDim.Render(t.sessionsHelpText(start, bodyHeight, len(t.chat.Sessions))))
	return boxStyle.Width(w).Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func (t *TUI) sessionGroupLabel(kind chatpage.SessionRowKind) string {
	switch kind {
	case chatpage.SessionRowCurrentWorkspace:
		return t.tr("tui.sessions.group_current")
	case chatpage.SessionRowActiveElsewhere:
		return t.tr("tui.sessions.group_active")
	default:
		return t.tr("tui.sessions.group_idle")
	}
}

func (t *TUI) renderSessionDeleteConfirm(width int) string {
	var lines []string
	lines = append(lines, styleHL.Render(t.tr("tui.sessions.delete_confirm_title")), "")
	if t.chat.SessionCursor >= 0 && t.chat.SessionCursor < len(t.chat.Sessions) {
		s := t.chat.Sessions[t.chat.SessionCursor]
		lines = append(lines, styleToolDim.Render(sessionTitle(s)))
		lines = append(lines, styleDim.Render(s.CWD))
	}
	lines = append(lines, "", styleDim.Render(t.tr("tui.sessions.delete_confirm_help")))
	return boxStyle.Width(width).Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func (t *TUI) sessionStatusLabel(s protocol.SessionInfo) string {
	if s.ID == t.currentSession.ID {
		return t.tr("tui.sessions.current")
	}
	if sessionActive(s) {
		return t.tr("tui.sessions.active")
	}
	switch s.Status {
	case protocol.SessionStatusIdle:
		return t.tr("tui.sessions.idle")
	case protocol.SessionStatusRunning:
		return t.tr("tui.sessions.running")
	case protocol.SessionStatusWaiting:
		return t.tr("tui.sessions.waiting")
	case protocol.SessionStatusCompacting:
		return t.tr("tui.sessions.compacting")
	default:
		return string(s.Status)
	}
}

func (t *TUI) renderSessionRow(i int, s protocol.SessionInfo, width int) []string {
	cursor := "  "
	contentStyle := styleToolDim
	if i == t.chat.SessionCursor {
		cursor = styleCursor.Render("▶ ")
		contentStyle = styleHL
	}
	status := t.sessionStatusLabel(s)
	name := sessionTitle(s)
	clients := ""
	if s.ClientCount > 0 {
		clients = " · " + t.i18n.Tf("tui.sessions.client_count", s.ClientCount)
	}
	head := fmt.Sprintf("%s%s %s%s", cursor, styleTool.Render("["+status+"]"), contentStyle.Render(name), styleDim.Render(clients))
	meta := []string{textutil.TruncateRunes(s.CWD, max(10, width-4))}
	if s.MessageCount > 0 {
		meta = append(meta, t.i18n.Tf("tui.sessions.message_count", s.MessageCount))
	}
	if updated := relativeSessionTime(s.UpdatedAt); updated != "" {
		meta = append(meta, t.i18n.Tf("tui.sessions.updated", updated))
	}
	return []string{head, "    " + styleDim.Render(strings.Join(meta, " · "))}
}

func (t *TUI) sessionsHelpText(start, height, total int) string {
	text := t.tr("tui.sessions.help_current")
	if t.chat.SessionCursor >= 0 && t.chat.SessionCursor < len(t.chat.Sessions) {
		switch t.chat.SessionRowKindAt(t.chat.SessionCursor) {
		case chatpage.SessionRowActiveElsewhere:
			text = t.tr("tui.sessions.help_active")
		case chatpage.SessionRowIdleElsewhere:
			text = t.tr("tui.sessions.help_idle")
		}
	}
	if total > height {
		text += fmt.Sprintf(" · %d-%d/%d", start+1, min(total, start+height), total)
	}
	return text
}

func relativeSessionTime(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, value)
	}
	if err != nil {
		return value
	}
	d := time.Since(parsed)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 48*time.Hour:
		return "yesterday"
	default:
		return parsed.Format("Jan 2 15:04")
	}
}
