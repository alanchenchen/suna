package tui

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/alanchenchen/suna/internal/protocol"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
)

func TestStartToolTreatsInvalidSpawnPrefixAsMainTool(t *testing.T) {
	m := &chatpage.Model{}
	te := m.StartTool(protocol.ToolStartParams{ID: "spawn:missing:read-1", Tool: "readfile", Intent: "读取文件"}, "spawn:missing:read-1", time.Now())
	if te.ParentID != "" {
		t.Fatalf("ParentID = %q, want empty for missing spawn parent", te.ParentID)
	}
	ids := m.VisibleToolIDs()
	if len(ids) != 1 || ids[0] != "spawn:missing:read-1" {
		t.Fatalf("VisibleToolIDs() = %v, want invalid spawn-prefixed tool as main tool", ids)
	}
}

func TestSubtaskPanelKeyboardAndToolDetail(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100, height: 30, mode: uipage.Chat}
	tui.initChatComponents()
	block := tui.ensureToolBlock()
	block.Add(&toolEntry{ID: "spawn-1", Name: "Spawn", RawName: "spawn", Intent: "调研提示词", Status: toolRunning, ParamsRaw: map[string]any{"model": "DF/glm-5.2", "tools": []string{"http"}, "task": "调研主流 agent 提示词"}, StartedAt: time.Now().Add(-time.Second)})
	block.Add(&toolEntry{ID: "spawn:spawn-1:http-1", ParentID: "spawn-1", Name: "HTTP", RawName: "http", Intent: "获取资料", ParamsRaw: map[string]any{"url": "https://kilo.ai/"}, Params: `{"url":"https://kilo.ai/"}`, Result: strings.Repeat("result\n", 20), Status: toolRunning, StartedAt: time.Now().Add(-time.Second)})
	block.Add(&toolEntry{ID: "spawn-2", Name: "Spawn", RawName: "spawn", Intent: "结果复核", Status: toolRunning})

	input := stripANSIForTest(tui.renderInputArea())
	if strings.Contains(input, "Tab 聚焦子任务") {
		t.Fatalf("renderInputArea() = %q, should not show subtask focus hint", input)
	}
	panel := stripANSIForTest(tui.renderSubtaskBlock(block))
	for _, want := range []string{"调研提示词", "当前子任务", "工具调用", "获取资料", "Enter 展开工具详情"} {
		if !strings.Contains(panel, want) {
			t.Fatalf("renderSubtaskPanel() = %q, want %q", panel, want)
		}
	}
	if strings.Contains(panel, "─ 工具详情") {
		t.Fatalf("renderSubtaskPanel() = %q, should keep tool detail collapsed by default", panel)
	}

	_, _ = tui.updateChatKey("tab", tea.KeyPressMsg{})
	if got, want := tui.chat.SubtaskCursor, 1; got != want {
		t.Fatalf("SubtaskCursor = %d after Tab, want %d", got, want)
	}
	_, _ = tui.updateChatKey("tab", tea.KeyPressMsg{})
	if got, want := tui.chat.SubtaskCursor, 0; got != want {
		t.Fatalf("SubtaskCursor = %d after second Tab, want %d", got, want)
	}

	_, _ = tui.updateChatKey("enter", tea.KeyPressMsg{})
	if !tui.chat.SubtaskToolDetailExpanded {
		t.Fatalf("SubtaskToolDetailExpanded = false after Enter, want true")
	}
	expanded := stripANSIForTest(tui.renderSubtaskBlock(block))
	if !strings.Contains(expanded, "工具详情") || !strings.Contains(expanded, "PgUp/PgDn") {
		t.Fatalf("expanded panel = %q, want inline scrollable tool detail", expanded)
	}
	_, _ = tui.updateChatKey("esc", tea.KeyPressMsg{})
	if tui.chat.SubtaskToolDetailExpanded {
		t.Fatalf("SubtaskToolDetailExpanded = true after Esc, want false")
	}
}

