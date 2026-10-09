package chat

import (
	"fmt"
	"sort"
	"strings"

	"github.com/alanchenchen/suna/internal/tui/components/toolview"
)

// 每种角色只预留其可能使用的块；工具的两个盒子和标题保持固定槽位。
type transcriptSource struct {
	slot     int32
	nextSuna bool
	slots    uint8
}

// 只有参与工具、技能或思考链查找的消息需要保存旧所有权。
type transcriptOwner struct {
	block *toolview.Block
	skill string
	id    uint64
}

// 已知批量长度时只分配所需容量，不改变内容，也不影响单条追加的摊销策略。
func reserveTranscriptSlice[T any](values []T, capacity int) []T {
	if cap(values) >= capacity {
		return values
	}
	out := make([]T, len(values), capacity)
	copy(out, values)
	return out
}

func transcriptSlots(role string) int {
	switch role {
	case "tool":
		return 3
	case "assistant", "reasoning", "skill":
		return 2
	default:
		return 1
	}
}

func (m *Model) transcriptMessageRange(i int) (int, int) {
	source := m.transcript.sources[i]
	return int(source.slot), int(source.slot) + int(source.slots)
}

// 角色替换才调整后缀；流式内容和工具结果变化仍只更新固定槽位及对数高度索引。
func (m *Model) resizeTranscriptMessage(i, count int) {
	start, end := m.transcriptMessageRange(i)
	delta := count - (end - start)
	if delta == 0 {
		return
	}
	if delta > 0 {
		m.TranscriptBlocks = append(m.TranscriptBlocks, make([]transcriptBlock, delta)...)
		copy(m.TranscriptBlocks[end+delta:], m.TranscriptBlocks[end:len(m.TranscriptBlocks)-delta])
	} else {
		copy(m.TranscriptBlocks[end+delta:], m.TranscriptBlocks[end:])
		clear(m.TranscriptBlocks[len(m.TranscriptBlocks)+delta:])
		m.TranscriptBlocks = m.TranscriptBlocks[:len(m.TranscriptBlocks)+delta]
	}
	clear(m.TranscriptBlocks[start : start+count])
	m.transcript.sources[i].slots = uint8(count)
	for j := i + 1; j < len(m.transcript.sources); j++ {
		m.transcript.sources[j].slot += int32(delta)
	}
	values := make([]int, len(m.TranscriptBlocks))
	for j, block := range m.TranscriptBlocks {
		values[j] = block.LineCount
	}
	m.transcript.heights.reset(values)
}

type transcriptStyleKey struct {
	width, markdownWidth                 int
	theme, label, askHelp, askChoiceHelp string
}

type transcriptLiveState struct {
	current, expanded                                   *toolview.Block
	kind                                                string
	cursor, detail, subtask, subtool, subdetail, result int
	subUser, toolUser, expandedDetail                   bool
	height, reasoningHeight                             int
	subtaskKeys                                         bool
	reasoning                                           uint64
}

type retainedTranscript struct {
	ready         bool
	deps          TranscriptDeps
	style         transcriptStyleKey
	live          transcriptLiveState
	discard       DisplayDiscardSummary
	sources       []transcriptSource
	owners        map[int]transcriptOwner
	heights       heightIndex
	dirty         map[int]bool
	dynamic       map[int]bool
	held          map[int]bool
	blocks        map[*toolview.Block]int
	ids           map[uint64]int
	tools         map[string]int
	skills        map[string]int
	lastAssistant int
}

