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
		method: protocol.NotifyAgentDelta,
		params: mustJSON(t, protocol.AgentDeltaParams{Kind: protocol.AgentDeltaAssistant, Content: "first answer"}),
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

func TestToolAfterCompletedSubtaskStartsNewBlock(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN)}

	start := func(id, tool string) {
		tui.handleLocalNotification(localNotification{
			method: protocol.NotifyToolStart,
			params: mustJSON(t, protocol.ToolStartParams{ID: id, Tool: tool}),
		})
	}
	end := func(id, tool string) {
		tui.handleLocalNotification(localNotification{
			method: protocol.NotifyToolEnd,
			params: mustJSON(t, protocol.ToolEndParams{ID: id, Tool: tool}),
		})
	}

	start("spawn-1", "spawn")
	start("spawn:spawn-1:child-1", "readfile")
	end("spawn:spawn-1:child-1", "readfile")
	end("spawn-1", "spawn")
	start("tool-2", "listdir")

	blocks := collectToolBlocks(tui.chat.Messages)
	if len(blocks) != 2 {
		t.Fatalf("len(tool blocks) = %d, want 2", len(blocks))
	}
	if got := blocks[0].Order; len(got) != 2 || got[0] != "spawn-1" || got[1] != "spawn:spawn-1:child-1" {
		t.Fatalf("first block order = %#v, want [spawn-1 spawn:spawn-1:child-1]", got)
	}
	if got := blocks[1].Order; len(got) != 1 || got[0] != "tool-2" {
		t.Fatalf("second block order = %#v, want [tool-2]", got)
	}
	if entry := blocks[1].Entries["tool-2"]; entry == nil || entry.ParentID != "" {
		t.Fatalf("tool-2 entry = %#v, want top-level tool", entry)
	}
	if tui.chat.CurrentToolBlock != blocks[1] {
		t.Fatalf("currentToolBlock = %p, want second block %p", tui.chat.CurrentToolBlock, blocks[1])
	}
}