func TestSubtaskBlockUsesAvailableWidthForRowSummary(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 180, height: 30, mode: uipage.Chat}
	tui.initChatComponents()
	block := tui.ensureToolBlock()
	tui.chat.CurrentToolBlock = block
	intent := "并行检查配置和 TUI 字段传播路径，避免新增字段在保存时丢失"
	path := "internal/tui/pages/config/forms.go"
	block.Add(&toolEntry{ID: "spawn-1", Name: "Spawn", RawName: "spawn", Intent: intent, Status: toolRunning})
	block.Add(&toolEntry{ID: "spawn:spawn-1:read-1", ParentID: "spawn-1", Name: "Readfile", RawName: "readfile", ParamsRaw: map[string]any{"path": path}, Status: toolRunning})

	plain := stripANSIForTest(tui.renderSubtaskBlock(block))
	if !strings.Contains(plain, intent) {
		t.Fatalf("renderSubtaskBlock() = %q, want full subtask intent on wide viewport", plain)
	}
	if !strings.Contains(plain, path) {
		t.Fatalf("renderSubtaskBlock() = %q, want full active tool path on wide viewport", plain)
	}
}

func TestGlobalToolDetailSkipsSpawnAndSubtaskChildren(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100, height: 28, mode: uipage.Chat}
	tui.initChatComponents()
	block := tui.ensureToolBlock()
	block.Add(&toolEntry{ID: "spawn-1", Name: "Spawn", RawName: "spawn", Intent: "调研提示词", Status: toolRunning})
	block.Add(&toolEntry{ID: "spawn:spawn-1:http-1", ParentID: "spawn-1", Name: "HTTP", RawName: "http", Intent: "获取资料", Status: toolRunning})
	block.Add(&toolEntry{ID: "read-1", Name: "Readfile", RawName: "readfile", Intent: "读取文件", Status: toolDone})
	tui.chat.SelectedToolID = "spawn:spawn-1:http-1"

	tui.toggleToolDetail()
	if !tui.chat.ShowToolDetail {
		t.Fatalf("ShowToolDetail = false after toggle, want true")
	}
	if got, want := tui.chat.SelectedToolID, "read-1"; got != want {
		t.Fatalf("SelectedToolID = %q, want %q", got, want)
	}
}

func TestGlobalToolDetailNoopsWhenOnlySubtasks(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100, height: 28, mode: uipage.Chat}
	tui.initChatComponents()
	block := tui.ensureToolBlock()
	block.Add(&toolEntry{ID: "spawn-1", Name: "Spawn", RawName: "spawn", Intent: "调研提示词", Status: toolRunning})
	block.Add(&toolEntry{ID: "spawn:spawn-1:http-1", ParentID: "spawn-1", Name: "HTTP", RawName: "http", Intent: "获取资料", Status: toolRunning})

	tui.toggleToolDetail()
	if tui.chat.ShowToolDetail {
		t.Fatalf("ShowToolDetail = true with only subtask tools, want false")
	}
}

func TestSubtaskBlockRendersAsTranscriptContent(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100, height: 30, mode: uipage.Chat}
	tui.initChatComponents()
	block := tui.ensureToolBlock()
	block.Add(&toolEntry{ID: "spawn-1", Name: "Spawn", RawName: "spawn", Intent: "调研提示词", Status: toolRunning})
	block.Add(&toolEntry{ID: "spawn:spawn-1:http-1", ParentID: "spawn-1", Name: "HTTP", RawName: "http", Intent: "获取资料", Status: toolDone, Result: strings.Repeat("result\n", 20)})
	block.Add(&toolEntry{ID: "spawn:spawn-1:read-1", ParentID: "spawn-1", Name: "Readfile", RawName: "readfile", Intent: "读取文件", Status: toolRunning})

	collapsed := tui.renderSubtaskBlock(block)
	if got := renderedLineCount(collapsed); got == 0 {
		t.Fatalf("collapsed rendered rows = 0; panel = %q", stripANSIForTest(collapsed))
	}
	_, _ = tui.updateChatKey("enter", tea.KeyPressMsg{})
	expanded := tui.renderSubtaskBlock(block)
	if got := renderedLineCount(expanded); got <= renderedLineCount(collapsed) {
		t.Fatalf("expanded rendered rows = %d, collapsed rows = %d; panel = %q", got, renderedLineCount(collapsed), stripANSIForTest(expanded))
	}
	plain := stripANSIForTest(expanded)
	if !strings.Contains(plain, "获取资料") || !strings.Contains(plain, "读取文件") || !strings.Contains(plain, "工具详情") {
		t.Fatalf("expanded panel = %q, want tools and detail", plain)
	}
}

