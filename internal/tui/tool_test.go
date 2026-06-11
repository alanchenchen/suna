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

	blocks := collectToolBlocks(tui.chat.Messages)
	if len(blocks) != 2 {
		t.Fatalf("len(tool blocks) = %d, want 2", len(blocks))
	}
	if tui.chat.CurrentToolBlock != blocks[1] {
		t.Fatalf("currentToolBlock = %p, want second block %p", tui.chat.CurrentToolBlock, blocks[1])
	}
	if got := blocks[0].Order; len(got) != 1 || got[0] != "tool-1" {
		t.Fatalf("first block order = %#v, want [tool-1]", got)
	}
	if got := blocks[1].Order; len(got) != 1 || got[0] != "tool-2" {
		t.Fatalf("second block order = %#v, want [tool-2]", got)
	}
	if len(tui.chat.Messages) != 3 {
		t.Fatalf("len(messages) = %d, want 3", len(tui.chat.Messages))
	}
	if tui.chat.Messages[1].Role != "assistant" {
		t.Fatalf("messages[1].Role = %q, want assistant", tui.chat.Messages[1].Role)
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

	blocks := collectToolBlocks(tui.chat.Messages)
	if len(blocks) != 1 {
		t.Fatalf("len(tool blocks) = %d, want 1", len(blocks))
	}
	if got := blocks[0].Order; len(got) != 2 || got[0] != "tool-1" || got[1] != "tool-2" {
		t.Fatalf("block order = %#v, want [tool-1 tool-2]", got)
	}
}

func TestSystemMessageClosesCurrentToolBlock(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN)}

	tui.handleLocalNotification(localNotification{
		method: protocol.NotifyToolStart,
		params: mustJSON(t, protocol.ToolStartParams{ID: "tool-1", Tool: "readfile"}),
	})
	tui.appendNonToolMessage(chatMsg{Role: "system", Content: "note"})
	tui.handleLocalNotification(localNotification{
		method: protocol.NotifyToolStart,
		params: mustJSON(t, protocol.ToolStartParams{ID: "tool-2", Tool: "readfile"}),
	})

	blocks := collectToolBlocks(tui.chat.Messages)
	if len(blocks) != 2 {
		t.Fatalf("len(tool blocks) = %d, want 2", len(blocks))
	}
}

func TestToolBlockTitleCountsUniqueChangedFiles(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN)}
	block := &toolBlock{Entries: make(map[string]*toolEntry)}
	block.Add(&toolEntry{ID: "edit-1", RawName: "editfile", Metadata: map[string]any{"kind": "file_change", "path": "internal/a.go"}})
	block.Add(&toolEntry{ID: "edit-2", RawName: "editfile", Metadata: map[string]any{"kind": "file_change", "path": "internal/a.go"}})
	block.Add(&toolEntry{ID: "edit-3", RawName: "editfile", Metadata: map[string]any{"kind": "file_change", "path": "internal/b.go"}})

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
		RawName: "editfile",
		Name:    "editfile",
		Intent:  "Update config",
		Status:  toolDone,
		Metadata: map[string]any{
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
			t.Fatalf("renderToolEntry() = %q, want file summary substring %q", plain, want)
		}
	}
}

func TestRenderToolEntryShowsGuardSummary(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 100}
	te := &toolEntry{
		RawName: "editfile",
		Name:    "editfile",
		Intent:  "Update config",
		Status:  toolRunning,
		Guard: &guardInfo{
			Risk:     "medium",
			Decision: "approve",
			Source:   "llm",
			Reason:   "matches requested edit",
		},
	}

	rendered := tui.renderToolEntry(te, false)
	plain := stripANSIForTest(rendered)
	for _, want := range []string{"Guard", "LLM approved", "medium", "matches requested edit"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("renderToolEntry() = %q, want guard summary substring %q", plain, want)
		}
	}
}

func TestRenderToolEntryShowsFSChangeSummary(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 120}
	te := &toolEntry{
		RawName:   "filesystem",
		Name:      "FS",
		Intent:    "Clean generated build output",
		ParamsRaw: map[string]any{"action": "remove", "path": "dist", "recursive": true, "expected_kind": "dir"},
		Status:    toolDone,
		Metadata: map[string]any{
			"kind":       "fs_change",
			"action":     "remove",
			"path":       "dist",
			"entry_kind": "dir",
			"recursive":  true,
			"entries":    248,
			"size":       12400,
		},
	}

	rendered := tui.renderToolEntry(te, false)
	plain := stripANSIForTest(rendered)
	for _, want := range []string{"FS remove dist", "Clean generated build output", "FS", "PERMANENTLY DELETED", "recursive", "248 entries"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("renderToolEntry() = %q, want fs summary substring %q", plain, want)
		}
	}
	if strings.Count(plain, "dist") != 1 {
		t.Fatalf("renderToolEntry() = %q, want fs path shown only in main line", plain)
	}
}

