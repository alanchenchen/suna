package chat

import (
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/tui/components/toolview"
)

const (
	// 只向 viewport 喂可见区域上下各一屏，避免长历史滚动时终端处理完整 transcript。
	transcriptOverscanScreens = 1
	// Markdown 渲染缓存只保留有界输出；原始消息始终保留，淘汰后可按需重新渲染。
	markdownRenderCacheBudgetBytes = 32 * 1024 * 1024
	markdownRenderCacheRecentKeep  = 6
)

type TranscriptDeps struct {
	Width         int
	MarkdownWidth int
	Theme         string
	RenderAll     bool

	SunaLabel     string
	AskHelp       string
	AskChoiceHelp string

	RenderSunaHeader     func(string) string
	RenderUserMessage    func(any, int) string
	RenderAssistant      func(*Msg) string
	RenderReasoning      func(*Msg) string
	RenderToolBlock      func(*toolview.Block) string
	RenderError          func(string) string
	RenderRestoreSummary func(string) string
	RenderSkillLoad      func(protocol.SkillLoadParams) string
	RenderSkillReview    func(protocol.SkillReviewParams) string
	RenderSystem         func(string) string
	RenderAskSelected    func(string) string
	RenderAskOption      func(string) string
	RenderAskHelp        func(string) string
	RenderModelPicker    func() string
	RenderStatusLine     func() string
	HasVisibleProgress   func() bool
}

type transcriptBlock struct {
	MsgIndex  int
	Streaming bool
	Text      string
	LineCount int
}

type transcriptWindowSignature struct {
	Start      int
	End        int
	Width      int
	Height     int
	TotalLines int
	Hash       uint64
}

func (m *Model) SyncTranscript(deps TranscriptDeps) {
	followBottom := m.ForceBottom || m.FollowBottom || m.TranscriptAtBottom()
	if m.ForceBottom {
		m.ForceBottom = false
	}
	blocks, nav := m.RenderTranscriptBlocksWithNav(deps)
	m.TranscriptBlocks = blocks
	m.recomputeTranscriptLayout()
	m.applyResponseNav(nav)
	if followBottom {
		m.SetTranscriptYOffset(m.TranscriptMaxYOffset())
		m.FollowBottom = true
	} else {
		m.SetTranscriptYOffset(m.TranscriptYOffset)
		m.FollowBottom = m.TranscriptAtBottom()
	}
	if m.trimMarkdownRenderCache() {
		blocks, nav = m.RenderTranscriptBlocksWithNav(deps)
		m.TranscriptBlocks = blocks
		m.recomputeTranscriptLayout()
		m.applyResponseNav(nav)
		if followBottom {
			m.SetTranscriptYOffset(m.TranscriptMaxYOffset())
			return
		}
		m.SetTranscriptYOffset(m.TranscriptYOffset)
	}
}

type ResponseNavInfo struct {
	StartLine int
	LineCount int
	MsgIndex  int
	Streaming bool
}

// RenderTranscript 负责 Chat transcript 的结构编排；具体样式与 markdown 渲染由 root 注入。
func (m Model) RenderTranscript(deps TranscriptDeps) string {
	deps.RenderAll = true
	blocks, _ := m.RenderTranscriptBlocksWithNav(deps)
	lines := make([]string, 0)
	for _, block := range blocks {
		lines = append(lines, blockLineRange(block.Text, 0, block.LineCount)...)
	}
	return strings.Join(lines, "\n")
}

func (m Model) RenderTranscriptWithNav(deps TranscriptDeps) (string, ResponseNavInfo) {
	deps.RenderAll = true
	blocks, nav := m.RenderTranscriptBlocksWithNav(deps)
	lines := make([]string, 0)
	for _, block := range blocks {
		lines = append(lines, blockLineRange(block.Text, 0, block.LineCount)...)
	}
	return strings.Join(lines, "\n"), nav
}

