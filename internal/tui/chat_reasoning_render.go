package tui

import (
	"fmt"
	"strings"
	"time"

	textutil "github.com/alanchenchen/suna/internal/tui/components/text"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
)

const (
	// 思考框本身只展示少量行，流式阶段先裁剪源文本，避免每帧对完整思考链 wrap/markdown。
	reasoningDetailSourceBytes = 32 * 1024
	reasoningDetailSourceLines = 80
	reasoningSummaryTailBytes  = 8 * 1024
	reasoningRunningMaxRows    = 5
	reasoningCompletedMaxRows  = 3
	reasoningDetailMaxRows     = 10
)

func (t *TUI) renderThinkingBox(content string, running bool, startedAt, endedAt time.Time) string {
	width := max(24, min(t.width-8, 62))
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
	if !running && !t.chat.ShowReasoningDetail {
		title += " · " + t.tr("tui.chat.thinking_detail_hint")
	}
	display := strings.TrimSpace(content)
	if running && display == "" {
		display = t.tr("status.thinking")
	}
	if !t.chat.ShowReasoningDetail {
		trimmed := strings.TrimSpace(clipTailBytes(display, reasoningSummaryTailBytes))
		if running {
			display = renderStreamingText(trimmed, inner)
		} else {
			display = renderStreamingText(clipHeadLinesBytes(trimmed, reasoningDetailSourceLines, reasoningSummaryTailBytes), inner)
		}
		if strings.TrimSpace(display) == "" {
			display = t.tr("tui.chat.thought_done")
		}
	} else {
		trimmed := strings.TrimSpace(content)
		if running {
			trimmed = clipTailLinesBytes(trimmed, reasoningDetailSourceLines, reasoningDetailSourceBytes)
			display = renderStreamingText(trimmed, inner)
		} else {
			trimmed = clipHeadLinesBytes(trimmed, reasoningDetailSourceLines, reasoningDetailSourceBytes)
			display = RenderMarkdown(trimmed, inner)
		}
	}
	lines := strings.Split(strings.TrimRight(display, "\n"), "\n")
	body := make([]string, 0, len(lines))
	for _, line := range lines {
		body = append(body, textutil.WrapLine(line, inner)...)
	}
	body = limitThinkingBodyRows(body, t.chat.ShowReasoningDetail, running)
	return textutil.IndentLines(renderThinkingRoundBox(width, title, body), transcriptBlockIndent) + "\n"
}
func limitThinkingBodyRows(lines []string, detail bool, running bool) []string {
	maxRows := reasoningCompletedMaxRows
	if detail {
		maxRows = reasoningDetailMaxRows
	} else if running {
		maxRows = reasoningRunningMaxRows
	}
	lines = trimEmptyThinkingRows(lines)
	if len(lines) <= maxRows {
		return lines
	}
	if running {
		return append([]string{"..."}, lines[len(lines)-maxRows+1:]...)
	}
	return append(append([]string(nil), lines[:maxRows-1]...), "...")
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
	out := t.renderThinkingBox(content, msg.Streaming, msg.StartedAt, msg.EndedAt)
	msg.Render = msgRenderCache{Width: t.width, Theme: currentTheme.Name, ContentLen: len(content), LineCount: chatpage.RenderedLineCount(out), Output: out, Mode: reasoningRenderMode(t.chat.ShowReasoningDetail)}
	return out
}

func reasoningRenderMode(detail bool) string {
	if detail {
		return "reasoning_detail"
	}
	return "reasoning_collapsed"
}
