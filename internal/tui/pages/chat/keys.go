package chat

import "github.com/alanchenchen/suna/internal/tui/components/toolview"

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
	KeyTargetAttachments
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
	if m.AttachmentsOverlayOpen {
		return KeyTargetAttachments
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
		case "ctrl+c", "esc", "ctrl+t", "ctrl+r", "pgup", "pgdown", "up", "down", "home", "end", "tab":
			return true
		default:
			return false
		}
	}
	switch key {
	case "ctrl+c", "esc", "enter", "ctrl+j", "ctrl+t", "ctrl+r", "pgup", "pgdown", "up", "down", "home", "end", "tab":
		return true
	default:
		return false
	}
}

func (m *Model) InsertNewline() {
	m.ExitInputHistory()
	m.Textarea.InsertString("\n")
}

// ToggleVisibleBlockDetail 切换当前视窗中最相关的工具块展开态（Ctrl+T）。
// 与 Ctrl+R 的思考链展开同构：就地展开、单展开约束，展开后由调用方恢复滚动锚点。
// 目标选择不依赖"最后一个块"启发式，而是按块与视窗中心的距离取最近者，
// 因此历史块只要还在视窗内就能展开。
func (m *Model) ToggleVisibleBlockDetail() (TranscriptAnchor, bool) {
	if m == nil {
		return TranscriptAnchor{}, false
	}
	m.ensureMessageIDs()
	viewportStart := m.TranscriptYOffset
	viewportEnd := viewportStart + m.Viewport.Height()
	if m.Viewport.Height() <= 0 {
		viewportEnd = m.TranscriptTotalLines
	}

	cursor := 0
	bestBlock := (*toolview.Block)(nil)
	bestBoxKind := ""
	bestMsgIndex := -1
	bestStart := 0
	bestDistance := 0
	for _, block := range m.TranscriptBlocks {
		blockStart := cursor
		blockEnd := cursor + block.LineCount
		cursor = blockEnd
		if block.MsgIndex < 0 || block.MsgIndex >= len(m.Messages) || blockEnd <= viewportStart || blockStart >= viewportEnd {
			continue
		}
		msg := &m.Messages[block.MsgIndex]
		if msg.Role != "tool" {
			continue
		}
		tb, ok := msg.Content.(*toolview.Block)
		if !ok {
			continue
		}
		// 同一个 tool 消息渲染出两个相邻盒子，各自独立展开：
		// tool 盒子需要主条目，subtask 面板需要子任务条目。
		boxKind := block.BoxKind
		if boxKind == boxKindTool && len(toolview.VisibleMainEntries(tb)) == 0 {
			continue
		}
		if boxKind == boxKindSubtask && !hasSubtaskEntries(tb) {
			continue
		}
		if boxKind == "" {
			continue
		}
		distance := absInt((blockStart + blockEnd) - (viewportStart + viewportEnd))
		if bestBlock == nil || distance < bestDistance || (distance == bestDistance && blockStart > bestStart) {
			bestBlock = tb
			bestBoxKind = boxKind
			bestMsgIndex = block.MsgIndex
			bestStart = blockStart
			bestDistance = distance
		}
	}
	if bestBlock == nil || bestMsgIndex < 0 {
		return TranscriptAnchor{}, false
	}
	anchor := TranscriptAnchor{MessageID: m.Messages[bestMsgIndex].ID, RelativeRow: bestStart - viewportStart}
	if bestBlock == m.ExpandedBlock && bestBoxKind == m.ExpandedBoxKind {
		// 视窗内最相关的盒子就是当前展开的盒子：收起（toggle 关闭）。
		m.ExpandedBlock = nil
		m.ExpandedBoxKind = ""
	} else {
		// 展开视窗内最相关的盒子，自动替换旧的展开盒子（单展开约束）。
		m.ExpandedBlock = bestBlock
		m.ExpandedBoxKind = bestBoxKind
	}
	m.ExpandedBlockCursor = 0
	m.ExpandedBlockDetailScroll = 0
	// 切换展开块时重置 subtask 工具详情的展开态与滚动位置，
	// 否则会残留上一个块的面板状态（单展开约束要求状态随目标一起切换）。
	m.SubtaskToolDetailExpanded = false
	m.SubtaskToolDetailScroll = 0
	m.SubtaskResultScroll = 0
	return anchor, true
}
