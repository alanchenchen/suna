package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
)

// 子任务面板的提示必须随状态变化，且提示不能被边框截断：
// 折叠态提示“Ctrl+T 详情”（结果刚到达、面板已折叠时最需要），
// 展开态提示“Ctrl+T 收起”（纯 subtask 块没有 tool 盒子，提示只能由面板提供）。
func TestSubtaskPanelHintsFollowStateAndSurviveTruncation(t *testing.T) {
	newTUI := func() (*TUI, *toolBlock) {
		tui := &TUI{i18n: newTranslator(LocaleZH), width: 100, height: 30, mode: uipage.Chat}
		tui.initChatComponents()
		block := tui.ensureToolBlock()
		block.Add(&toolEntry{ID: "spawn-1", Name: "Spawn", RawName: "spawn", Intent: "分析图片", Status: toolDone,
			Result: `{"status":"completed","result":"用量 26.8%","side_effects":{"status":"none"}}`})
		return tui, block
	}

	// 运行中：面板已自动展开，不应出现任何 Ctrl+T 提示。
	tui, block := newTUI()
	if got := stripANSIForTest(tui.renderSubtaskBlock(block)); strings.Contains(got, "Ctrl+T") {
		t.Fatalf("running panel = %q, want no Ctrl+T hint", got)
	}

	// run 结束后折叠：必须有“详情”提示，且不被边框截断。
	tui, block = newTUI()
	tui.chat.CurrentToolBlock = nil
	tui.syncContent()
	got := stripANSIForTest(tui.renderSubtaskBlock(block))
	if !strings.Contains(got, tui.tr("tui.tool.detail_hint")) {
		t.Fatalf("collapsed panel = %q, want detail hint intact", got)
	}

	// Ctrl+T 展开：必须有“收起”提示且结果可见。
	tui, block = newTUI()
	tui.chat.CurrentToolBlock = nil
	tui.syncContent()
	tui.toggleBlockDetail()
	got = stripANSIForTest(tui.renderSubtaskBlock(block))
	if !strings.Contains(got, tui.tr("tui.tool.detail_expanded")) {
		t.Fatalf("expanded panel = %q, want collapse hint", got)
	}
	if !strings.Contains(got, "26.8%") {
		t.Fatalf("expanded panel = %q, want subtask result", got)
	}
}

// 子任务结果在根调用结束时到达（此时 CurrentToolBlock 已置空），
// 面板必须能通过 Ctrl+T 展开激活，否则结果永远没有展示机会。
func TestSubtaskPanelActivatesAfterRunEndViaExpand(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100, height: 30, mode: uipage.Chat}
	tui.initChatComponents()
	block := tui.ensureToolBlock()
	block.Add(&toolEntry{ID: "spawn-1", Name: "Spawn", RawName: "spawn", Intent: "分析图片", Status: toolDone,
		Result: `{"status":"completed","result":"图片显示用量 26.8%","side_effects":{"status":"none"}}`})
	tui.syncContent()
	// 子任务根调用结束后 CurrentToolBlock 置空，这正是结果到达的时刻。
	tui.chat.CurrentToolBlock = nil

	if tui.hasActiveSubtaskPanel() {
		t.Fatalf("hasActiveSubtaskPanel() = true before expand, want inactive")
	}
	tui.toggleBlockDetail()
	if !tui.hasActiveSubtaskPanel() {
		t.Fatalf("hasActiveSubtaskPanel() = false after Ctrl+T, want active")
	}
	if ids := tui.visibleSubtaskIDs(); len(ids) != 1 {
		t.Fatalf("visibleSubtaskIDs() = %v, want the expanded block entries", ids)
	}
}

// 历史块不应自动激活面板：否则键位被接管却没有任何可见反馈（隐形操作）。
func TestHistoricalSubtaskBlockDoesNotHijackKeys(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100, height: 30, mode: uipage.Chat}
	tui.initChatComponents()
	block := tui.ensureToolBlock()
	block.Add(&toolEntry{ID: "spawn-1", Name: "Spawn", RawName: "spawn", Intent: "分析图片", Status: toolDone})
	tui.chat.CurrentToolBlock = nil

	if tui.hasActiveSubtaskPanel() {
		t.Fatalf("hasActiveSubtaskPanel() = true for historical block, want false")
	}
	if tui.canUseSubtaskPanelKeys() {
		t.Fatalf("canUseSubtaskPanelKeys() = true for historical block, want false")
	}
}