func TestRenderToolEntryShowsSearchAndHTTPSummaries(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 120}
	search := &toolEntry{
		RawName:   "search",
		Name:      "Search",
		Intent:    "Find guard rendering",
		ParamsRaw: map[string]any{"mode": "content", "query": "Guard", "path": "internal"},
		Status:    toolDone,
		Metadata:  map[string]any{"kind": "search_result", "matches": 18, "files_matched": 6, "files_scanned": 214},
	}
	httpEntry := &toolEntry{
		RawName:   "http",
		Name:      "HTTP",
		Intent:    "Check service health",
		ParamsRaw: map[string]any{"method": "GET", "url": "https://example.com/status"},
		Status:    toolDone,
		Metadata:  map[string]any{"kind": "http_response", "method": "GET", "status": 200, "body_bytes": 1200},
	}

	plain := stripANSIForTest(tui.renderToolEntry(search, false) + tui.renderToolEntry(httpEntry, false))
	for _, want := range []string{"Search content \"Guard\" in internal", "18 matches in 6 files", "HTTP GET https://example.com/status", "HTTP GET  200"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("rendered tools = %q, want substring %q", plain, want)
		}
	}
}

func TestRenderSubtaskEntryShowsModelInLabel(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 100}
	te := &toolEntry{
		RawName:   "spawn",
		Name:      "Spawn",
		Intent:    "Analyze code structure",
		ParamsRaw: map[string]any{"model": "Oio/gpt-5.5"},
		Status:    toolRunning,
	}

	rendered := tui.renderToolEntry(te, false)
	plain := stripANSIForTest(rendered)
	if !strings.Contains(plain, "Subtask [Oio/gpt-5.5] · Analyze code structure") {
		t.Fatalf("renderToolEntry() = %q, want subtask label with model", plain)
	}
}

func TestRenderRunningSubtaskShowsWaitingWhenNoChildRunning(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100}
	block := &toolBlock{Entries: map[string]*toolEntry{}}
	parent := &toolEntry{ID: "spawn-1", RawName: "spawn", Name: "Spawn", Intent: "分析代码", ParamsRaw: map[string]any{"model": "Oio/gpt-5.5"}, Status: toolRunning}
	child := &toolEntry{ID: "spawn:spawn-1:tool-1", ParentID: "spawn-1", RawName: "search", Name: "Search", Intent: "搜索代码", Status: toolDone}
	block.Add(parent)
	block.Add(child)

	plain := stripANSIForTest(tui.renderToolBlock(block))
	if !strings.Contains(plain, "等待子任务继续") {
		t.Fatalf("renderToolBlock() = %q, want subtask waiting line", plain)
	}
}

func TestRenderRunningSubtaskHidesWaitingWhenChildRunning(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100}
	block := &toolBlock{Entries: map[string]*toolEntry{}}
	parent := &toolEntry{ID: "spawn-1", RawName: "spawn", Name: "Spawn", Intent: "分析代码", Status: toolRunning}
	child := &toolEntry{ID: "spawn:spawn-1:tool-1", ParentID: "spawn-1", RawName: "search", Name: "Search", Intent: "搜索代码", Status: toolRunning}
	block.Add(parent)
	block.Add(child)

	plain := stripANSIForTest(tui.renderToolBlock(block))
	if strings.Contains(plain, "等待子任务继续") {
		t.Fatalf("renderToolBlock() = %q, should hide subtask waiting line while child is running", plain)
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
		t.Fatalf("Marshal() error = %v", err)
	}
	return b
}

func collectToolBlocks(messages []chatMsg) []*toolBlock {
	var blocks []*toolBlock
	for _, msg := range messages {
		if msg.Role != "tool" {
			continue
		}
		block, ok := msg.Content.(*toolBlock)
		if ok && block != nil {
			blocks = append(blocks, block)
		}
	}
	return blocks
}