// MarkMessageDirty 标记原始消息的就地修改；调用方必须在修改后、下一次同步前调用。
// 追加由同步自动发现；替换、重排或删除整个历史应调用 InvalidateTranscript。
func (m *Model) MarkMessageDirty(index int) {
	if index < 0 || index >= len(m.Messages) {
		return
	}
	if m.transcript.dirty == nil {
		m.transcript.dirty = make(map[int]bool)
	}
	m.transcript.dirty[index] = true
	// 显式修改不得复用过期离屏高度，包括等长替换；新建布局仍可复用元数据。
	if m.transcript.ready && index < len(m.transcript.sources) {
		// 下次同步前若又收到事件，也必须保持替换消息的所有权一致。
		m.indexTranscriptMessage(index)
		slot, end := m.transcriptMessageRange(index)
		if slot < len(m.TranscriptBlocks) && m.transcript.heights.prefix(end) > m.transcript.heights.prefix(slot) {
			if m.Messages[index].Role == "assistant" || m.Messages[index].Role == "reasoning" {
				m.Messages[index].Render = MsgRenderCache{}
			}
		}
	}
	m.markDisplayBudgetDirty(index)
}

// InvalidateTranscript 用于历史替换、语言或注入渲染器语义改变；不改变滚动和预算策略。
func (m *Model) InvalidateTranscript() {
	m.transcript = retainedTranscript{}
	m.TranscriptBlocks = nil
	m.displayBudget = displayBudget{}
}

func (m *Model) transcriptLive(deps TranscriptDeps) transcriptLiveState {
	return transcriptLiveState{m.CurrentToolBlock, m.ExpandedBlock, m.ExpandedBoxKind,
		m.ExpandedBlockCursor, m.ExpandedBlockDetailScroll, m.SubtaskCursor, m.SubtaskToolCursor,
		m.SubtaskToolDetailScroll, m.SubtaskResultScroll, m.SubtaskCursorUserSet,
		m.SubtaskToolCursorUserSet, m.SubtaskToolDetailExpanded, m.Viewport.Height(), deps.ReasoningHeight, deps.SubtaskKeys, m.ExpandedReasoningID}
}

func (m *Model) markBlockDirty(block *toolview.Block) {
	if i, ok := m.transcript.blocks[block]; ok {
		m.MarkMessageDirty(i)
		return
	}
	if block != nil && len(m.Messages) > 0 && m.Messages[len(m.Messages)-1].Content == block {
		m.MarkMessageDirty(len(m.Messages) - 1)
	}
}