func renderedLineCount(s string) int {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func TestSubtaskBlockTimelineWindowsAroundRunningTool(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100, height: 24, mode: uipage.Chat}
	tui.initChatComponents()
	block := tui.ensureToolBlock()
	block.Add(&toolEntry{ID: "spawn-1", Name: "Spawn", RawName: "spawn", Intent: "调研提示词", Status: toolRunning})
	for i := 0; i < 12; i++ {
		status := toolDone
		if i == 10 {
			status = toolRunning
		}
		block.Add(&toolEntry{ID: fmt.Sprintf("spawn:spawn-1:tool-%02d", i), ParentID: "spawn-1", Name: "Readfile", RawName: "readfile", Intent: fmt.Sprintf("读取文件 %02d", i), Status: status})
	}

	plain := stripANSIForTest(tui.renderSubtaskBlock(block))
	if !strings.Contains(plain, "读取文件 10") {
		t.Fatalf("renderSubtaskBlock() = %q, want running tool visible", plain)
	}
	if !strings.Contains(plain, "上方还有") {
		t.Fatalf("renderSubtaskBlock() = %q, want above-more hint", plain)
	}
	if strings.Contains(plain, "读取文件 00") {
		t.Fatalf("renderSubtaskBlock() = %q, should window old tools out", plain)
	}
	if got, want := tui.chat.SubtaskToolCursor, 10; got != want {
		t.Fatalf("SubtaskToolCursor = %d, want running tool index %d", got, want)
	}
}

func TestSubtaskToolCursorMovesWithArrowKeys(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100, height: 28, mode: uipage.Chat}
	tui.initChatComponents()
	block := tui.ensureToolBlock()
	block.Add(&toolEntry{ID: "spawn-1", Name: "Spawn", RawName: "spawn", Intent: "调研提示词", Status: toolRunning})
	block.Add(&toolEntry{ID: "spawn:spawn-1:tool-1", ParentID: "spawn-1", Name: "Search", RawName: "search", Intent: "搜索", Status: toolDone})
	block.Add(&toolEntry{ID: "spawn:spawn-1:tool-2", ParentID: "spawn-1", Name: "Readfile", RawName: "readfile", Intent: "读取", Status: toolRunning})

	_ = tui.renderSubtaskBlock(block)
	if got, want := tui.chat.SubtaskToolCursor, 1; got != want {
		t.Fatalf("initial SubtaskToolCursor = %d, want running index %d", got, want)
	}
	_, _ = tui.updateChatKey("up", tea.KeyPressMsg{})
	if got, want := tui.chat.SubtaskToolCursor, 0; got != want {
		t.Fatalf("SubtaskToolCursor after up = %d, want %d", got, want)
	}
	_, _ = tui.updateChatKey("down", tea.KeyPressMsg{})
	if got, want := tui.chat.SubtaskToolCursor, 1; got != want {
		t.Fatalf("SubtaskToolCursor after down = %d, want %d", got, want)
	}
}

func TestSubtaskBlockShowsWaitingForNextTool(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100, height: 28, mode: uipage.Chat}
	tui.initChatComponents()
	block := tui.ensureToolBlock()
	block.Add(&toolEntry{ID: "spawn-1", Name: "Spawn", RawName: "spawn", Intent: "调研提示词", Status: toolRunning})
	block.Add(&toolEntry{ID: "spawn:spawn-1:tool-1", ParentID: "spawn-1", Name: "Search", RawName: "search", Intent: "搜索", Status: toolDone})

	plain := stripANSIForTest(tui.renderSubtaskBlock(block))
	if !strings.Contains(plain, "等待下一个工具调用") {
		t.Fatalf("renderSubtaskBlock() = %q, want waiting next tool row", plain)
	}
}

func TestRenderTitledRoundBoxKeepsRightBorderAligned(t *testing.T) {
	box := renderTitledRoundBox(48, "子任务  共 3 个 · 3 运行中 · 0 完成 · 0 失败", []string{
		styleHL.Render("检查子任务面板实现") + styleDim.Render(" · ") + styleToolDim.Render("Find implementation files and docs that mention subtask blocks"),
		styleDim.Render("─ 工具调用 ─────────────────────────────────────────────"),
	})
	for i, line := range strings.Split(box, "\n") {
		if got, want := lipgloss.Width(line), 48; got != want {
			t.Fatalf("line %d width = %d, want %d; line = %q", i, got, want, line)
		}
	}
}

