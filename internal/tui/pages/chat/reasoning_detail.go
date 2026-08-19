package chat

// TranscriptAnchor 记录某条消息在视窗中的相对位置，用于内容展开后稳定滚动位置。
type TranscriptAnchor struct {
	MessageID   uint64
	RelativeRow int
}

// ReasoningMode 返回某条思考消息当前应使用的展示模式。
func (m Model) ReasoningMode(msg *Msg) string {
	if msg != nil && !msg.Streaming && msg.ID != 0 && msg.ID == m.ExpandedReasoningID {
		return "reasoning_detail"
	}
	return "reasoning_collapsed"
}

func (m Model) HasStreamingReasoning() bool {
	for i := range m.Messages {
		if m.Messages[i].Role == "reasoning" && m.Messages[i].Streaming {
			return true
		}
	}
	return false
}

// ToggleVisibleReasoningDetail 切换当前视窗中最相关的已完成思考块。
func (m *Model) ToggleVisibleReasoningDetail() (TranscriptAnchor, bool) {
	if m == nil || m.HasStreamingReasoning() {
		return TranscriptAnchor{}, false
	}
	m.ensureMessageIDs()
	viewportStart := m.TranscriptYOffset
	viewportEnd := viewportStart + m.Viewport.Height()
	if m.Viewport.Height() <= 0 {
		viewportEnd = m.TranscriptTotalLines
	}

	cursor := 0
	bestIndex := -1
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
		if msg.Role != "reasoning" || msg.Streaming {
			continue
		}
		if msg.ID == m.ExpandedReasoningID {
			anchor := TranscriptAnchor{MessageID: msg.ID, RelativeRow: blockStart - viewportStart}
			m.ExpandedReasoningID = 0
			return anchor, true
		}
		distance := absInt((blockStart + blockEnd) - (viewportStart + viewportEnd))
		if bestIndex < 0 || distance < bestDistance || (distance == bestDistance && blockStart > bestStart) {
			bestIndex = block.MsgIndex
			bestStart = blockStart
			bestDistance = distance
		}
	}
	if bestIndex < 0 {
		return TranscriptAnchor{}, false
	}
	msg := &m.Messages[bestIndex]
	m.ExpandedReasoningID = msg.ID
	return TranscriptAnchor{MessageID: msg.ID, RelativeRow: bestStart - viewportStart}, true
}

// RestoreTranscriptAnchor 在 transcript 高度变化后恢复目标消息的屏幕位置。
func (m *Model) RestoreTranscriptAnchor(anchor TranscriptAnchor) bool {
	if m == nil || anchor.MessageID == 0 {
		return false
	}
	cursor := 0
	for _, block := range m.TranscriptBlocks {
		if block.MsgIndex >= 0 && block.MsgIndex < len(m.Messages) && m.Messages[block.MsgIndex].ID == anchor.MessageID {
			m.SetTranscriptYOffset(cursor - anchor.RelativeRow)
			m.FollowBottom = m.TranscriptAtBottom()
			m.ManualScrollPaused = !m.FollowBottom
			if m.FollowBottom {
				m.NewContentWhilePaused = false
			}
			return true
		}
		cursor += block.LineCount
	}
	return false
}

func (m *Model) ensureMessageIDs() {
	for i := range m.Messages {
		if m.Messages[i].ID != 0 {
			if m.Messages[i].ID > m.NextMessageID {
				m.NextMessageID = m.Messages[i].ID
			}
			continue
		}
		m.NextMessageID++
		m.Messages[i].ID = m.NextMessageID
	}
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
