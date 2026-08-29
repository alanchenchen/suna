package tui

// SelectionRegion 表示选区所在的区域：transcript 内容区或输入区。
// 输入区选区用于复制输入框草稿（textarea 不暴露行样式，输入区不做高亮）。
type SelectionRegion int

const (
	// SelectionRegionTranscript 是 transcript 内容区选区（默认）。
	SelectionRegionTranscript SelectionRegion = iota
	// SelectionRegionInput 是输入区选区（复制输入框草稿）。
	SelectionRegionInput
)

// Selection 表示 transcript 内容行上的鼠标选区，锚定内容行索引（非屏幕行），
// 滚动跟随内容不丢失。状态机与输入设备无关：鼠标/键盘都通过同一套方法驱动，
// 便于后续接入键盘选区（shift+方向扩展）而不改核心逻辑。
type Selection struct {
	// Active 表示拖动中（MouseDown 后、MouseUp 前），此时选区随鼠标实时扩展。
	Active bool
	// AnchorLine 是选区起点内容行（按下时的行）。
	AnchorLine int
	// EndLine 是当前终点内容行（拖动中的当前行）。
	EndLine int
	// HasSelection 表示是否有定格选区（拖动结束、可复制）。
	HasSelection bool
	// Dragged 表示拖动中是否发生过移动（Extend）。
	// 按下后未移动就释放视为单击（清除选区），不产生定格选区。
	Dragged bool
	// Region 是选区所在区域（transcript 或输入区）。
	Region SelectionRegion
}

// Begin 在鼠标按下时设置锚点：锚点与终点都落在按下行，进入拖动态。
// region 指定选区所在区域（transcript 或输入区）。
func (s *Selection) Begin(line int, region SelectionRegion) {
	s.Active = true
	s.AnchorLine = line
	s.EndLine = line
	s.HasSelection = false
	s.Dragged = false
	s.Region = region
}

// Extend 在拖动中更新终点行，并 clamp 到内容行范围内。
// 反向拖动（终点 < 锚点）由 LineRange 统一排序，这里不特殊处理。
// 首次移动会标记 Dragged，用于区分单击（未移动即释放）与拖动。
func (s *Selection) Extend(line, totalLines int) {
	if !s.Active {
		return
	}
	if totalLines <= 0 {
		return
	}
	s.EndLine = min(max(line, 0), totalLines-1)
	s.Dragged = true
}

// Finish 在鼠标释放时结束拖动态。
// 若拖动中从未移动（单击），视为清除选区；否则选区定格为可复制状态。
func (s *Selection) Finish() {
	if !s.Active {
		return
	}
	s.Active = false
	if !s.Dragged {
		s.Clear()
		return
	}
	s.HasSelection = true
}

// Clear 清除选区（单击重置或 Esc），回到无选区状态。
func (s *Selection) Clear() {
	s.Active = false
	s.HasSelection = false
	s.AnchorLine = 0
	s.EndLine = 0
	s.Region = SelectionRegionTranscript
}

// LineRange 返回有序的 [start, end] 内容行范围（含端点），供高亮与复制使用。
// 反向拖动（终点 < 锚点）时自动排序。无选区时返回 (0, -1) 表示空。
func (s *Selection) LineRange() (int, int) {
	if !s.HasSelection && !s.Active {
		return 0, -1
	}
	start, end := s.AnchorLine, s.EndLine
	if start > end {
		start, end = end, start
	}
	return start, end
}

// Contains 判断内容行是否在选区内（含端点）。用于高亮渲染。
func (s *Selection) Contains(line int) bool {
	start, end := s.LineRange()
	if end < start {
		return false
	}
	return line >= start && line <= end
}

// HasAny 表示是否存在选区（拖动中或已定格），用于 y 键拦截与高亮判断。
func (s *Selection) HasAny() bool {
	return s.Active || s.HasSelection
}