func TestSubtaskResultSectionRendersParsedText(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100, height: 40, mode: uipage.Chat}
	tui.initChatComponents()
	block := tui.ensureToolBlock()
	te := &toolEntry{ID: "spawn-1", Name: "Spawn", RawName: "spawn", Intent: "分析图片", Status: toolDone,
		Result: `{"status":"completed","result":"图片显示用量 26.8%","side_effects":{"status":"remaining","summary":"修改了 runner.go"}}`}
	block.Add(te)

	lines := stripANSIForTest(strings.Join(tui.renderSelectedSubtaskResult(te, 60), "\n"))
	if !strings.Contains(lines, "结果") {
		t.Fatalf("renderSelectedSubtaskResult() = %q, want result label", lines)
	}
	if !strings.Contains(lines, "图片显示用量 26.8%") {
		t.Fatalf("renderSelectedSubtaskResult() = %q, want parsed result text", lines)
	}
	if strings.Contains(lines, `"status"`) {
		t.Fatalf("renderSelectedSubtaskResult() = %q, should not leak raw JSON", lines)
	}
	if !strings.Contains(lines, "副作用") || !strings.Contains(lines, "修改了 runner.go") {
		t.Fatalf("renderSelectedSubtaskResult() = %q, want side effects disclosure", lines)
	}
}

func TestSubtaskResultSectionHiddenWhileRunning(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100, height: 40, mode: uipage.Chat}
	tui.initChatComponents()
	te := &toolEntry{ID: "spawn-1", Name: "Spawn", RawName: "spawn", Status: toolRunning,
		Result: `{"status":"completed","result":"partial"}`}
	if lines := tui.renderSelectedSubtaskResult(te, 60); len(lines) != 0 {
		t.Fatalf("renderSelectedSubtaskResult() = %v, want empty while running", lines)
	}
}

// 长结果不再硬截断丢失内容：小节只展示窗口行数，并用进度提示引导 PgUp/PgDn 滚动。
func TestSubtaskResultSectionScrollsLongText(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100, height: 20, mode: uipage.Chat}
	tui.initChatComponents()
	long := strings.Repeat("line\n", 40)
	te := &toolEntry{ID: "spawn-1", Name: "Spawn", RawName: "spawn", Status: toolDone,
		Result: `{"status":"completed","result":"` + strings.ReplaceAll(long, "\n", "\\n") + `"}`}

	lines := stripANSIForTest(strings.Join(tui.renderSelectedSubtaskResult(te, 60), "\n"))
	if !strings.Contains(lines, "PgUp/PgDn") {
		t.Fatalf("renderSelectedSubtaskResult() = %q, want scroll hint with PgUp/PgDn", lines)
	}
	// 可视行数受 subtaskResultMaxRows 限制，其余内容靠滚动查看。
	if got := strings.Count(lines, "line"); got != tui.subtaskResultMaxRows() {
		t.Fatalf("visible result lines = %d, want %d", got, tui.subtaskResultMaxRows())
	}
}

// 滚动后能看到长结果的后续内容，证明结果全文可达（不再丢失）。
func TestSubtaskResultSectionScrollRevealsRemainingLines(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100, height: 20, mode: uipage.Chat}
	tui.initChatComponents()
	var sb strings.Builder
	for i := 0; i < 40; i++ {
		sb.WriteString(fmt.Sprintf("row-%02d\n", i))
	}
	te := &toolEntry{ID: "spawn-1", Name: "Spawn", RawName: "spawn", Status: toolDone,
		Result: `{"status":"completed","result":"` + strings.ReplaceAll(sb.String(), "\n", "\\n") + `"}`}
	block := tui.ensureToolBlock()
	block.Add(te)
	tui.chat.ExpandedBlock = block
	tui.chat.ExpandedBoxKind = "tool"

	first := stripANSIForTest(strings.Join(tui.renderSelectedSubtaskResult(te, 60), "\n"))
	if !strings.Contains(first, "row-00") {
		t.Fatalf("first window = %q, want row-00", first)
	}

	tui.scrollSubtaskResult(tui.subtaskResultMaxRows())
	second := stripANSIForTest(strings.Join(tui.renderSelectedSubtaskResult(te, 60), "\n"))
	if strings.Contains(second, "row-00") {
		t.Fatalf("after scroll = %q, want later rows", second)
	}
}

// 展开块内的 ↑↓ 光标只作用于展开块，且越界时保持边界（不回绕）。
func TestExpandedBlockCursorMovesWithinBounds(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100, height: 30, mode: uipage.Chat}
	tui.initChatComponents()
	block := tui.ensureToolBlock()
	block.Add(&toolEntry{ID: "a", Name: "A", RawName: "readfile", Status: toolDone})
	block.Add(&toolEntry{ID: "b", Name: "B", RawName: "readfile", Status: toolDone})
	tui.chat.ExpandedBlock = block
	tui.chat.ExpandedBoxKind = "tool"

	tui.moveExpandedBlockCursor(1)
	if got := tui.chat.ExpandedBlockCursor; got != 1 {
		t.Fatalf("cursor = %d, want 1", got)
	}
	tui.moveExpandedBlockCursor(1)
	if got := tui.chat.ExpandedBlockCursor; got != 1 {
		t.Fatalf("cursor = %d, want clamped at last entry", got)
	}
	tui.moveExpandedBlockCursor(-1)
	if got := tui.chat.ExpandedBlockCursor; got != 0 {
		t.Fatalf("cursor = %d, want 0", got)
	}
}

