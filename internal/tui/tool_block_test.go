package tui

import (
	"encoding/json"
	"strings"
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

func TestToolBlockTitleCountsUniqueChangedFiles(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN)}
	block := &toolBlock{entries: make(map[string]*toolEntry)}
	block.add(&toolEntry{id: "edit-1", rawName: "editfile", metadata: map[string]any{"kind": "file_change", "path": "internal/a.go"}})
	block.add(&toolEntry{id: "edit-2", rawName: "editfile", metadata: map[string]any{"kind": "file_change", "path": "internal/a.go"}})
	block.add(&toolEntry{id: "edit-3", rawName: "editfile", metadata: map[string]any{"kind": "file_change", "path": "internal/b.go"}})

	got := tui.toolBlockTitle(tui.visibleToolEntries(block))
	if !strings.Contains(got, "2 files changed") {
		t.Fatalf("toolBlockTitle() = %q, want unique changed file count", got)
	}
	if strings.Contains(got, "3 files changed") {
		t.Fatalf("toolBlockTitle() = %q, counted file change operations instead of files", got)
	}
}

func TestRenderToolEntryShowsFileChangeSummary(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 100}
	te := &toolEntry{
		rawName: "editfile",
		name:    "editfile",
		intent:  "Update config",
		status:  toolDone,
		metadata: map[string]any{
			"kind":          "file_change",
			"path":          "internal/tool/writefile.go",
			"operation":     "updated",
			"added_lines":   18,
			"removed_lines": 4,
			"replacements":  1,
			"size_before":   2140,
			"size_after":    2601,
		},
	}

	rendered := tui.renderToolEntry(te, false)
	plain := stripANSIForTest(rendered)
	for _, want := range []string{"↳", "File", "internal/tool/writefile.go", "UPDATED", "+18", "-4", "1 repl", "2.1KB", "2.5KB"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("rendered summary missing %q:\n%s", want, plain)
		}
	}
}

func TestRenderToolEntryShowsGuardSummary(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 100}
	te := &toolEntry{
		rawName: "editfile",
		name:    "editfile",
		intent:  "Update config",
		status:  toolRunning,
		guard: &guardInfo{
			risk:     "medium",
			decision: "approve",
			source:   "llm",
			reason:   "matches requested edit",
		},
	}

	rendered := tui.renderToolEntry(te, false)
	plain := stripANSIForTest(rendered)
	for _, want := range []string{"Guard", "LLM approved", "medium", "matches requested edit"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("rendered guard summary missing %q:\n%s", want, plain)
		}
	}
}

func TestCompactPathKeepsFullPathWhenItFits(t *testing.T) {
	path := "Users/alanchen/Documents/suna/internal/runner/types.go"
	if got := compactPath(path, 80); got != path {
		t.Fatalf("compactPath() = %q, want full path", got)
	}
}

func TestCompactPathKeepsFilenameSuffixWhenTight(t *testing.T) {
	got := compactPath("very/long/path/internal/runner/types.go", 12)
	if !strings.HasSuffix(got, "types.go") {
		t.Fatalf("compactPath() = %q, want filename suffix", got)
	}
	if got == "types.go" || !strings.HasPrefix(got, "…") {
		t.Fatalf("compactPath() = %q, want ellipsized suffix", got)
	}
}

func stripANSIForTest(s string) string {
	var b strings.Builder
	inEsc := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inEsc {
			if c >= '@' && c <= '~' {
				inEsc = false
			}
			continue
		}
		if c == 0x1b {
			inEsc = true
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
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