func (m Model) RenderTranscriptBlocksWithNav(deps TranscriptDeps) ([]transcriptBlock, ResponseNavInfo) {
	var blocks []transcriptBlock
	var nav ResponseNavInfo
	lineCount := 0
	inSunaBlock := false
	addBlockWithLineCount := func(msgIndex int, streaming bool, text string, lines int) {
		if lines <= 0 {
			return
		}
		blocks = append(blocks, transcriptBlock{MsgIndex: msgIndex, Streaming: streaming, Text: text, LineCount: lines})
		lineCount += lines
	}
	addBlock := func(msgIndex int, streaming bool, text string) {
		if text == "" {
			return
		}
		addBlockWithLineCount(msgIndex, streaming, text, blockLines(text))
	}
	renderSunaHeader := func() {
		if inSunaBlock {
			return
		}
		if deps.RenderSunaHeader != nil {
			addBlock(-1, false, deps.RenderSunaHeader(deps.SunaLabel))
		}
		inSunaBlock = true
	}

	for i := range m.Messages {
		msg := &m.Messages[i]
		switch msg.Role {
		case "user":
			if deps.RenderUserMessage != nil {
				addBlock(i, msg.Streaming, "\n"+deps.RenderUserMessage(msg.Content, maxInt(20, deps.Width-8))+"\n")
			}
			inSunaBlock = false
		case "assistant":
			renderSunaHeader()
			startLine := lineCount
			if deps.RenderAssistant != nil {
				// 非窗口内的已完成 assistant 只复用行数元数据，不渲染大段 Markdown 文本。
				if cachedLines, ok := m.cachedAssistantBlockLines(msg, deps); ok && !deps.RenderAll && !m.shouldRenderBlockText(startLine, cachedLines) {
					addBlockWithLineCount(i, msg.Streaming, "", cachedLines)
				} else {
					addBlock(i, msg.Streaming, deps.RenderAssistant(msg)+"\n")
				}
			}
			endLine := lineCount
			nav = ResponseNavInfo{StartLine: startLine, LineCount: maxInt(0, endLine-startLine), MsgIndex: i, Streaming: msg.Streaming}
		case "reasoning":
			renderSunaHeader()
			if deps.RenderReasoning != nil {
				addBlock(i, msg.Streaming, deps.RenderReasoning(msg))
			}
		case "tool":
			if v, ok := msg.Content.(*toolview.Block); ok {
				renderSunaHeader()
				if deps.RenderToolBlock != nil {
					addBlock(i, msg.Streaming, deps.RenderToolBlock(v))
				}
			}
		case "error":
			content, _ := msg.Content.(string)
			if deps.RenderError != nil {
				addBlock(i, msg.Streaming, "\n"+deps.RenderError(content)+"\n")
			}
			inSunaBlock = false
		case "restore_summary":
			content, _ := msg.Content.(string)
			if deps.RenderRestoreSummary != nil {
				addBlock(i, msg.Streaming, "\n"+deps.RenderRestoreSummary(content)+"\n")
			}
			inSunaBlock = false
		case "panel":
			content, _ := msg.Content.(string)
			addBlock(i, msg.Streaming, "\n"+content+"\n")
			inSunaBlock = false
		case "skill":
			if p, ok := msg.Content.(protocol.SkillLoadParams); ok && deps.RenderSkillLoad != nil {
				addBlock(i, msg.Streaming, "\n"+deps.RenderSkillLoad(p)+"\n")
			}
			inSunaBlock = false
		case "skill_review":
			if p, ok := msg.Content.(protocol.SkillReviewParams); ok && deps.RenderSkillReview != nil {
				addBlock(i, msg.Streaming, "\n"+deps.RenderSkillReview(p)+"\n")
			}
			inSunaBlock = false
		default:
			content, _ := msg.Content.(string)
			if deps.RenderSystem != nil {
				addBlock(i, msg.Streaming, "\n"+deps.RenderSystem(content)+"\n")
			}
			inSunaBlock = false
		}
	}

	if ask := m.ActiveAsk(); ask != nil && len(ask.Options) > 0 {
		for i, opt := range ask.Options {
			if i == ask.Cursor {
				if deps.RenderAskSelected != nil {
					addBlock(-1, false, deps.RenderAskSelected(opt))
				}
			} else if deps.RenderAskOption != nil {
				addBlock(-1, false, deps.RenderAskOption(opt))
			}
		}
		help := deps.AskHelp
		if !ask.AllowCustom {
			help = deps.AskChoiceHelp
		}
		if deps.RenderAskHelp != nil {
			addBlock(-1, false, deps.RenderAskHelp(help))
		}
	}
	if m.ModelPickerOpen && deps.RenderModelPicker != nil {
		addBlock(-1, false, deps.RenderModelPicker())
	}

	visibleProgress := false
	if deps.HasVisibleProgress != nil {
		visibleProgress = deps.HasVisibleProgress()
	}
	if m.Loading && m.PhaseStart.After(time.Time{}) && !visibleProgress {
		renderSunaHeader()
		if deps.RenderStatusLine != nil {
			addBlock(-1, false, deps.RenderStatusLine())
		}
	}
	return blocks, nav
}