func TestThinkingBoxUsesRoundedTitleBorder(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100}
	tui.initChatComponents()
	got := tui.renderThinkingBox("正在分析", true, time.Now(), time.Time{})
	plain := stripANSIForTest(got)
	if !strings.Contains(plain, "╭") || !strings.Contains(plain, "╯") {
		t.Fatalf("renderThinkingBox() = %q, want rounded border", plain)
	}
	if !strings.Contains(plain, "思考") {
		t.Fatalf("renderThinkingBox() = %q, want title in border", plain)
	}
}

func TestSubtaskBlockTitleShowsStatusIconAndFailureReason(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100, height: 30, mode: uipage.Chat}
	tui.initChatComponents()
	block := tui.ensureToolBlock()
	block.Add(&toolEntry{ID: "spawn-1", Name: "Spawn", RawName: "spawn", Intent: "失败子任务", Status: toolError, Result: "quota exceeded\ntry later"})

	panel := stripANSIForTest(tui.renderSubtaskBlock(block))
	if !strings.Contains(panel, "✗ 子任务") {
		t.Fatalf("renderSubtaskBlock() = %q, want failed title icon", panel)
	}
	if !strings.Contains(panel, "quota exceeded") {
		t.Fatalf("renderSubtaskBlock() = %q, want failure reason", panel)
	}
}

func TestSubtaskBlockPrioritizesFailureReasonOverLastChild(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100, height: 30, mode: uipage.Chat}
	tui.initChatComponents()
	block := tui.ensureToolBlock()
	tui.chat.CurrentToolBlock = block
	block.Add(&toolEntry{ID: "spawn-1", Name: "Spawn", RawName: "spawn", Intent: "失败子任务", Status: toolError, Result: "quota exceeded\ntry later"})
	block.Add(&toolEntry{ID: "spawn:spawn-1:read-1", ParentID: "spawn-1", Name: "Readfile", RawName: "readfile", Intent: "读取文件", Status: toolDone})

	panel := stripANSIForTest(tui.renderSubtaskBlock(block))
	if !strings.Contains(panel, "quota exceeded") {
		t.Fatalf("renderSubtaskBlock() = %q, want failure reason even when subtask has children", panel)
	}
	if !strings.Contains(panel, "错误:") || !strings.Contains(panel, "quota exceeded") {
		t.Fatalf("renderSubtaskBlock() = %q, want selected subtask error line", panel)
	}
}

func TestChatTopMetaRendersModelAndThinkingParameter(t *testing.T) {
	tui := &TUI{
		i18n:           newTranslator(LocaleEN),
		width:          100,
		currentSession: protocol.SessionInfo{ModelRef: "deepseek/deepseek-v4-flash"},
		configState: protocol.ConfigParams{Models: []protocol.ConfigModel{{
			Provider:  "deepseek",
			Model:     "deepseek-v4-flash",
			Reasoning: deepSeekReasoning("high"),
		}}},
	}

	meta := stripANSIForTest(tui.chatTopMeta())
	if !strings.Contains(meta, "deepseek/deepseek-v4-flash") {
		t.Fatalf("chatTopMeta() = %q, want model ref", meta)
	}
	if !strings.Contains(meta, "◇ Think High") {
		t.Fatalf("chatTopMeta() = %q, want compact thinking parameter", meta)
	}
}

func TestChatTopMetaOmitsContextStats(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100}
	tui.providerName = "openai"
	tui.modelName = "gpt-4.1"
	tui.contextTokens = 36200
	tui.contextWindow = 400000

	got := stripANSIForTest(tui.chatTopMeta())
	if !strings.Contains(got, "openai/gpt-4.1") {
		t.Fatalf("chatTopMeta() = %q, want model ref", got)
	}
	if strings.Contains(got, "ctx") || strings.Contains(got, "36.2k") || strings.Contains(got, "400k") {
		t.Fatalf("chatTopMeta() = %q, should omit context stats", got)
	}
}