func (m *Model) SyncTranscript(deps TranscriptDeps) {
	followBottom := !m.ManualScrollPaused && (m.ForceBottom || m.FollowBottom || m.TranscriptAtBottom())
	m.ForceBottom = false
	initial := !m.transcript.ready
	key := transcriptStyleKey{deps.Width, deps.MarkdownWidth, deps.Theme, deps.SunaLabel, deps.AskHelp, deps.AskChoiceHelp}
	if !m.transcript.ready || key != m.transcript.style || len(m.Messages) < len(m.transcript.sources) {
		// 已有历史一次预留准确的角色槽位，避免首次构建留下扩容冗余。
		capacity := 2
		for _, msg := range m.Messages {
			capacity += transcriptSlots(msg.Role)
		}
		m.TranscriptBlocks = make([]transcriptBlock, 0, capacity)
		m.transcript = retainedTranscript{ready: true, style: key, sources: make([]transcriptSource, 0, len(m.Messages)), dirty: make(map[int]bool),
			dynamic: make(map[int]bool), held: make(map[int]bool), blocks: make(map[*toolview.Block]int),
			ids: make(map[uint64]int), tools: make(map[string]int), skills: make(map[string]int), lastAssistant: -1}
		m.TranscriptBlocks = append(m.TranscriptBlocks, transcriptBlock{MsgIndex: -1}, transcriptBlock{MsgIndex: -1})
		m.transcript.heights.tree = make([]int, 3, capacity+1)
	}
	m.transcript.deps = deps
	live := m.transcriptLive(deps)
	if live != m.transcript.live {
		m.markBlockDirty(m.transcript.live.current)
		m.markBlockDirty(m.transcript.live.expanded)
		m.markBlockDirty(live.current)
		m.markBlockDirty(live.expanded)
		if i, ok := m.transcript.ids[m.transcript.live.reasoning]; ok {
			m.MarkMessageDirty(i)
		}
		if i, ok := m.transcript.ids[live.reasoning]; ok {
			m.MarkMessageDirty(i)
		}
		if live.reasoningHeight != m.transcript.live.reasoningHeight {
			for _, i := range m.transcript.ids {
				if m.Messages[i].Role != "reasoning" {
					continue
				}
				m.Messages[i].Render = MsgRenderCache{}
				m.MarkMessageDirty(i)
			}
		}
		m.transcript.live = live
	}
	for i := range m.transcript.dynamic {
		m.MarkMessageDirty(i)
	}
	// 批量载入历史已知最终长度，按角色预留；单条追加保留普通摊销扩容。
	batch := len(m.Messages)-len(m.transcript.sources) > 1
	if batch {
		capacity := len(m.TranscriptBlocks)
		for i := len(m.transcript.sources); i < len(m.Messages); i++ {
			capacity += transcriptSlots(m.Messages[i].Role)
		}
		m.TranscriptBlocks = reserveTranscriptSlice(m.TranscriptBlocks, capacity)
		m.transcript.sources = reserveTranscriptSlice(m.transcript.sources, len(m.Messages))
		m.transcript.heights.tree = reserveTranscriptSlice(m.transcript.heights.tree, capacity+1)
	}
	for len(m.transcript.sources) < len(m.Messages) {
		i := len(m.transcript.sources)
		count := transcriptSlots(m.Messages[i].Role)
		m.transcript.sources = append(m.transcript.sources, transcriptSource{slot: int32(len(m.TranscriptBlocks) - 1), slots: uint8(count)})
		// 尾部交互占一个槽位，追加消息时只移动这个槽位。
		tail := len(m.TranscriptBlocks) - 1
		m.setTranscriptSlot(tail, transcriptBlock{})
		for j := 0; j < count; j++ {
			m.TranscriptBlocks = append(m.TranscriptBlocks, transcriptBlock{})
			m.transcript.heights.append(0)
		}
		m.indexTranscriptMessage(i)
		m.MarkMessageDirty(i)
	}
	if m.transcript.discard != m.DisplayDiscard || m.TranscriptBlocks[0].Text == "" {
		text := ""
		if !m.DisplayDiscard.Empty() {
			if deps.RenderDisplayDiscard != nil {
				text = deps.RenderDisplayDiscard(m.DisplayDiscard)
			} else {
				text = fmt.Sprintf("\n  已释放 %d 轮早期显示历史 · 约 %s\n", m.DisplayDiscard.Turns, m.DisplayDiscard.ApproxMB())
			}
		}
		m.setTranscriptSlot(0, transcriptBlock{MsgIndex: -1, Text: text, LineCount: blockLines(text)})
		m.transcript.discard = m.DisplayDiscard
	}
	indices := make([]int, 0, len(m.transcript.dirty))
	for i := range m.transcript.dirty {
		indices = append(indices, i)
	}
	sort.Ints(indices)
	for _, i := range indices {
		if !m.transcript.dirty[i] {
			continue
		}
		for j := i; j < len(m.Messages); j++ {
			previous := m.transcript.sources[j].nextSuna
			m.refreshTranscriptMessage(j, false)
			delete(m.transcript.dirty, j)
			if previous == m.transcript.sources[j].nextSuna {
				break
			}
		}
	}
	if (initial || batch || len(indices) > 1024) && len(m.transcript.dirty) == 0 {
		m.transcript.dirty = make(map[int]bool)
	}
	m.syncTranscriptAsk(deps)
	m.TranscriptTotalLines = m.transcript.heights.prefix(len(m.TranscriptBlocks))
	m.updateRetainedResponseNav()
	if followBottom {
		m.TranscriptYOffset = m.TranscriptMaxYOffset()
		m.FollowBottom = true
		m.ManualScrollPaused = false
	} else {
		m.TranscriptYOffset = clampInt(m.TranscriptYOffset, 0, m.TranscriptMaxYOffset())
		m.FollowBottom = m.TranscriptAtBottom()
	}
	// 新高度确定后才补水，确保首次跳到底部和滚回已淘汰消息不会出现空白帧。
	m.applyTranscriptWindow()
	m.trimMarkdownRenderCache()
}

