package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// applyMouseSelectionHighlight 在 viewport 渲染内容上叠加拖选高亮（反色）。
// content 是 Viewport.View() 的输出（逐行、每行按宽度补全），行号与屏幕行一一对应：
// 第 i 行对应屏幕行 i + transcriptViewportTop。选区坐标是屏幕坐标，因此
// 高亮行范围是 [anchorY-4, cursorY-4]，列范围是 [minX, maxX]。
//
// 高亮实现：对选中行用 ansi.Cut 切出左/中/右三段，中间段包上 SGR 反色码。
// 行首/行尾的 ANSI 样式（如颜色）会被反色包裹，视觉上呈现"选中"效果。
func (t *TUI) applyMouseSelectionHighlight(content string) string {
	if !t.mouseSel.dragging {
		return content
	}
	x0, y0, x1, y1 := t.mouseSel.selectionBounds()
	lines := strings.Split(content, "\n")
	for i := range lines {
		screenY := i + transcriptViewportTop
		if screenY < y0 || screenY > y1 {
			continue
		}
		lines[i] = highlightLineRange(lines[i], x0, x1)
	}
	return strings.Join(lines, "\n")
}

// highlightLineRange 把一行文本的 [x0, x1] 列区间用反色包裹。
// 列坐标基于剥离样式后的可见宽度；ANSI 转义码不计入列宽。
func highlightLineRange(line string, x0, x1 int) string {
	lineWidth := ansi.StringWidth(line)
	if lineWidth == 0 || x0 >= lineWidth {
		return line
	}
	end := min(x1+1, lineWidth)
	// Cut 按可见列切分，保留 ANSI 结构；三段拼接时中间段加反色。
	left := ansi.Cut(line, 0, x0)
	mid := ansi.Cut(line, x0, end)
	right := ansi.Cut(line, end, lineWidth)
	return left + "\x1b[7m" + mid + "\x1b[27m" + right
}