func (m *Model) recomputeTranscriptLayout() {
	total := 0
	for _, block := range m.TranscriptBlocks {
		total += block.LineCount
	}
	m.TranscriptTotalLines = total
}

func (m Model) cachedAssistantBlockLines(msg *Msg, deps TranscriptDeps) (int, bool) {
	if msg.Streaming || msg.Role != "assistant" || msg.Render.LineCount <= 0 {
		return 0, false
	}
	if msg.Render.Width != deps.MarkdownWidth || msg.Render.Theme != deps.Theme {
		return 0, false
	}
	return msg.Render.LineCount + 1, true
}

func (m Model) shouldRenderBlockText(start, lines int) bool {
	if lines <= 0 || m.Viewport.Height() <= 0 {
		return true
	}
	windowStart, windowEnd := m.desiredTranscriptWindowRange()
	return start+lines > windowStart && start < windowEnd
}

func (m Model) desiredTranscriptWindowRange() (int, int) {
	height := m.Viewport.Height()
	if height <= 0 {
		return 0, m.TranscriptTotalLines
	}
	overscan := height * transcriptOverscanScreens
	start := maxInt(0, m.TranscriptYOffset-overscan)
	end := minInt(m.TranscriptTotalLines, m.TranscriptYOffset+height+overscan)
	return start, end
}

func (m *Model) applyTranscriptWindow() {
	height := m.Viewport.Height()
	if height <= 0 {
		lines := m.visibleTranscriptLines(0, m.TranscriptTotalLines)
		m.applyTranscriptWindowLines(0, m.TranscriptTotalLines, lines)
		return
	}
	start, end := m.desiredTranscriptWindowRange()
	lines := m.visibleTranscriptLines(start, end)
	m.applyTranscriptWindowLines(start, end, lines)
}

func (m *Model) applyTranscriptWindowLines(start, end int, lines []string) {
	// 内容签名不包含滚动偏移：在 overscan 窗口内滚动时复用同一批内容，只移动 viewport offset。
	sig := transcriptWindowSignature{
		Start:      start,
		End:        end,
		Width:      m.Viewport.Width(),
		Height:     m.Viewport.Height(),
		TotalLines: m.TranscriptTotalLines,
		Hash:       transcriptLinesHash(lines),
	}
	if sig != m.TranscriptWindowSignature {
		m.TranscriptWindowStart = start
		m.TranscriptWindowEnd = end
		m.TranscriptWindowSignature = sig
		m.Viewport.SetContentLines(lines)
	}
	m.Viewport.SetYOffset(m.TranscriptYOffset - start)
}

func (m Model) visibleTranscriptLines(start, end int) []string {
	if end <= start || len(m.TranscriptBlocks) == 0 {
		return nil
	}
	lines := make([]string, 0, end-start)
	cursor := 0
	for _, block := range m.TranscriptBlocks {
		blockEnd := cursor + block.LineCount
		if blockEnd <= start {
			cursor = blockEnd
			continue
		}
		if cursor >= end {
			break
		}
		from := maxInt(0, start-cursor)
		to := minInt(block.LineCount, end-cursor)
		lines = append(lines, blockLineRange(block.Text, from, to)...)
		cursor = blockEnd
	}
	return lines
}

func (m *Model) SetTranscriptYOffset(offset int) {
	m.TranscriptYOffset = clampInt(offset, 0, m.TranscriptMaxYOffset())
	m.applyTranscriptWindow()
}

func (m Model) canReuseTranscriptWindow(offset int) bool {
	if m.Viewport.Height() <= 0 || m.TranscriptWindowEnd <= m.TranscriptWindowStart {
		return false
	}
	return offset >= m.TranscriptWindowStart && offset+m.Viewport.Height() <= m.TranscriptWindowEnd
}

func (m *Model) ScrollTranscript(delta int) {
	newOffset := clampInt(m.TranscriptYOffset+delta, 0, m.TranscriptMaxYOffset())
	if newOffset != m.TranscriptYOffset {
		m.TranscriptYOffset = newOffset
		// 滚动仍在当前 overscan 窗口内时，不重新切片/重设 viewport 内容，只移动窗口内偏移。
		if m.canReuseTranscriptWindow(newOffset) {
			m.Viewport.SetYOffset(newOffset - m.TranscriptWindowStart)
		} else {
			m.applyTranscriptWindow()
		}
	}
	m.FollowBottom = m.TranscriptAtBottom()
}