func (m *Model) setTranscriptSlot(slot int, block transcriptBlock) {
	m.transcript.heights.add(slot, block.LineCount-m.TranscriptBlocks[slot].LineCount)
	m.TranscriptBlocks[slot] = block
}

func (m *Model) refreshTranscriptMessage(i int, retain bool) {
	deps := m.transcript.deps
	deps.RenderAll = retain
	inSuna := false
	if i > 0 {
		inSuna = m.transcript.sources[i-1].nextSuna
	}
	m.resizeTranscriptMessage(i, transcriptSlots(m.Messages[i].Role))
	slot, endSlot := m.transcriptMessageRange(i)
	start := m.transcript.heights.prefix(slot)
	blocks, _, next := m.renderTranscriptMessage(deps, i, start, inSuna)
	for j := 0; j < endSlot-slot; j++ {
		block := transcriptBlock{MsgIndex: -1}
		if j < len(blocks) {
			block = blocks[j]
		}
		m.setTranscriptSlot(slot+j, block)
	}
	m.transcript.sources[i].nextSuna = next
	// 只保留当前窗口文本，离屏仅存高度；首次布局确定后的补水在同一次同步完成。
	end := m.transcript.heights.prefix(endSlot)
	if retain || m.shouldRenderBlockText(start, end-start) {
		// 标题可能单独与窗口重叠；正文只有高度时不能视为已完整补水。
		if m.transcriptMessageTextReady(i) {
			m.transcript.held[i] = true
		} else {
			delete(m.transcript.held, i)
		}
	} else {
		for j := 0; j < endSlot-slot; j++ {
			m.TranscriptBlocks[slot+j].Text = ""
		}
		delete(m.transcript.held, i)
	}
	msg := &m.Messages[i]
	if msg.ID == 0 {
		m.NextMessageID++
		msg.ID = m.NextMessageID
	} else if msg.ID > m.NextMessageID {
		m.NextMessageID = msg.ID
	}
	if msg.Role == "assistant" {
		m.transcript.lastAssistant = max(i, m.transcript.lastAssistant)
	}
	m.indexTranscriptMessage(i)
	dynamic := msg.Streaming
	if block, ok := msg.Content.(*toolview.Block); ok && block != nil {
		for _, entry := range block.Entries {
			if entry != nil && (entry.Status == toolview.StatusRunning || entry.Status == toolview.StatusCancelling) {
				dynamic = true
				break
			}
		}
	}
	if view, ok := msg.Content.(*SkillLoadView); ok && view != nil && view.Status == "loading" {
		dynamic = true
	}
	if dynamic {
		m.transcript.dynamic[i] = true
	} else {
		delete(m.transcript.dynamic, i)
	}
	m.markDisplayBudgetDirty(i)
	// 首次布局期间也限制渲染输出，而不只限制最终帧。
	m.trimRetainedMarkdownCache()
}

func (m *Model) syncTranscriptAsk(deps TranscriptDeps) {
	var texts []string
	if ask := m.ActiveAsk(); ask != nil && len(ask.Options) > 0 {
		for i, opt := range ask.Options {
			text := ""
			if i == ask.Cursor {
				if deps.RenderAskSelected != nil {
					text = deps.RenderAskSelected(opt)
				}
			} else if deps.RenderAskOption != nil {
				text = deps.RenderAskOption(opt)
			}
			if text != "" {
				texts = append(texts, text)
			}
		}
		help := deps.AskHelp
		if !ask.AllowCustom {
			help = deps.AskChoiceHelp
		}
		if deps.RenderAskHelp != nil {
			if text := deps.RenderAskHelp(help); text != "" {
				texts = append(texts, text)
			}
		}
	}
	text := strings.Join(texts, "\n")
	m.setTranscriptSlot(len(m.TranscriptBlocks)-1, transcriptBlock{MsgIndex: -1, Text: text, LineCount: blockLines(text)})
}