func TestToolBlockClosesAfterSubtaskParentEndsBeforeChild(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN)}
	start := func(id, tool string) {
		tui.handleLocalNotification(localNotification{
			method: protocol.NotifyToolStart,
			params: mustJSON(t, protocol.ToolStartParams{ID: id, Tool: tool}),
		})
	}
	end := func(id, tool string) {
		tui.handleLocalNotification(localNotification{
			method: protocol.NotifyToolEnd,
			params: mustJSON(t, protocol.ToolEndParams{ID: id, Tool: tool}),
		})
	}

	start("spawn-1", "spawn")
	start("spawn:spawn-1:child-1", "readfile")
	end("spawn-1", "spawn")
	if tui.chat.CurrentToolBlock == nil || !tui.chat.CloseToolBlockWhenIdle {
		t.Fatal("subtask block was closed before child completed")
	}
	end("spawn:spawn-1:child-1", "readfile")
	if tui.chat.CurrentToolBlock != nil || tui.chat.CloseToolBlockWhenIdle {
		t.Fatalf("subtask block = %p, pending = %v, want closed/false", tui.chat.CurrentToolBlock, tui.chat.CloseToolBlockWhenIdle)
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

func TestRenderToolEntryShowsGuardDecisionWithoutActionBadge(t *testing.T) {
	for _, locale := range []LocaleID{LocaleEN, LocaleZH} {
		for _, tt := range []struct {
			name     string
			source   string
			decision string
			labelKey string
		}{
			{name: "llm approve", source: "llm", decision: "approve", labelKey: "tui.tool.guard.llm_approved"},
			{name: "llm reject", source: "llm", decision: "reject", labelKey: "tui.tool.guard.llm_blocked"},
			{name: "user approve", source: "user", decision: "approve", labelKey: "tui.tool.guard.user_approved"},
			{name: "user reject", source: "user", decision: "reject", labelKey: "tui.tool.guard.user_rejected"},
			{name: "rule approve", source: "rule", decision: "approve", labelKey: "tui.tool.guard.rule_approved"},
			{name: "rule reject", source: "rule", decision: "reject", labelKey: "tui.tool.guard.rule_blocked"},
			{name: "auto approve", source: "static", decision: "approve", labelKey: "tui.tool.guard.auto_approved"},
			{name: "policy reject", source: "static", decision: "reject", labelKey: "tui.tool.guard.policy_blocked"},
			{name: "review unavailable", source: "fallback", decision: "reject", labelKey: "tui.tool.guard.review_unavailable"},
		} {
			t.Run(string(locale)+"/"+tt.name, func(t *testing.T) {
				tui := &TUI{i18n: newTranslator(locale), width: 100}
				te := &toolEntry{
					RawName: "editfile", Name: "editfile", Intent: "Update config", Status: toolRunning,
					Guard: &guardInfo{Decision: tt.decision, Source: tt.source, Reason: "review reason"},
				}
				plain := stripANSIForTest(tui.renderToolEntry(te, false))
				for _, want := range []string{tui.tr("tui.tool.guard.badge"), tui.tr(tt.labelKey), "review reason"} {
					if !strings.Contains(plain, want) {
						t.Fatalf("renderToolEntry() = %q, want guard summary substring %q", plain, want)
					}
				}
				if badge := tui.tr("tui.tool.guard.write_badge"); strings.Contains(plain, badge) {
					t.Fatalf("renderToolEntry() = %q, want no %q badge", plain, badge)
				}
				if badge := strings.TrimSpace(stripANSIForTest(tui.toolDetailDeps().ReadOnlyBadge(false))); badge != tui.tr("tui.tool.guard.write_badge") {
					t.Fatalf("detail badge = %q, want action badge retained", badge)
				}
			})
		}
	}
}

func TestRenderToolEntryKeepsReadOnlyGuardPresentation(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 100}
	te := &toolEntry{RawName: "readfile", Name: "readfile", Status: toolRunning,
		Guard: &guardInfo{ReadOnly: true, Decision: "approve", Source: "static"},
	}
	if plain := stripANSIForTest(tui.renderToolEntry(te, false)); strings.Contains(plain, "Guard") {
		t.Fatalf("renderToolEntry() = %q, want read-only approval without Guard summary", plain)
	}
	te.Guard.Decision, te.Guard.Source = "reject", "rule"
	plain := stripANSIForTest(tui.renderToolEntry(te, false))
	for _, want := range []string{"rule blocked", "read-only"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("renderToolEntry() = %q, want %q", plain, want)
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
		ParamsRaw: map[string]any{"mode": "auto", "pattern": "Guard", "path": "internal"},
		Status:    toolDone,
		Metadata:  map[string]any{"kind": "search_result", "matches": 18, "files_matched": 6, "files_scanned": 214, "path_matches": 2, "symbol_matches": 4, "content_matches": 12},
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
	for _, want := range []string{"Search auto \"Guard\" in internal", "18 matches in 6 files", "2 path / 4 symbol / 12 content", "HTTP GET https://example.com/status", "HTTP GET  200"} {
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
		ParamsRaw: map[string]any{"model": "anthropic/claude-sonnet"},
		Status:    toolRunning,
	}

	rendered := tui.renderToolEntry(te, false)
	plain := stripANSIForTest(rendered)
	if !strings.Contains(plain, "Subtask [anthropic/claude-sonnet] · Analyze code structure") {
		t.Fatalf("renderToolEntry() = %q, want subtask label with model", plain)
	}
}

func TestRenderToolBlockHidesSubtaskTree(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100}
	block := &toolBlock{Entries: map[string]*toolEntry{}}
	parent := &toolEntry{ID: "spawn-1", RawName: "spawn", Name: "Spawn", Intent: "分析代码", ParamsRaw: map[string]any{"model": "anthropic/claude-sonnet"}, Status: toolRunning}
	child := &toolEntry{ID: "spawn:spawn-1:tool-1", ParentID: "spawn-1", RawName: "search", Name: "Search", Intent: "搜索代码", Status: toolRunning}
	main := &toolEntry{ID: "read-1", RawName: "readfile", Name: "Readfile", Intent: "读取文件", Status: toolDone}
	block.Add(parent)
	block.Add(child)
	block.Add(main)

	plain := stripANSIForTest(tui.renderToolBlock(block))
	if strings.Contains(plain, "分析代码") || strings.Contains(plain, "搜索代码") || strings.Contains(plain, "等待子任务继续") {
		t.Fatalf("renderToolBlock() = %q, should hide spawn tree", plain)
	}
	if !strings.Contains(plain, "读取文件") {
		t.Fatalf("renderToolBlock() = %q, want ordinary tool", plain)
	}
}

func stripANSIForTest(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != 0x1b {
			b.WriteByte(c)
			continue
		}
		if i+1 >= len(s) {
			break
		}
		i++
		switch s[i] {
		case '[':
			for i+1 < len(s) {
				i++
				if s[i] >= '@' && s[i] <= '~' {
					break
				}
			}
		case ']':
			for i+1 < len(s) {
				i++
				if s[i] == 0x07 {
					break
				}
				if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
					i++
					break
				}
			}
		}
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
