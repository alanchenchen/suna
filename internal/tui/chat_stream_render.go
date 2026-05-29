package tui

import "strings"

func (t *TUI) renderAssistantMessage(msg *chatMsg) string {
	content, _ := msg.content.(string)
	width := max(20, t.width-6)
	if msg.streaming {
		// 流式阶段避免每个 delta 都跑 Glamour；最终 done 后仍使用完整 Markdown 渲染。
		return indentLines(renderStreamingText(content, width), "  ")
	}
	return indentLines(t.cachedMarkdown(msg, content, width), "  ")
}

func (t *TUI) renderReasoningMessage(msg *chatMsg) string {
	content, _ := msg.content.(string)
	return t.renderThinkingBox(content, msg.streaming, msg.startedAt, msg.endedAt)
}

func (t *TUI) cachedMarkdown(msg *chatMsg, content string, width int) string {
	cache := msg.render
	if cache.width == width && cache.theme == currentTheme.Name && cache.content == content {
		return cache.output
	}
	out := RenderMarkdown(content, width)
	msg.render = msgRenderCache{width: width, theme: currentTheme.Name, content: content, output: out}
	return out
}

func renderStreamingText(content string, width int) string {
	if content == "" {
		return ""
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimRight(content, "\n"), "\n") {
		lines = append(lines, wrapLine(line, width)...)
	}
	return strings.Join(lines, "\n")
}