func (m *Model) updateRetainedResponseNav() {
	i := m.transcript.lastAssistant
	if i < 0 || i >= len(m.Messages) {
		return
	}
	if m.Messages[i].Role != "assistant" {
		m.transcript.lastAssistant = -1
		for j := len(m.Messages) - 1; j >= 0; j-- {
			if m.Messages[j].Role == "assistant" {
				m.transcript.lastAssistant = j
				break
			}
		}
		if m.transcript.lastAssistant < 0 {
			// 最后一条回复被替换时，不能留下可跳转的旧坐标。
			m.ClearResponseNav()
			m.LastAssistantStartLine = 0
			m.LastAssistantLineCount = 0
			m.LastAssistantMsgIndex = -1
			return
		}
		m.updateRetainedResponseNav()
		return
	}
	slot, endSlot := m.transcriptMessageRange(i)
	start := 0
	if m.TranscriptBlocks[slot].MsgIndex == -1 {
		start = m.TranscriptBlocks[slot].LineCount
	}
	lines := m.transcript.heights.prefix(endSlot) - m.transcript.heights.prefix(slot) - start
	m.applyResponseNav(ResponseNavInfo{StartLine: m.transcript.heights.prefix(slot) + start, LineCount: lines, MsgIndex: i, Streaming: m.Messages[i].Streaming})
}

// 完整保留以实际文本为准，零高度空槽位不需要补水。
func (m *Model) transcriptMessageTextReady(i int) bool {
	slot, endSlot := m.transcriptMessageRange(i)
	for _, block := range m.TranscriptBlocks[slot:endSlot] {
		if block.LineCount > 0 && block.Text == "" {
			return false
		}
	}
	return true
}

func (m *Model) hydrateTranscriptRange(start, end int) {
	if !m.transcript.ready || end <= start {
		return
	}
	first, _, _ := m.transcriptRange(start, end)
	for slot := first; slot < len(m.TranscriptBlocks) && m.transcript.heights.prefix(slot) < end; slot = m.nextTranscriptSlot(slot, m.transcript.heights.prefix(slot+1)) {
		if slot == 0 || slot == len(m.TranscriptBlocks)-1 {
			continue
		}
		// 标题没有消息编号，按有序源槽位查找其所属消息。
		i := int(m.TranscriptBlocks[slot].MsgIndex)
		if i < 0 {
			i = sort.Search(len(m.transcript.sources), func(j int) bool { return int(m.transcript.sources[j].slot) > slot }) - 1
		}
		if !m.transcript.held[i] || !m.transcriptMessageTextReady(i) {
			m.refreshTranscriptMessage(i, true)
		}
	}
	m.releaseTranscriptOutside(start, end, false)
	m.TranscriptTotalLines = m.transcript.heights.prefix(len(m.TranscriptBlocks))
	m.updateRetainedResponseNav()
}

func (m *Model) releaseTranscriptOutside(start, end int, compact bool) {
	held := m.transcript.held
	if compact {
		m.transcript.held = make(map[int]bool)
	}
	for i := range held {
		slot, endSlot := m.transcriptMessageRange(i)
		blockStart := m.transcript.heights.prefix(slot)
		blockEnd := m.transcript.heights.prefix(endSlot)
		if blockEnd > start && blockStart < end {
			if compact {
				m.transcript.held[i] = true
			}
			continue
		}
		for j := 0; j < endSlot-slot; j++ {
			m.TranscriptBlocks[slot+j].Text = ""
		}
		delete(m.transcript.held, i)
	}
}
