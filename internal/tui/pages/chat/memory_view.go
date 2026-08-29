package chat

import "github.com/alanchenchen/suna/internal/protocol"

type MemoryRowKind int

const (
	MemoryRowItem MemoryRowKind = iota
	MemoryRowClear
)

type MemoryRowView struct {
	Memory   protocol.MemoryItem
	Kind     MemoryRowKind
	Selected bool
}

type MemoryOverlayView struct {
	Rows    []MemoryRowView
	Loading bool
	Error   string
	Total   int
	Width   int
	Inner   int
	Height  int
	Confirm MemoryConfirmMode
}

func (m Model) MemoryOverlayView(width, overlayMaxHeight int) MemoryOverlayView {
	w := max(52, min(92, width-4))
	inner := max(32, w-8)
	bodyHeight := max(5, min(16, overlayMaxHeight-10))
	rows := make([]MemoryRowView, 0, len(m.Memories)+1)
	for i, item := range m.Memories {
		rows = append(rows, MemoryRowView{Memory: item, Kind: MemoryRowItem, Selected: i == m.MemoryCursor})
	}
	// 记忆为空时清空项没有意义，不显示；空状态由渲染层处理。
	if len(m.Memories) > 0 {
		rows = append(rows, MemoryRowView{Kind: MemoryRowClear, Selected: m.MemorySelectionIsClear()})
	}
	return MemoryOverlayView{
		Rows:    rows,
		Loading: m.MemoryLoading && len(m.Memories) == 0,
		Error:   m.MemoryError,
		Total:   len(m.Memories),
		Width:   w,
		Inner:   inner,
		Height:  bodyHeight,
		Confirm: m.MemoryConfirm,
	}
}