func TestRenderChatStatusBarShowsContextAndUsage(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100}
	tui.hasUsage = true
	tui.contextTokens = 36200
	tui.contextWindow = 400000
	tui.lastInputTok = 32600
	tui.lastOutputTok = 2300
	tui.lastCachedTok = 29700
	tui.lastTokensPerSec = 50

	raw := tui.renderChatStatusBar()
	got := stripANSIForTest(raw)
	for _, want := range []string{"ctx 36.2k/400k", "9%", "↑32.6k", "↓2.3k", "↻29.7k", "50t/s"} {
		if !strings.Contains(got, want) && !strings.Contains(raw, want) {
			t.Fatalf("renderChatStatusBar() = %q, want %q", got, want)
		}
	}
}

func TestInputComposerMarkerRepeatsForWrappedVisualLines(t *testing.T) {
	long := strings.Repeat("长", 40)
	got := stripANSIForTest(renderInputComposerBar(24, []string{long}, false, true))
	lines := strings.Split(got, "\n")
	if len(lines) < 2 {
		t.Fatalf("renderInputComposerBar() = %q, want wrapped lines", got)
	}
	for _, line := range lines {
		if !strings.Contains(line, "▌") {
			t.Fatalf("renderInputComposerBar() = %q, every wrapped line should keep input marker", got)
		}
	}
}

func TestPendingImagePasteDoesNotRenderAsAttachmentPanel(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24}
	tui.initChatComponents()
	tui.chat.EnqueueImagePaste(pendingImagePaste{Name: "image.png", SourceKind: "clipboard"})
	if panel := tui.renderAttachmentPanel(); panel != "" {
		t.Fatalf("renderAttachmentPanel() = %q, pending paste should render as overlay instead", panel)
	}
	if overlay := tui.renderPendingImagePasteOverlay(80); !strings.Contains(stripANSIForTest(overlay), "image.png") {
		t.Fatalf("renderPendingImagePasteOverlay() = %q, want pending image name", overlay)
	}
}

func TestThinkingBoxStreamingGrowsButStaysBounded(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100}
	tui.initChatComponents()
	started := time.Now().Add(-time.Second)
	short := tui.renderThinkingBox("短句", true, started, time.Time{})
	long := tui.renderThinkingBox("第一行\n第二行\n第三行\n第四行\n第五行\n第六行\n第七行", true, started, time.Time{})
	shortRows := chatpage.RenderedLineCount(short)
	longRows := chatpage.RenderedLineCount(long)
	if longRows <= shortRows {
		t.Fatalf("streaming thinking height = %d, want taller than short height %d", longRows, shortRows)
	}
	if maxRows := reasoningRunningMaxRows + 3; longRows > maxRows {
		t.Fatalf("streaming thinking height = %d, want at most %d", longRows, maxRows)
	}
	plain := stripANSIForTest(long)
	if strings.Contains(plain, "第一行") || !strings.Contains(plain, "第七行") {
		t.Fatalf("renderThinkingBox(long) = %q, want clipped tail preview", plain)
	}
}

func TestThinkingBoxCompletedShowsAtMostThreeBodyRows(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100}
	started := time.Now().Add(-time.Second)
	ended := time.Now()
	got := stripANSIForTest(tui.renderThinkingBox("第一行\n第二行\n第三行\n第四行\n第五行", false, started, ended))
	if !strings.Contains(got, "第一行") || !strings.Contains(got, "第二行") {
		t.Fatalf("renderThinkingBox() = %q, want leading completed reasoning lines", got)
	}
	if strings.Contains(got, "第四行") || strings.Contains(got, "第五行") {
		t.Fatalf("renderThinkingBox() = %q, should clip completed reasoning after three rows", got)
	}
	if !strings.Contains(got, "...") {
		t.Fatalf("renderThinkingBox() = %q, want overflow marker", got)
	}
}

func TestWaitingAfterToolWithCompletedToolUsesBottomLoadingHint(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24}
	tui.initChatComponents()
	tui.chat.Loading = true
	tui.chat.Phase = phaseWaitingAfterTool
	tui.chat.PhaseStart = time.Now().Add(-time.Second)
	block := tui.ensureToolBlock()
	block.Add(&toolEntry{ID: "1", Name: "Read", Intent: "读取文件", Status: toolDone, StartedAt: time.Now().Add(-2 * time.Second), EndedAt: time.Now().Add(-time.Second)})

	tui.syncContent()
	view := stripANSIForTest(tui.replaceLiveTranscriptPlaceholders(tui.chat.Viewport.View()))
	if strings.Contains(view, "工具已完成") || strings.Contains(view, "子任务已完成") {
		t.Fatalf("view = %q, should not duplicate bottom loading status", view)
	}
	input := stripANSIForTest(tui.renderInputArea())
	if !strings.Contains(input, "工具已完成，正在请求模型继续") {
		t.Fatalf("renderInputArea() = %q, want tool waiting hint", input)
	}
}

