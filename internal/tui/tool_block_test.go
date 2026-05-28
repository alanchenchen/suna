package tui

import (
	"encoding/json"
	"testing"

	"github.com/alanchenchen/suna/internal/protocol"
)

func TestInterleavedToolsStartNewBlockAfterAssistantText(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN)}

	tui.handleLocalNotification(localNotification{
		method: protocol.NotifyToolStart,
		params: mustJSON(t, protocol.ToolStartParams{ID: "tool-1", Tool: "readfile"}),
	})
	tui.handleLocalNotification(localNotification{
		method: protocol.NotifyToolEnd,
		params: mustJSON(t, protocol.ToolEndParams{ID: "tool-1", Tool: "readfile"}),
	})
	tui.handleLocalNotification(localNotification{
		method: protocol.NotifyStream,
		params: mustJSON(t, protocol.StreamParams{Chunk: "first answer"}),
	})
	tui.handleLocalNotification(localNotification{
		method: protocol.NotifyToolStart,
		params: mustJSON(t, protocol.ToolStartParams{ID: "tool-2", Tool: "readfile"}),
	})

	blocks := collectToolBlocks(tui.messages)
	if len(blocks) != 2 {
		t.Fatalf("len(tool blocks) = %d, want 2", len(blocks))
	}
	if tui.currentToolBlock != blocks[1] {
		t.Fatalf("currentToolBlock = %p, want second block %p", tui.currentToolBlock, blocks[1])
	}
	if got := blocks[0].order; len(got) != 1 || got[0] != "tool-1" {
		t.Fatalf("first block order = %#v, want [tool-1]", got)
	}
	if got := blocks[1].order; len(got) != 1 || got[0] != "tool-2" {
		t.Fatalf("second block order = %#v, want [tool-2]", got)
	}
	if len(tui.messages) != 3 {
		t.Fatalf("len(messages) = %d, want 3", len(tui.messages))
	}
	if tui.messages[1].role != "assistant" {
		t.Fatalf("messages[1].role = %q, want assistant", tui.messages[1].role)
	}
}

func TestConsecutiveToolsReuseSameBlock(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN)}

	tui.handleLocalNotification(localNotification{
		method: protocol.NotifyToolStart,
		params: mustJSON(t, protocol.ToolStartParams{ID: "tool-1", Tool: "readfile"}),
	})
	tui.handleLocalNotification(localNotification{
		method: protocol.NotifyToolStart,
		params: mustJSON(t, protocol.ToolStartParams{ID: "tool-2", Tool: "listdir"}),
	})

	blocks := collectToolBlocks(tui.messages)
	if len(blocks) != 1 {
		t.Fatalf("len(tool blocks) = %d, want 1", len(blocks))
	}
	if got := blocks[0].order; len(got) != 2 || got[0] != "tool-1" || got[1] != "tool-2" {
		t.Fatalf("block order = %#v, want [tool-1 tool-2]", got)
	}
}

func TestSystemMessageClosesCurrentToolBlock(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN)}

	tui.handleLocalNotification(localNotification{
		method: protocol.NotifyToolStart,
		params: mustJSON(t, protocol.ToolStartParams{ID: "tool-1", Tool: "readfile"}),
	})
	tui.appendNonToolMessage(chatMsg{role: "system", content: "note"})
	tui.handleLocalNotification(localNotification{
		method: protocol.NotifyToolStart,
		params: mustJSON(t, protocol.ToolStartParams{ID: "tool-2", Tool: "readfile"}),
	})

	blocks := collectToolBlocks(tui.messages)
	if len(blocks) != 2 {
		t.Fatalf("len(tool blocks) = %d, want 2", len(blocks))
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return b
}

func collectToolBlocks(messages []chatMsg) []*toolBlock {
	var blocks []*toolBlock
	for _, msg := range messages {
		if msg.role != "tool" {
			continue
		}
		block, ok := msg.content.(*toolBlock)
		if ok && block != nil {
			blocks = append(blocks, block)
		}
	}
	return blocks
}
