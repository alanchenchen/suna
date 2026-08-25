package scroll

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// LineSource 是虚拟滚动数据源：调用方只暴露总行数和指定行渲染。
// 面板滚动时只请求可见窗口，避免先把长文本完整 materialize 成 []string。
type LineSource interface {
	Len() int
	Line(index int) string
}

// SliceSource 把已有 []string 适配成 LineSource。
type SliceSource []string

func (s SliceSource) Len() int { return len(s) }
func (s SliceSource) Line(index int) string {
	if index < 0 || index >= len(s) {
		return ""
	}
	return s[index]
}

// Sections 顺序拼接多个 LineSource。
type Sections []LineSource

func (s Sections) Len() int {
	total := 0
	for _, section := range s {
		if section != nil {
			total += section.Len()
		}
	}
	return total
}

func (s Sections) Line(index int) string {
	if index < 0 {
		return ""
	}
	for _, section := range s {
		if section == nil {
			continue
		}
		sectionLen := section.Len()
		if index < sectionLen {
			return section.Line(index)
		}
		index -= sectionLen
	}
	return ""
}

// WrappedLineSection 保存原始逻辑行和 wrap 后行数；Line 只切出目标展示行，
// 不保存完整 wrap 结果，适合工具返回这类较长但只显示一屏的文本。
type WrappedLineSection struct {
	lines  []string
	counts []int
	total  int
	width  int
	style  lipgloss.Style
}

func NewWrappedLineSection(content string, width int, style lipgloss.Style) *WrappedLineSection {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	section := &WrappedLineSection{lines: lines, width: width, style: style}
	section.counts = make([]int, len(lines))
	for i, line := range lines {
		count := countWrappedDisplayLines(line, width)
		section.counts[i] = count
		section.total += count
	}
	return section
}

func (s *WrappedLineSection) Len() int {
	if s == nil {
		return 0
	}
	return s.total
}

func (s *WrappedLineSection) Line(index int) string {
	if s == nil || index < 0 {
		return ""
	}
	for i, count := range s.counts {
		if index < count {
			return s.style.Render(wrappedDisplayLineAt(s.lines[i], s.width, index))
		}
		index -= count
	}
	return ""
}

// Window 只渲染 offset 起的 height 行，并顺手夹紧滚动位置。
// total 仍由 source.Len() 提供，用于底部显示 20-40/300 这类进度。
func Window(source LineSource, height int, offset *int) ([]string, int, int) {
	total := 0
	if source != nil {
		total = source.Len()
	}
	if height <= 0 || total == 0 {
		if offset != nil {
			*offset = 0
		}
		return nil, 0, total
	}
	maxOffset := max(0, total-height)
	start := 0
	if offset != nil {
		if *offset < 0 {
			*offset = 0
		}
		if *offset > maxOffset {
			*offset = maxOffset
		}
		start = *offset
	}
	end := min(total, start+height)
	out := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		out = append(out, source.Line(i))
	}
	return out, start, total
}

func countWrappedDisplayLines(line string, width int) int {
	if width <= 0 || line == "" {
		return 1
	}
	count := 1
	currentWidth := 0
	state := byte(0)
	for i := 0; i < len(line); {
		_, cellWidth, n, newState := ansi.GraphemeWidth.DecodeSequenceInString(line[i:], state, nil)
		if n <= 0 {
			n = 1
			cellWidth = 1
		}
		if cellWidth > 0 && currentWidth > 0 && currentWidth+cellWidth > width {
			count++
			currentWidth = 0
		}
		currentWidth += cellWidth
		state = newState
		i += n
	}
	return count
}

func wrappedDisplayLineAt(line string, width, target int) string {
	if target <= 0 && (width <= 0 || lipgloss.Width(line) <= width) {
		return line
	}
	if width <= 0 || line == "" {
		if target == 0 {
			return line
		}
		return ""
	}
	lineIndex := 0
	lineStart := 0
	currentWidth := 0
	state := byte(0)
	for i := 0; i < len(line); {
		_, cellWidth, n, newState := ansi.GraphemeWidth.DecodeSequenceInString(line[i:], state, nil)
		if n <= 0 {
			n = 1
			cellWidth = 1
		}
		if cellWidth > 0 && currentWidth > 0 && currentWidth+cellWidth > width {
			if lineIndex == target {
				return line[lineStart:i]
			}
			lineIndex++
			lineStart = i
			currentWidth = 0
		}
		currentWidth += cellWidth
		state = newState
		i += n
	}
	if lineIndex == target {
		return line[lineStart:]
	}
	return ""
}