func TestEscClearsDraftWithoutDiscardConfirm(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24}
	tui.initChatComponents()
	tui.mode = uipage.Chat
	tui.chat.Textarea.SetValue("需要清空的草稿")

	_, cmd := tui.updateChatKeyNormal("esc", tea.KeyPressMsg{})
	if cmd == nil {
		t.Fatalf("updateChatKeyNormal(esc) returned nil command, want input focus command")
	}
	if got := tui.chat.Textarea.Value(); got != "" {
		t.Fatalf("Textarea.Value() = %q, want empty draft", got)
	}
	if tui.chat.HasDiscardDraftConfirm() {
		t.Fatal("HasDiscardDraftConfirm() = true, want direct clear without confirm")
	}
}

func TestImagePasteOverlayLeftAlignsWithInputArea(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24}
	tui.initChatComponents()
	view := strings.Join([]string{
		"pet top",
		"pet meta",
		"pet bottom",
		"top separator",
		"content 1",
		"content 2",
		"content 3",
		"content 4",
		"  ────────────────────────────────",
		"  ▌ 输入消息...",
		"  help",
		"  status",
	}, "\n")
	panel := "╭────────╮\n│ image │\n╰────────╯"

	got := tui.overlayImagePasteAboveInput(view, panel, "")
	lines := strings.Split(got, "\n")
	if len(lines) < 8 {
		t.Fatalf("overlayImagePasteAboveInput() lines = %d, want >= 8", len(lines))
	}
	if !strings.HasPrefix(lines[5], "  ╭") {
		t.Fatalf("overlay first line = %q, want left aligned with two-space input margin", lines[5])
	}
	if lines[0] != "pet top" || lines[1] != "pet meta" || lines[2] != "pet bottom" || lines[3] != "top separator" {
		t.Fatalf("overlayImagePasteAboveInput() = %q, should not cover top chrome", got)
	}
}

func TestImagePasteOverlayKeepsTinyViewUnchanged(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 8}
	tui.initChatComponents()
	view := strings.Join([]string{
		"pet top",
		"pet meta",
		"pet bottom",
		"top separator",
		"  ────────────────────────────────",
		"  ▌ 输入消息...",
		"  help",
		"  status",
	}, "\n")
	panel := "╭────────╮\n│ image │\n╰────────╯"

	got := tui.overlayImagePasteAboveInput(view, panel, "")
	if got != view {
		t.Fatalf("overlayImagePasteAboveInput() = %q, want tiny view unchanged to avoid covering top chrome", got)
	}
}

func TestRestoreSummaryBoxRendersCompactContent(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100}
	content := strings.Join([]string{
		"上一轮工具操作摘要：",
		"6 次 · 5 成功 / 1 失败",
		"失败：exec · go test ./... 超时",
		"变更：editfile ×1，filesystem ×1",
		"最近：editfile → readfile → filesystem → exec",
		"已折叠 2 次较早操作",
	}, "\n")
	got := stripANSIForTest(tui.renderRestoreSummaryBox(content))
	if strings.Count(got, "上一轮工具操作") != 1 {
		t.Fatalf("renderRestoreSummaryBox() = %q, want single title", got)
	}
	checks := []string{"6 次 · 5 成功 / 1 失败", "失败：exec", "变更：editfile", "最近：editfile", "已折叠 2 次"}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Fatalf("renderRestoreSummaryBox() = %q, want %q", got, want)
		}
	}
}

func TestSubtaskSectionDividerReachesRightEdge(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100, height: 40}
	tui.initChatComponents()
	block := &toolBlock{}
	block.Add(&toolEntry{
		ID:      "spawn-1",
		Name:    "Spawn",
		RawName: "spawn",
		Intent:  "研究渲染机制",
		Status:  toolRunning,
		ParamsRaw: map[string]any{
			"model": "DF/MiniMax-M3",
			"tools": "[http exec]",
			"task":  "研究 TUI 渲染刷新机制",
		},
	})
	tui.chat.CurrentToolBlock = block
	got := stripANSIForTest(tui.renderSubtaskBlock(block))
	for _, line := range strings.Split(got, "\n") {
		if !strings.Contains(line, "─ 当前子任务") && !strings.Contains(line, "─ 工具调用") {
			continue
		}
		if strings.Contains(line, "   │") {
			t.Fatalf("subtask section divider has a visible right gap: %q", line)
		}
	}
}