// 输入框有草稿时 ↑↓ 不能被 subtask 面板抢走，否则多行草稿无法移动光标。
func TestSubtaskPanelKeysYieldToDraft(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100, height: 30, mode: uipage.Chat}
	tui.initChatComponents()
	block := tui.ensureToolBlock()
	block.Add(&toolEntry{ID: "spawn-1", Name: "Spawn", RawName: "spawn", Intent: "分析", Status: toolRunning, StartedAt: time.Now()})
	tui.chat.Textarea.SetValue("draft")

	if tui.canUseSubtaskPanelKeys() {
		t.Fatalf("canUseSubtaskPanelKeys() = true with non-empty draft, want false")
	}
	tui.chat.Textarea.SetValue("")
	if !tui.canUseSubtaskPanelKeys() {
		t.Fatalf("canUseSubtaskPanelKeys() = false with empty draft, want true")
	}
}

// 展开块内 ↑↓ 在有草稿时让位输入框，避免多行编辑无法移动文本光标。
func TestExpandedBlockKeysYieldToDraft(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100, height: 30, mode: uipage.Chat}
	tui.initChatComponents()
	block := tui.ensureToolBlock()
	block.Add(&toolEntry{ID: "a", Name: "A", RawName: "readfile", Status: toolDone})
	block.Add(&toolEntry{ID: "b", Name: "B", RawName: "readfile", Status: toolDone})
	tui.chat.ExpandedBlock = block
	tui.chat.ExpandedBoxKind = "tool"

	// 有草稿：↑ 不应移动块内光标。
	tui.chat.Textarea.SetValue("draft")
	tui.chat.Textarea.CursorStart()
	_, _ = tui.updateChatKey("up", tea.KeyPressMsg{Code: tea.KeyUp})
	if got := tui.chat.ExpandedBlockCursor; got != 0 {
		t.Fatalf("cursor = %d with non-empty draft, want 0 (input keeps arrow keys)", got)
	}

	// 无草稿：↑↓ 接管块内光标。
	tui.chat.Textarea.SetValue("")
	_, _ = tui.updateChatKey("down", tea.KeyPressMsg{Code: tea.KeyDown})
	if got := tui.chat.ExpandedBlockCursor; got != 1 {
		t.Fatalf("cursor = %d with empty draft, want 1", got)
	}
}

// Esc 在展开态先收起，不能落到"取消 run"分支。
func TestEscCollapsesExpandedBlockBeforeCancelling(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100, height: 30, mode: uipage.Chat}
	tui.initChatComponents()
	block := tui.ensureToolBlock()
	block.Add(&toolEntry{ID: "a", Name: "A", RawName: "readfile", Status: toolDone})
	tui.chat.ExpandedBlock = block
	tui.chat.ExpandedBoxKind = "tool"
	tui.chat.Loading = true

	if _, _ = tui.updateChatEsc(); tui.chat.ExpandedBlock != nil {
		t.Fatalf("ExpandedBlock = %v after Esc, want nil", tui.chat.ExpandedBlock)
	}
	if tui.cancelling {
		t.Fatalf("cancelling = true after Esc, want run untouched")
	}
}

// 裁剪释放历史时必须清空指向被裁块的展开态，避免悬挂指针与不可见展开。
func TestTrimDisplayHistoryClearsExpandedBlock(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100, height: 30, mode: uipage.Chat}
	tui.initChatComponents()
	block := tui.ensureToolBlock()
	block.Add(&toolEntry{ID: "a", Name: "A", RawName: "readfile", Status: toolDone})
	tui.chat.ExpandedBlock = block
	tui.chat.ExpandedBoxKind = "tool"
	// 追加一条 user 消息，使裁剪可以按 turn 释放前面的工具块。
	tui.appendNonToolMessage(chatMsg{Role: "user", Content: "hello"})

	if !tui.chat.TrimDisplayHistory(1) {
		t.Fatalf("TrimDisplayHistory() = false, want trimmed")
	}
	if tui.chat.ExpandedBlock != nil {
		t.Fatalf("ExpandedBlock = %v after trim, want nil", tui.chat.ExpandedBlock)
	}
}

// TestExpandedBlockWheelYieldsToSubtaskToolDetail 保证滚轮优先级与 PgUp/PgDn 一致：
// subtask 工具详情展开时，滚轮先滚它，而不是展开块的条目详情。
func TestExpandedBlockWheelYieldsToSubtaskToolDetail(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100, height: 30, mode: uipage.Chat}
	tui.initChatComponents()
	block := tui.ensureToolBlock()
	block.Add(&toolEntry{ID: "t1", Name: "Readfile", RawName: "readfile", Intent: "读文件", Status: toolDone})
	tui.chat.ExpandedBlock = block
	tui.chat.ExpandedBoxKind = "tool"
	tui.chat.SubtaskToolDetailExpanded = true
	tui.chat.SubtaskToolDetailScroll = 5

	// 无 active subtask 面板时，SubtaskToolDetailExpanded 不应吞掉滚轮。
	if tui.hasActiveSubtaskPanel() {
		t.Fatal("precondition: no active subtask panel expected")
	}
	// 展开块含主条目 → 滚轮应作用于展开块详情。
	if !tui.expandedBlockHasMainEntries() {
		t.Fatal("expected main entries to accept wheel")
	}
}
