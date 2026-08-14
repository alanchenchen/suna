package tui

import chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"

// nativeListStyles 将当前 TUI 主题注入原生 Bubbles list 的 delegate。
func (t *TUI) nativeListStyles() chatpage.ListStyles {
	return chatpage.ListStyles{
		Cursor: styleCursor,
		Title:  styleHL,
		Text:   styleSysLine,
		Dim:    styleDim,
		OK:     styleToolOk,
		Error:  styleToolErr,
		Run:    styleToolRun,
	}
}

func (t *TUI) refreshNativeLists() {
	if t.chat.SkillsList.Owner() == "" {
		return
	}
	t.chat.InitNativeLists(currentTheme.Name == ThemeDark, t.nativeListStyles(), t.nativeListText())
}

// nativeListText 在 UI 层注入原生列表文案，通用列表组件不依赖翻译器。
func (t *TUI) nativeListText() chatpage.ListText {
	return chatpage.ListText{
		SkillsTitle:  t.tr("tui.list.skills.title"),
		MCPTitle:     t.tr("tui.list.mcp.title"),
		ModelsTitle:  t.tr("tui.list.models.title"),
		CountSuffix:  t.tr("tui.list.count_suffix"),
		Filter:       t.tr("tui.list.filter_prompt"),
		Skill:        t.tr("tui.list.skill"),
		Skills:       t.tr("tui.list.skills"),
		Server:       t.tr("tui.list.server"),
		Servers:      t.tr("tui.list.servers"),
		Model:        t.tr("tui.list.model"),
		Models:       t.tr("tui.list.models"),
		Toggle:       t.tr("tui.list.toggle"),
		Reload:       t.tr("tui.list.reload"),
		GlobalScope:  t.tr("tui.skills.scope.global"),
		ProjectScope: t.tr("tui.skills.scope.project"),
		Select:       t.tr("tui.list.select"),
		Close:        t.tr("tui.list.close"),
		Tools:        t.tr("tui.list.tools"),
		Up:           t.tr("tui.list.key.up"),
		Down:         t.tr("tui.list.key.down"),
		FilterHelp:   t.tr("tui.list.key.filter"),
		ClearFilter:  t.tr("tui.list.key.clear_filter"),
		Cancel:       t.tr("tui.list.key.cancel"),
	}
}