func TestRunningSubtaskLiveElapsedKeepsBoxWidth(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 72, height: 40}
	tui.initChatComponents()
	block := &toolBlock{}
	block.Add(&toolEntry{ID: "spawn-1", Name: "Spawn", RawName: "spawn", Intent: strings.Repeat("调研渲染稳定性", 4), Status: toolRunning, StartedAt: time.Now().Add(-12 * time.Second)})
	block.Add(&toolEntry{ID: "spawn:spawn-1:read-1", ParentID: "spawn-1", Name: "Readfile", RawName: "readfile", Intent: strings.Repeat("读取很长的上下文文件", 4), Status: toolRunning, StartedAt: time.Now().Add(-12 * time.Second)})
	tui.chat.CurrentToolBlock = block

	raw := tui.renderSubtaskBlock(block)
	if !strings.Contains(raw, elapsedMarkerPrefix) {
		t.Fatalf("renderSubtaskBlock() = %q, want live elapsed marker", raw)
	}
	rendered := tui.replaceLiveTranscriptPlaceholders(raw)
	if strings.Contains(rendered, elapsedMarkerPrefix) || strings.Contains(rendered, "suna-spinner") {
		t.Fatalf("rendered subtask block still contains live marker: %q", rendered)
	}
	plain := stripANSIForTest(rendered)
	if !strings.Contains(plain, "12.") {
		t.Fatalf("rendered subtask block = %q, want dynamic elapsed around 12s", plain)
	}
	lines := strings.Split(strings.TrimRight(plain, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("rendered subtask block has %d lines, want box", len(lines))
	}
	wantWidth := lipgloss.Width(lines[0])
	for _, line := range lines[1:] {
		if got := lipgloss.Width(line); got != wantWidth {
			t.Fatalf("subtask box line width mismatch: got %d want %d line=%q\n%s", got, wantWidth, line, plain)
		}
	}
}

func TestLiveElapsedFormatKeepsFixedWidth(t *testing.T) {
	cases := []struct {
		name    string
		seconds float64
		want    string
	}{
		{name: "negative", seconds: -1, want: " 0.0s"},
		{name: "zero", seconds: 0, want: " 0.0s"},
		{name: "sub ten", seconds: 9.99, want: " 9.9s"},
		{name: "near hundred", seconds: 99.95, want: "99.9s"},
		{name: "hundred", seconds: 100, want: " 100s"},
		{name: "near thousand", seconds: 999.9, want: " 999s"},
		{name: "thousand plus", seconds: 1000, want: "999+s"},
	}
	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := formatElapsedSeconds(tt.seconds)
			if got != tt.want {
				t.Fatalf("formatElapsedSeconds(%v) = %q, want %q", tt.seconds, got, tt.want)
			}
			if width := lipgloss.Width(got); width != lipgloss.Width(elapsedPlaceholderText) {
				t.Fatalf("formatElapsedSeconds(%v) width = %d, want %d", tt.seconds, width, lipgloss.Width(elapsedPlaceholderText))
			}
		})
	}
}

func TestSubtaskDurationFallsBackToEndedAt(t *testing.T) {
	tui := &TUI{}
	started := time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC)
	entry := &toolEntry{Status: toolDone, StartedAt: started, EndedAt: started.Add(1500 * time.Millisecond)}

	if got := tui.subtaskDuration(entry); got != "1.5s" {
		t.Fatalf("subtaskDuration() = %q, want 1.5s", got)
	}
}

