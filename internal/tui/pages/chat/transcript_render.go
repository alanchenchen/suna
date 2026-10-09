package chat

import (
	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/tui/components/toolview"
)

// renderTranscriptMessage 与完整导出共用单消息编排，不维护第二份历史文本。
func (m *Model) renderTranscriptMessage(deps TranscriptDeps, i, start int, inSunaBlock bool) ([]transcriptBlock, ResponseNavInfo, bool) {
	var blocks []transcriptBlock
	var nav ResponseNavInfo
	lineCount := start

	addBlockWithLineCount := func(msgIndex int, streaming bool, text string, lines int, boxKind string) {
		if lines <= 0 {
			return
		}
		blocks = append(blocks, transcriptBlock{MsgIndex: int32(msgIndex), Streaming: streaming, Text: text, LineCount: lines, BoxKind: transcriptBox(boxKind)})
		lineCount += lines
	}
	addBlockWithKind := func(msgIndex int, streaming bool, text string, boxKind string) {
		if text == "" {
			return
		}
		addBlockWithLineCount(msgIndex, streaming, text, blockLines(text), boxKind)
	}
	addBlock := func(msgIndex int, streaming bool, text string) {
		addBlockWithKind(msgIndex, streaming, text, "")
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

	msg := &m.Messages[i]
	switch msg.Role {
	case "user":
		if deps.RenderUserMessage != nil {
			addBlock(i, msg.Streaming, "\n"+deps.RenderUserMessage(msg.Content, max(20, deps.Width-8))+"\n")
		}
		inSunaBlock = false
	case "assistant":
		renderSunaHeader()
		startLine := lineCount
		if deps.RenderAssistant != nil {
			// 非窗口内的已完成 assistant 只复用行数元数据，不渲染大段 Markdown 文本。
			if cachedLines, ok := m.cachedAssistantBlockLines(msg, deps); ok && !deps.RenderAll && !m.shouldRenderBlockText(startLine, cachedLines) {
				addBlockWithLineCount(i, msg.Streaming, "", cachedLines, "")
			} else {
				addBlock(i, msg.Streaming, deps.RenderAssistant(msg)+"\n")
			}
		}
		endLine := lineCount
		nav = ResponseNavInfo{StartLine: startLine, LineCount: max(0, endLine-startLine), MsgIndex: i, Streaming: msg.Streaming}
	case "run_duration":
		if content, ok := msg.Content.(string); ok && deps.RenderRunDuration != nil {
			addBlock(i, false, deps.RenderRunDuration(content, msg.EndedAt)+"\n")
		}
	case "reasoning":
		renderSunaHeader()
		startLine := lineCount
		if deps.RenderReasoning != nil {
			mode := "reasoning_collapsed"
			if deps.ReasoningMode != nil {
				mode = deps.ReasoningMode(msg)
			}
			// 思考链展开时可能很长；离屏时只复用行数，避免滚动历史后仍反复渲染不可见内容。
			if cachedLines, ok := m.cachedRenderedBlockLines(msg, deps.Width, deps.Theme, mode); ok && !deps.RenderAll && !m.shouldRenderBlockText(startLine, cachedLines) {
				addBlockWithLineCount(i, msg.Streaming, "", cachedLines, "")
			} else {
				addBlock(i, msg.Streaming, deps.RenderReasoning(msg))
			}
		}
	case "tool":
		if v, ok := msg.Content.(*toolview.Block); ok {
			renderSunaHeader()
			if deps.RenderToolBlock != nil {
				addBlockWithKind(i, msg.Streaming, deps.RenderToolBlock(v), boxKindTool)
			}
			if deps.RenderSubtaskBlock != nil {
				addBlockWithKind(i, msg.Streaming, deps.RenderSubtaskBlock(v), boxKindSubtask)
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
		renderSunaHeader()
		if p, ok := msg.Content.(*SkillLoadView); ok && deps.RenderSkillLoad != nil {
			addBlock(i, msg.Streaming, deps.RenderSkillLoad(p)+"\n")
		}
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
	if m.displayBudget.ready {
		m.displayBudget.dirty[i] = true
	}
	return blocks, nav, inSunaBlock
}
