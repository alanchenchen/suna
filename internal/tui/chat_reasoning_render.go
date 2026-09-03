package tui

import (
	"fmt"
	"strings"
	"time"

	textutil "github.com/alanchenchen/suna/internal/tui/components/text"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
)

const (
	reasoningSummarySourceLines  = 80
	reasoningSummaryTailBytes    = 8 * 1024
	reasoningRunningMaxRows      = 5
	reasoningCompletedMaxRows    = 3
	reasoningRunningMaxRowsCap   = 8
	reasoningCompletedMaxRowsCap = 5
)

func (t *TUI) renderThinkingBox(content string, running bool, startedAt, endedAt time.Time) string {
	return t.renderThinkingBoxMode(content, running, false, startedAt, endedAt)
}

func (t *TUI) renderThinkingBoxMode(content string, running, detail bool, startedAt, endedAt time.Time) string {
	detail = detail && !running
	width := max(24, min(t.width-8, 100))
	inner := width - 4
	elapsed := reasoningElapsed(running, startedAt, endedAt)
	title := t.tr("tui.chat.thinking")
	if running {
		// spinner 与耗时都使用等宽占位符，最终在 viewChat() 中替换，避免 tick 触发全量重建。
		title = fmt.Sprintf("%s %s%s", spinnerPlaceholder, t.tr("tui.chat.thinking"), liveElapsedPlaceholder(startedAt))
	} else if elapsed > 0 {
		// completed 状态保留精确时长，不含 spinner，不会因 tick 变化。
		title = fmt.Sprintf("✓ %s %.1fs", t.tr("tui.chat.thinking"), elapsed.Seconds())
	}
	display := strings.TrimSpace(content)
	if running && display == "" {
		display = t.tr("status.thinking")
	}
	if !detail {
		trimmed := strings.TrimSpace(clipTailBytes(display, reasoningSummaryTailBytes))
		if running {
			display = renderStreamingText(trimmed, inner)
		} else {
			display = renderStreamingText(clipHeadLinesBytes(trimmed, reasoningSummarySourceLines, reasoningSummaryTailBytes), inner)
		}
		if strings.TrimSpace(display) == "" {
			display = t.tr("tui.chat.thought_done")
		}
	} else {
		// 展开态渲染 markdown：思考链中常有代码块，纯文本会丢失代码高亮与格式，
		// 用户主动展开查看完整内容时 md 渲染更清晰。折叠态保持纯文本（截断预览）。
		display = RenderMarkdown(strings.TrimSpace(content), inner)
	}
	lines := strings.Split(strings.TrimRight(display, "\n"), "\n")
	body := make([]string, 0, len(lines))
	for _, line := range lines {
		body = append(body, textutil.WrapLine(line, inner)...)
	}
	body, truncated := t.limitThinkingBodyRows(body, detail, running)
	// 只有内容被截断时才提示展开：未截断时 Ctrl+R 展开前后一致，提示是噪音。
	if !running && !detail && truncated {
		title += " · " + t.tr("tui.chat.thinking_detail_hint")
	}
	return textutil.IndentLines(renderThinkingRoundBox(width, title, body), transcriptBlockIndent) + "\n"
}

// limitThinkingBodyRows 截断思考内容到固定行数，返回截断后的行与是否发生截断。
// running 显示尾部（最新思考），completed 显示头部（早期思考）；
// 省略行用样式化文案（含折叠行数，i18n），比裸 "..." 更有信息量。
func (t *TUI) limitThinkingBodyRows(lines []string, detail bool, running bool) ([]string, bool) {
	lines = trimEmptyThinkingRows(lines)
	if detail && !running {
		return lines, false
	}
	maxRows := t.reasoningMaxRows(running)
	if len(lines) <= maxRows {
		return lines, false
	}
	folded := len(lines) - (maxRows - 1)
	ellipsis := styleDim.Render(t.i18n.Tf("tui.chat.thinking_folded", folded))
	if running {
		return append([]string{ellipsis}, lines[len(lines)-maxRows+1:]...), true
	}
	return append(append([]string(nil), lines[:maxRows-1]...), ellipsis), true
}

// reasoningMaxRows 按终端高度自适应思考链行数：小终端保持下限不挤占对话区，
// 大终端提升到上限展示更多思考过程。高度为 0（测试/未初始化）时回落下限。
func (t *TUI) reasoningMaxRows(running bool) int {
	if running {
		return min(reasoningRunningMaxRowsCap, max(reasoningRunningMaxRows, t.height/10))
	}
	return min(reasoningCompletedMaxRowsCap, max(reasoningCompletedMaxRows, t.height/12))
}

func trimEmptyThinkingRows(lines []string) []string {
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	end := len(lines)
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return append([]string(nil), lines[start:end]...)
}

func reasoningElapsed(running bool, startedAt, endedAt time.Time) time.Duration {
	if startedAt.IsZero() {
		return 0
	}
	if running {
		return time.Since(startedAt).Truncate(100 * time.Millisecond)
	}
	if endedAt.IsZero() || endedAt.Before(startedAt) {
		return 0
	}
	return endedAt.Sub(startedAt).Truncate(100 * time.Millisecond)
}

func (t *TUI) renderReasoningMessage(msg *chatMsg) string {
	content, _ := msg.Content.(string)
	if msg.Stream != nil {
		content = msg.Stream.Text()
	}
	mode := t.chat.ReasoningMode(msg)
	if !msg.Streaming && msg.Render.Width == t.width && msg.Render.Theme == currentTheme.Name && msg.Render.ContentLen == len(content) && msg.Render.Mode == mode && msg.Render.Output != "" {
		return msg.Render.Output
	}
	detail := mode == "reasoning_detail"
	out := t.renderThinkingBoxMode(content, msg.Streaming, detail, msg.StartedAt, msg.EndedAt)
	msg.Render = msgRenderCache{Width: t.width, Theme: currentTheme.Name, ContentLen: len(content), LineCount: chatpage.RenderedLineCount(out), Output: out, Mode: mode}
	return out
}
