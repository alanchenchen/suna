package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/alanchenchen/suna/internal/tui/components/toolview"
)

// 就地展开不是模态：展开工具块后，transcript 文本选区与输入历史仍应可用。
// 这些用例锁定"展开态只接管滚轮与块内导航，不屏蔽其他交互"的边界。
func TestExpandedBlockKeepsTranscriptSelection(t *testing.T) {
	tui := newEdgeTUI(t)
	block := &toolview.Block{Order: []string{"t1"}, Entries: map[string]*toolview.Entry{}}
	block.Entries["t1"] = &toolview.Entry{ID: "t1", RawName: "readfile", Status: toolview.StatusDone}
	tui.chat.Messages = append(tui.chat.Messages, chatMsg{Role: "tool", Content: block})
	tui.chat.ExpandedBlock = block
	tui.chat.ExpandedBoxKind = "tool"
	tui.syncContent() // 选区手势依赖已渲染的 transcript 行数

	if tui.chat.TranscriptTotalLines <= 0 {
		t.Fatal("test setup: transcript must have rendered lines")
	}
	press := tea.MouseMsg(tea.MouseClickMsg(tea.Mouse{X: 4, Y: 6, Button: tea.MouseLeft}))
	if consumed, _ := tui.handleSelectionMouse(press); !consumed {
		t.Fatal("expanded block must not block transcript selection gestures")
	}
}

func TestExpandedBlockKeepsInputHistory(t *testing.T) {
	tui := newEdgeTUI(t)
	block := &toolview.Block{Order: []string{"t1"}, Entries: map[string]*toolview.Entry{}}
	block.Entries["t1"] = &toolview.Entry{ID: "t1", RawName: "readfile", Status: toolview.StatusDone}
	tui.chat.Messages = append(tui.chat.Messages, chatMsg{Role: "tool", Content: block})
	tui.chat.ExpandedBlock = block
	tui.chat.ExpandedBoxKind = "tool"

	if !tui.canBrowseInputHistory() {
		t.Fatal("expanded block must not disable input history browsing")
	}
}