func TestProgressBlocksShareLeftIndent(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100}
	tui.initChatComponents()

	mainTools := &toolBlock{}
	mainTools.Add(&toolEntry{ID: "tool-1", Name: "Search", RawName: "search", Intent: "查找资料", Status: toolDone})

	subtasks := &toolBlock{}
	subtasks.Add(&toolEntry{ID: "spawn-1", Name: "Spawn", RawName: "spawn", Intent: "调研方案", Status: toolRunning})

	blocks := map[string]string{
		"tool":     tui.renderToolBlock(mainTools),
		"thinking": tui.renderThinkingBox("正在分析", false, time.Now().Add(-time.Second), time.Now()),
		"subtask":  tui.renderSubtaskBlock(subtasks),
	}
	for name, rendered := range blocks {
		first := strings.Split(strings.TrimRight(rendered, "\n"), "\n")[0]
		if got, want := leadingSpaces(first), len(transcriptBlockIndent); got != want {
			t.Fatalf("%s block first line has %d leading spaces, want %d: %q", name, got, want, first)
		}
	}
}

func leadingSpaces(s string) int {
	for i, r := range s {
		if r != ' ' {
			return i
		}
	}
	return len(s)
}

func TestConfirmClipboardImagePasteSavesAttachment(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24}
	tui.initChatComponents()
	tui.currentSession.ID = "session-1"
	tui.attachmentStatus = protocol.AttachmentStatusResult{SessionID: "session-1", Root: t.TempDir()}
	data := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 'I', 'H', 'D', 'R'}
	tui.chat.EnqueueImagePaste(pendingImagePaste{SourceKind: "clipboard_image", Name: "clipboard-image.png", MimeType: "image/png", Size: int64(len(data)), Data: data})

	tui.confirmPendingImagePaste()
	if len(tui.chat.Attachments) != 1 {
		t.Fatalf("attachments len = %d, want 1", len(tui.chat.Attachments))
	}
	got := tui.chat.Attachments[0]
	if got.SourceKind != protocol.AttachmentKindAttachment || got.Path == "" {
		t.Fatalf("attachment = %+v, want persisted attachment", got)
	}
	if _, err := os.Stat(got.Path); err != nil {
		t.Fatalf("saved image stat: %v", err)
	}
}

func TestClipboardImagePasteIgnoredAfterTerminalPaste(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24}
	tui.initChatComponents()
	started := time.Now()
	tui.lastPasteAt = started.Add(time.Millisecond)
	data := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 'I', 'H', 'D', 'R'}
	msg := clipboardImagePasteMsg{StartedAt: started, Pending: pendingImagePaste{SourceKind: "clipboard_image", Name: "clipboard-image.png", MimeType: "image/png", Size: int64(len(data)), Data: data}}

	model, _ := tui.Update(msg)
	tui = model.(*TUI)
	if tui.chat.ActiveImagePaste() != nil {
		t.Fatalf("clipboard image paste should be ignored when a later PasteMsg already arrived")
	}
}

func TestCachedStreamingStateKeepsOnlyTailLines(t *testing.T) {
	tui := &TUI{width: 80}
	tui.chat.Viewport.SetHeight(2)
	msg := &chatMsg{Role: "assistant", Streaming: true, Stream: &chatpage.StreamingTextState{}}
	for i := 0; i < 160; i++ {
		msg.Stream.Append("line\n")
	}

	got := tui.cachedStreamingState(msg, 20)
	if lines := strings.Count(got, "\n") + 1; lines > 120 {
		t.Fatalf("rendered lines = %d, want <= 120", lines)
	}
	if msg.Stream.Raw.Len() == 0 {
		t.Fatal("raw stream is empty, want full content retained")
	}
	if msg.Stream.DroppedLines == 0 {
		t.Fatal("dropped lines = 0, want tail window to drop early rendered lines")
	}
}

func TestAppendStreamingDeltaLongLineMatchesFullRender(t *testing.T) {
	chunk := strings.Repeat("abcdefghijklmnopqrstuvwxyz", 400)
	lines := []string{""}
	lastWidth := 0
	pendingNewlines := 0

	appendStreamingDelta(&lines, &lastWidth, &pendingNewlines, chunk, 40)

	got := strings.Join(lines, "\n")
	want := renderStreamingText(chunk, 40)
	if got != want {
		t.Fatalf("stream render mismatch\ngot:  %q\nwant: %q", got, want)
	}
}

func BenchmarkAppendStreamingDeltaLongLine(b *testing.B) {
	chunk := strings.Repeat("abcdefghijklmnopqrstuvwxyz", 1000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		lines := []string{""}
		lastWidth := 0
		pendingNewlines := 0
		appendStreamingDelta(&lines, &lastWidth, &pendingNewlines, chunk, 120)
	}
}