func (m *Model) PageTranscript(direction int) {
	delta := maxInt(1, m.Viewport.Height()/2)
	if direction < 0 {
		delta = -delta
	}
	m.ScrollTranscript(delta)
}

func (m Model) TranscriptMaxYOffset() int {
	return maxInt(0, m.TranscriptTotalLines-m.Viewport.Height())
}

func (m Model) TranscriptAtBottom() bool {
	return m.TranscriptYOffset >= m.TranscriptMaxYOffset()
}

func (m *Model) applyResponseNav(nav ResponseNavInfo) {
	if nav.MsgIndex < 0 || nav.LineCount <= 0 || nav.Streaming {
		return
	}
	if nav.MsgIndex != m.LastAssistantMsgIndex || nav.StartLine != m.LastAssistantStartLine || nav.LineCount != m.LastAssistantLineCount {
		m.LastAssistantStartLine = nav.StartLine
		m.LastAssistantLineCount = nav.LineCount
		m.LastAssistantMsgIndex = nav.MsgIndex
		m.ResponseNavJumped = false
		m.ResponseNavDismissed = false
	}
	m.ResponseNavAvailable = m.LastAssistantLineCount > m.Viewport.Height()
}

func (m *Model) JumpToLastAssistantStart() bool {
	if !m.ResponseNavAvailable {
		return false
	}
	m.SetTranscriptYOffset(maxInt(0, m.LastAssistantStartLine))
	m.FollowBottom = false
	m.ResponseNavJumped = true
	m.ResponseNavDismissed = false
	return true
}

func (m *Model) JumpToBottom() {
	m.SetTranscriptYOffset(m.TranscriptMaxYOffset())
	m.FollowBottom = true
	m.ForceBottom = false
	m.ResponseNavJumped = false
	m.ResponseNavDismissed = true
}

func (m *Model) ClearResponseNav() {
	m.ResponseNavAvailable = false
	m.ResponseNavJumped = false
	m.ResponseNavDismissed = true
}

func transcriptLinesHash(lines []string) uint64 {
	h := fnv.New64a()
	for _, line := range lines {
		_, _ = h.Write([]byte(line))
		_, _ = h.Write([]byte{'\n'})
	}
	return h.Sum64()
}

func (m *Model) trimMarkdownRenderCache() bool {
	// 只裁剪渲染输出，不裁剪原始消息和行数元数据，确保滚回旧内容时能按需恢复。
	used := 0
	for i := range m.Messages {
		used += len(m.Messages[i].Render.Output)
	}
	if used <= markdownRenderCacheBudgetBytes {
		return false
	}
	trimmed := false
	protected := m.protectedMarkdownCacheMessages()
	for i := range m.Messages {
		if used <= markdownRenderCacheBudgetBytes {
			return trimmed
		}
		if protected[i] || m.Messages[i].Streaming || m.Messages[i].Render.Output == "" {
			continue
		}
		used -= len(m.Messages[i].Render.Output)
		m.Messages[i].Render.Output = ""
		trimmed = true
	}
	return trimmed
}

func (m Model) protectedMarkdownCacheMessages() map[int]bool {
	protected := make(map[int]bool)
	for i := len(m.Messages) - 1; i >= 0 && len(protected) < markdownRenderCacheRecentKeep; i-- {
		if m.Messages[i].Render.Output != "" {
			protected[i] = true
		}
	}
	cursor := 0
	for _, block := range m.TranscriptBlocks {
		blockEnd := cursor + block.LineCount
		if blockEnd > m.TranscriptWindowStart && cursor < m.TranscriptWindowEnd && block.MsgIndex >= 0 {
			protected[block.MsgIndex] = true
		}
		cursor = blockEnd
	}
	return protected
}

func RenderedLineCount(text string) int { return blockLines(text) }

func blockLines(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}

func blockLineRange(text string, start, end int) []string {
	if text == "" || end <= start {
		return nil
	}
	lines := make([]string, 0, end-start)
	current := 0
	lineStart := 0
	for i := 0; i <= len(text); i++ {
		if i < len(text) && text[i] != '\n' {
			continue
		}
		if current >= start && current < end {
			lines = append(lines, text[lineStart:i])
		}
		current++
		if current >= end {
			break
		}
		lineStart = i + 1
	}
	return lines
}

func AskSelectedLine(icon, value string) string { return fmt.Sprintf("  %s %s\n", icon, value) }

func clampInt(v, low, high int) int {
	if high < low {
		low, high = high, low
	}
	return minInt(high, maxInt(low, v))
}
