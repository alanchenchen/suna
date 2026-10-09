package chat

// displayBudget 只保存每条消息的原估算值；原始内容和渲染输出不在这里复制。
type displayBudget struct {
	ready              bool
	bytes              []int
	outputs            heightIndex
	total, outputBytes int
	dirty              map[int]bool
}

func (m *Model) markDisplayBudgetDirty(i int) {
	if m.displayBudget.dirty == nil {
		m.displayBudget.dirty = make(map[int]bool)
	}
	m.displayBudget.dirty[i] = true
}

func (m *Model) updateDisplayBudget() {
	b := &m.displayBudget
	initial := !b.ready
	if !b.ready || len(b.bytes) > len(m.Messages) {
		*b = displayBudget{ready: true, bytes: make([]int, 0, len(m.Messages)), dirty: make(map[int]bool)}
	}
	batch := len(m.Messages)-len(b.bytes) > 1
	if batch {
		b.bytes = reserveTranscriptSlice(b.bytes, len(m.Messages))
		if len(b.outputs.tree) != 0 {
			b.outputs.tree = reserveTranscriptSlice(b.outputs.tree, len(m.Messages)+1)
		}
	}
	for len(b.bytes) < len(m.Messages) {
		i := len(b.bytes)
		b.bytes = append(b.bytes, 0)
		if len(b.outputs.tree) != 0 {
			b.outputs.append(0)
		}
		b.dirty[i] = true
	}
	dirtyCount := len(b.dirty)
	for i := range b.dirty {
		if i >= 0 && i < len(m.Messages) {
			m.accountDisplayMessage(i)
		}
		delete(b.dirty, i)
	}
	if initial || batch || dirtyCount > 1024 {
		b.dirty = make(map[int]bool)
	}
}

func (m *Model) accountDisplayMessage(i int) {
	b := &m.displayBudget
	msg := &m.Messages[i]
	value := estimateMsgDisplayBytes(msg)
	b.total += value - b.bytes[i]
	b.bytes[i] = value
	// 输出长度单独记账，既可精确保持旧缓存预算，也可索引最近六个非空缓存。
	output := len(msg.Render.Output)
	if len(b.outputs.tree) == 0 {
		if output == 0 {
			return
		}
		// 无缓存输出的历史不需要第二棵索引树；首次非空输出时才分配。
		b.outputs.reset(make([]int, len(b.bytes)))
	}
	previous := b.outputs.value(i)
	b.outputBytes += output - previous
	b.outputs.add(i, output-previous)
}

func (m *Model) trimRetainedMarkdownCache() bool {
	m.updateDisplayBudget()
	b := &m.displayBudget
	if b.outputBytes <= markdownRenderCacheBudgetBytes {
		return false
	}
	protected := make(map[int]bool)
	position := b.outputs.prefix(len(b.bytes)) - 1
	for count := 0; count < markdownRenderCacheRecentKeep && position >= 0; count++ {
		i := b.outputs.lowerBound(position)
		protected[i] = true
		position = b.outputs.prefix(i) - 1
	}
	first, last, cursor := m.transcriptRange(m.TranscriptWindowStart, m.TranscriptWindowEnd)
	for slot := first; slot < last; slot = m.nextTranscriptSlot(slot, cursor) {
		block := m.TranscriptBlocks[slot]
		cursor += block.LineCount
		if block.MsgIndex >= 0 && block.LineCount > 0 {
			protected[int(block.MsgIndex)] = true
		}
	}
	trimmed := false
	position = 0
	for b.outputBytes > markdownRenderCacheBudgetBytes && position < b.outputs.prefix(len(b.bytes)) {
		i := b.outputs.lowerBound(position)
		if protected[i] || m.Messages[i].Streaming {
			position = b.outputs.prefix(i + 1)
			continue
		}
		m.Messages[i].Render.Output = ""
		m.accountDisplayMessage(i)
		trimmed = true
	}
	return trimmed
}
