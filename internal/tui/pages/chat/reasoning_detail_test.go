package chat

import (
	"testing"
	"time"
)

func TestToggleVisibleReasoningDetailSkipsStreamingBlock(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	m.Viewport.SetHeight(12)
	m.AppendMessage(Msg{Role: "reasoning", Content: "history"})
	m.AppendMessage(Msg{Role: "reasoning", Content: "streaming now", Streaming: true})
	m.TranscriptBlocks = []transcriptBlock{
		{MsgIndex: 0, LineCount: 4},
		{MsgIndex: 1, LineCount: 4},
	}
	m.TranscriptTotalLines = 8

	// run 进行中（有思考块流式）仍可展开历史思考链：流式块被跳过。
	anchor, changed := m.ToggleVisibleReasoningDetail()
	if !changed {
		t.Fatal("ToggleVisibleReasoningDetail() changed = false during streaming, want true")
	}
	if got, want := m.ExpandedReasoningID, m.Messages[0].ID; got != want {
		t.Fatalf("ExpandedReasoningID = %d, want historical reasoning %d", got, want)
	}
	if anchor.MessageID != m.Messages[0].ID {
		t.Fatalf("anchor.MessageID = %d, want %d", anchor.MessageID, m.Messages[0].ID)
	}
}

func TestReasoningStartKeepsExpandedID(t *testing.T) {
	var m Model
	m.AppendMessage(Msg{Role: "reasoning", Content: "history"})
	m.ensureMessageIDs()
	m.ExpandedReasoningID = m.Messages[0].ID

	// 多步 run 中下一个思考块开始时，不应强制折叠用户展开的历史思考链。
	m.HandleReasoningStart(time.Now())
	if got, want := m.ExpandedReasoningID, m.Messages[0].ID; got != want {
		t.Fatalf("ExpandedReasoningID = %d, want preserved %d after reasoning start", got, want)
	}
}

func TestToggleVisibleReasoningDetailSelectsClosestCompletedBlock(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	m.Viewport.SetHeight(10)
	m.AppendMessage(Msg{Role: "reasoning", Content: "older"})
	m.AppendMessage(Msg{Role: "panel", Content: "middle"})
	m.AppendMessage(Msg{Role: "reasoning", Content: "newer"})
	m.TranscriptBlocks = []transcriptBlock{
		{MsgIndex: 0, LineCount: 4},
		{MsgIndex: 1, LineCount: 4},
		{MsgIndex: 2, LineCount: 4},
	}
	m.TranscriptTotalLines = 12
	m.TranscriptYOffset = 2

	anchor, changed := m.ToggleVisibleReasoningDetail()
	if !changed {
		t.Fatal("ToggleVisibleReasoningDetail() changed = false, want true")
	}
	if got, want := m.ExpandedReasoningID, m.Messages[2].ID; got != want {
		t.Fatalf("ExpandedReasoningID = %d, want newer visible reasoning %d", got, want)
	}
	if anchor.MessageID != m.Messages[2].ID {
		t.Fatalf("anchor.MessageID = %d, want %d", anchor.MessageID, m.Messages[2].ID)
	}
}

func TestToggleVisibleReasoningDetailCollapsesVisibleExpandedBlock(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	m.Viewport.SetHeight(8)
	m.AppendMessage(Msg{Role: "reasoning", Content: "complete"})
	m.TranscriptBlocks = []transcriptBlock{{MsgIndex: 0, LineCount: 5}}
	m.TranscriptTotalLines = 5
	m.ExpandedReasoningID = m.Messages[0].ID

	_, changed := m.ToggleVisibleReasoningDetail()
	if !changed {
		t.Fatal("ToggleVisibleReasoningDetail() changed = false, want true")
	}
	if m.ExpandedReasoningID != 0 {
		t.Fatalf("ExpandedReasoningID = %d, want 0 after collapse", m.ExpandedReasoningID)
	}
}

func TestToggleVisibleReasoningDetailSwitchesToClosestBlock(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	m.Viewport.SetHeight(10)
	m.AppendMessage(Msg{Role: "reasoning", Content: "older"})
	m.AppendMessage(Msg{Role: "panel", Content: "middle"})
	m.AppendMessage(Msg{Role: "reasoning", Content: "newer"})
	m.TranscriptBlocks = []transcriptBlock{
		{MsgIndex: 0, LineCount: 4},
		{MsgIndex: 1, LineCount: 4},
		{MsgIndex: 2, LineCount: 4},
	}
	m.TranscriptTotalLines = 12
	m.TranscriptYOffset = 2
	// 已展开 older（视窗内），滚动后按 Ctrl+R 应直接展开视窗内最相关的新块（newer），
	// 同时自动折叠旧的展开块，而不是先折叠再展开。
	m.ExpandedReasoningID = m.Messages[0].ID

	anchor, changed := m.ToggleVisibleReasoningDetail()
	if !changed {
		t.Fatal("ToggleVisibleReasoningDetail() changed = false, want true")
	}
	if got, want := m.ExpandedReasoningID, m.Messages[2].ID; got != want {
		t.Fatalf("ExpandedReasoningID = %d, want newer visible reasoning %d", got, want)
	}
	if anchor.MessageID != m.Messages[2].ID {
		t.Fatalf("anchor.MessageID = %d, want %d", anchor.MessageID, m.Messages[2].ID)
	}
}

func TestToggleVisibleReasoningDetailIgnoresStreamingReasoning(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	m.Viewport.SetHeight(8)
	m.AppendMessage(Msg{Role: "reasoning", Content: "running", Streaming: true})
	m.TranscriptBlocks = []transcriptBlock{{MsgIndex: 0, Streaming: true, LineCount: 5}}
	m.TranscriptTotalLines = 5

	if _, changed := m.ToggleVisibleReasoningDetail(); changed {
		t.Fatal("ToggleVisibleReasoningDetail() changed = true for streaming reasoning")
	}
	if m.ExpandedReasoningID != 0 {
		t.Fatalf("ExpandedReasoningID = %d, want 0", m.ExpandedReasoningID)
	}
}

func TestRestoreTranscriptAnchorKeepsRelativeRow(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	m.Viewport.SetHeight(10)
	m.AppendMessage(Msg{Role: "panel", Content: "before"})
	m.AppendMessage(Msg{Role: "reasoning", Content: "complete"})
	m.TranscriptBlocks = []transcriptBlock{
		{MsgIndex: 0, LineCount: 12},
		{MsgIndex: 1, LineCount: 20},
	}
	m.TranscriptTotalLines = 32

	anchor := TranscriptAnchor{MessageID: m.Messages[1].ID, RelativeRow: 3}
	if !m.RestoreTranscriptAnchor(anchor) {
		t.Fatal("RestoreTranscriptAnchor() = false, want true")
	}
	if got, want := m.TranscriptYOffset, 9; got != want {
		t.Fatalf("TranscriptYOffset = %d, want %d", got, want)
	}
}
