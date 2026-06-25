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
	tuitransport "github.com/alanchenchen/suna/internal/tui/transport"
)

func TestThinkingBoxCollapsedShowsAdaptivePreviewAndStopsElapsed(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100}
	started := time.Now().Add(-2 * time.Second)
	ended := started.Add(1500 * time.Millisecond)

	streaming := stripANSIForTest(tui.renderThinkingBox("第一段\n第二段\n最终判断", true, started, time.Time{}))
	if !strings.Contains(streaming, "第一段") || !strings.Contains(streaming, "第二段") || !strings.Contains(streaming, "最终判断") {
		t.Fatalf("renderThinkingBox(streaming) = %q, want adaptive reasoning preview", streaming)
	}
	if strings.Contains(streaming, "Ctrl+R") {
		t.Fatalf("renderThinkingBox(streaming) = %q, should not spend a body row on shortcut hint", streaming)
	}

	completed := stripANSIForTest(tui.renderThinkingBox("第一段\n第二段\n最终判断", false, started, ended))
	if !strings.Contains(completed, "1.5s") {
		t.Fatalf("renderThinkingBox(completed) = %q, want fixed duration", completed)
	}
	if !strings.Contains(completed, "Ctrl+R") || !strings.Contains(completed, "详情") {
		t.Fatalf("renderThinkingBox(completed) = %q, want collapsed detail hint", completed)
	}
	if !strings.Contains(completed, "第一段") || !strings.Contains(completed, "第二段") || !strings.Contains(completed, "最终判断") {
		t.Fatalf("renderThinkingBox(completed) = %q, want up to three completed reasoning lines", completed)
	}
}

func TestSendingMessageForcesScrollToBottom(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 18}
	tui.initChatComponents()
	for i := 0; i < 40; i++ {
		tui.appendNonToolMessage(chatMsg{Role: "system", Content: "历史消息"})
	}
	tui.syncContent()
	tui.chat.Viewport.SetYOffset(0)
	tui.chat.FollowBottom = false
	tui.chat.Textarea.SetValue("新的问题")

	tui.handleSend()
	if !tui.chat.Viewport.AtBottom() {
		t.Fatalf("vp.AtBottom() = false after message send; YOffset = %d", tui.chat.Viewport.YOffset())
	}
	if !tui.chat.FollowBottom {
		t.Fatalf("followBottom = false after message send, want true")
	}
}

func TestSlashCommandForcesScrollToBottom(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 18}
	tui.initChatComponents()
	for i := 0; i < 40; i++ {
		tui.appendNonToolMessage(chatMsg{Role: "system", Content: "历史消息"})
	}
	tui.syncContent()
	tui.chat.Viewport.SetYOffset(0)
	tui.chat.FollowBottom = false
	tui.chat.Textarea.SetValue("/compact")

	tui.handleSend()
	if !tui.chat.Viewport.AtBottom() {
		t.Fatalf("vp.AtBottom() = false after slash command; YOffset = %d", tui.chat.Viewport.YOffset())
	}
	if !tui.chat.FollowBottom {
		t.Fatalf("followBottom = false after slash command, want true")
	}
}

func TestMouseWheelScrollsAcrossTranscriptWindowWhenViewportAtWindowTop(t *testing.T) {
	// Chat viewport 只持有当前 transcript window。如果先让 Bubble viewport 处理滚轮，
	// 当局部 viewport 已在窗口顶部时 delta 会被 clamp 成 0，导致无法继续往上跨 window。
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 18, mode: uipage.Chat}
	tui.initChatComponents()
	for i := 0; i < 80; i++ {
		tui.appendNonToolMessage(chatMsg{Role: "system", Content: fmt.Sprintf("历史消息-%02d", i)})
	}
	tui.layoutChat()
	tui.syncContent()
	tui.chat.SetTranscriptYOffset(20)
	tui.chat.Viewport.SetYOffset(0)
	before := tui.chat.TranscriptYOffset

	_, _ = tui.updateChat(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelUp}))

	if got := tui.chat.TranscriptYOffset; got >= before {
		t.Fatalf("TranscriptYOffset = %d after wheel up, want < %d", got, before)
	}
}

func TestCompactLocksInputWithoutCancelHint(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24}
	tui.initChatComponents()
	tui.chat.Compacting = true
	tui.chat.Textarea.Blur()

	if !tui.inputLocked() {
		t.Fatalf("inputLocked() = false during compact, want true")
	}
	view := stripANSIForTest(tui.renderInputArea())
	if !strings.Contains(view, "正在压缩上下文") {
		t.Fatalf("renderInputArea() = %q, want compact running placeholder", view)
	}
	if strings.Contains(view, "Esc") || strings.Contains(view, "取消") {
		t.Fatalf("renderInputArea() = %q, should not advertise cancellation for compact", view)
	}
}

func TestAutoCompactNotificationShowsRunning(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24}
	tui.initChatComponents()

	tui.handleLocalNotification(localNotification{method: protocol.NotifyCompactResult, params: []byte(`{"running":true}`)})
	if !tui.chat.Compacting {
		t.Fatalf("compacting = false after compact running result, want true")
	}
	if !tui.chat.Loading {
		t.Fatalf("loading = false after compact running result, want true")
	}
	if !tui.inputLocked() {
		t.Fatalf("inputLocked() = false during compact, want true")
	}
	if len(tui.chat.Messages) != 0 {
		t.Fatalf("messages = %d after compact running result, want no transient message", len(tui.chat.Messages))
	}
	tui.syncContent()
	view := stripANSIForTest(tui.chat.Viewport.View())
	if !strings.Contains(view, "正在自动压缩上下文") || !strings.Contains(view, "完成后模型会自动继续") {
		t.Fatalf("compact status line = %q, want compact loading", view)
	}
}

func TestAutoCompactRunningFalseClearsLoading(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24}
	tui.initChatComponents()
	tui.handleLocalNotification(localNotification{method: protocol.NotifyCompactResult, params: []byte(`{"running":true}`)})

	tui.handleLocalNotification(localNotification{method: protocol.NotifyCompactResult, params: []byte(`{"running":false}`)})
	if tui.chat.Compacting {
		t.Fatalf("compacting = true after compact running false, want false")
	}
	if len(tui.chat.Messages) != 0 {
		t.Fatalf("messages = %d after compact running false, want no transient message", len(tui.chat.Messages))
	}
}

func TestAutoCompactErrorClearsLoadingAndShowsError(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24}
	tui.initChatComponents()
	tui.handleLocalNotification(localNotification{method: protocol.NotifyCompactResult, params: []byte(`{"running":true}`)})

	tui.handleLocalNotification(localNotification{method: protocol.NotifyCompactResult, params: []byte(`{"running":false,"error":"自动上下文压缩失败，请尝试 /compact"}`)})
	if tui.chat.Compacting {
		t.Fatalf("compacting = true after compact error, want false")
	}
	if len(tui.chat.Messages) != 1 {
		t.Fatalf("messages = %d after compact error, want only error", len(tui.chat.Messages))
	}
	view := stripANSIForTest(tui.chat.Messages[0].Content.(string))
	if !strings.Contains(view, "自动上下文压缩失败") {
		t.Fatalf("error message = %q, want compact error", view)
	}
}

func TestAutoCompactRunningClearsWhenStreamStarts(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24}
	tui.initChatComponents()
	tui.handleLocalNotification(localNotification{method: protocol.NotifyCompactResult, params: []byte(`{"running":true}`)})

	tui.handleLocalNotification(localNotification{method: protocol.NotifyAgentDelta, params: []byte(`{"kind":"assistant","content":"hello"}`)})
	if tui.chat.Compacting {
		t.Fatalf("compacting = true after stream starts, want false")
	}
}

func TestManualCompactCommandShowsLoadingBeforeDeferredRequest(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24, ready: true, localCli: tuitransport.NewClient()}
	tui.initChatComponents()
	tui.chat.Textarea.SetValue("/compact")

	_, cmd := tui.updateChatKey("enter", tea.KeyPressMsg{})
	if cmd == nil {
		t.Fatal("updateChatKey() returned nil, want deferred compact command")
	}
	if !tui.chat.Compacting {
		t.Fatalf("compacting = false immediately after /compact, want true")
	}
	if !tui.chat.Loading {
		t.Fatalf("loading = false immediately after /compact, want true")
	}
	if !tui.inputLocked() {
		t.Fatalf("inputLocked() = false immediately after /compact, want true")
	}
	tui.syncContent()
	view := stripANSIForTest(tui.chat.Viewport.View())
	if !strings.Contains(view, "正在压缩上下文") {
		t.Fatalf("viewport = %q, want manual compact loading before result", view)
	}
}

func TestManualCompactResultPanelClearsLoading(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24}
	tui.initChatComponents()
	tui.chat.Compacting = true
	tui.chat.Loading = true
	tui.chat.Phase = phaseFirstLLM

	tui.handleLocalNotification(localNotification{method: protocol.NotifyCompactResult, params: []byte(`{"before_tokens":100,"after_tokens":50,"context_window":1000}`)})
	if tui.chat.Compacting || tui.chat.Loading {
		t.Fatalf("compacting/loading = %v/%v after manual compact result, want false/false", tui.chat.Compacting, tui.chat.Loading)
	}
	if len(tui.chat.Messages) != 1 {
		t.Fatalf("messages = %d after manual compact result, want 1 panel", len(tui.chat.Messages))
	}
	if got := tui.chat.Messages[0].Role; got != "panel" {
		t.Fatalf("message role = %q, want panel", got)
	}
}

func TestCompactResultUnlocksInput(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24}
	tui.initChatComponents()
	tui.chat.Compacting = true

	data := []byte(`{"before_tokens":100,"after_tokens":50,"context_window":1000}`)
	tui.handleLocalNotification(localNotification{method: protocol.NotifyCompactResult, params: data})

	if tui.chat.Compacting {
		t.Fatalf("compacting = true after compact result, want false")
	}
	if tui.inputLocked() {
		t.Fatalf("inputLocked() = true after compact result, want false")
	}
}

func TestActiveReasoningSuppressesDuplicateStatusLine(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24}
	tui.initChatComponents()
	tui.chat.Loading = true
	tui.chat.Phase = phaseThinking
	tui.chat.PhaseStart = time.Now().Add(-time.Second)
	tui.appendNonToolMessage(chatMsg{Role: "reasoning", Content: "正在分析", Streaming: true, StartedAt: time.Now().Add(-time.Second)})

	tui.syncContent()
	view := stripANSIForTest(tui.chat.Viewport.View())
	if !strings.Contains(view, "╭") || !strings.Contains(view, "思考") {
		t.Fatalf("view = %q, want reasoning title box", view)
	}
	if strings.Contains(view, "Esc 取消") {
		t.Fatalf("view = %q, should not contain duplicate bottom status line", view)
	}
}

func TestWaitingWithoutVisibleProgressShowsStatusLine(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24}
	tui.initChatComponents()
	tui.chat.Loading = true
	tui.chat.Phase = phaseFirstLLM
	tui.chat.PhaseStart = time.Now().Add(-time.Second)

	tui.syncContent()
	view := stripANSIForTest(tui.chat.Viewport.View())
	if !strings.Contains(view, "等待 LLM") {
		t.Fatalf("view = %q, want wait status line", view)
	}
	if strings.Contains(view, "Esc 取消") {
		t.Fatalf("view = %q, should not contain duplicate cancel hint in status line", view)
	}
	input := stripANSIForTest(tui.renderInputArea())
	if !strings.Contains(input, "等待 LLM") || !strings.Contains(input, "Esc 取消") {
		t.Fatalf("renderInputArea() = %q, want cancellable locked input placeholder", input)
	}
}

func TestWaitingAfterSubtaskShowsSpecificStatusLine(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24}
	tui.initChatComponents()
	tui.chat.Loading = true
	tui.chat.Phase = phaseWaitingAfterTool
	tui.chat.LastWaitingTool = "spawn"
	tui.chat.PhaseStart = time.Now().Add(-time.Second)

	tui.syncContent()
	view := stripANSIForTest(tui.chat.Viewport.View())
	if !strings.Contains(view, "子任务已完成，等待主模型继续") {
		t.Fatalf("view = %q, want subtask waiting status line", view)
	}
}

func TestRunningToolShowsCompactStatusLine(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24}
	tui.initChatComponents()
	tui.chat.Loading = true
	tui.chat.Phase = phaseTool
	tui.chat.PhaseStart = time.Now().Add(-time.Second)
	block := tui.ensureToolBlock()
	block.Add(&toolEntry{ID: "1", Name: "Read", Intent: "读取文件", Status: toolRunning, StartedAt: time.Now().Add(-time.Second)})
	tui.chat.ActiveTools = map[string]*toolEntry{"1": block.Entries["1"]}

	tui.syncContent()
	view := stripANSIForTest(tui.chat.Viewport.View())
	if strings.Contains(view, "执行工具中") {
		t.Fatalf("view = %q, should not repeat tool-specific global status", view)
	}
	if strings.Contains(view, "Esc 取消") {
		t.Fatalf("view = %q, should not contain duplicate bottom status line", view)
	}
	input := stripANSIForTest(tui.renderInputArea())
	if !strings.Contains(input, "运行中") || !strings.Contains(input, "Esc 取消") {
		t.Fatalf("renderInputArea() = %q, want compact running placeholder", input)
	}
}

func TestLockedInputShowsStatusPlaceholder(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24}
	tui.initChatComponents()
	tui.chat.Loading = true
	tui.chat.Phase = phaseLLM
	tui.chat.PhaseStart = time.Now()
	tui.chat.Textarea.Blur()

	view := stripANSIForTest(tui.renderInputArea())
	if !strings.Contains(view, "正在回复") || !strings.Contains(view, "Esc") {
		t.Fatalf("renderInputArea() = %q, want active status and cancel hint", view)
	}
	if tui.chat.Textarea.Focused() {
		t.Fatalf("textarea.Focused() = true while input is locked, want false")
	}
}

func TestWelcomeNewInitializesChatBeforeResetPhase(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24, ready: true}
	tui.configState = protocol.ConfigParams{Models: []protocol.ConfigModel{{Provider: "test", Model: "model", ContextWindow: 128000, MaxOutputTokens: 8192}}}
	tui.initWelcomeList()

	_, cmd := tui.updateWelcome(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if tui.mode != uipage.Chat {
		t.Fatalf("mode = %q, want %q", tui.mode, uipage.Chat)
	}
	if tui.chat.Textarea.Placeholder == "" {
		t.Fatalf("textarea.Placeholder = empty, want initialized chat textarea")
	}
	if cmd == nil {
		t.Fatalf("cmd = nil, want chat focus command")
	}
}

func TestRenderSkillLoadMessageUsesHighlightedBadges(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80}
	applyTheme(ThemeDark)

	view := stripANSIForTest(tui.renderSkillLoadMessage(protocol.SkillLoadParams{Name: "img", Status: "loaded"}))
	for _, want := range []string{"╭", "╰", "✓ 已加载 SKILL", "img"} {
		if !strings.Contains(view, want) {
			t.Fatalf("renderSkillLoadMessage() = %q, want substring %q", view, want)
		}
	}
	if strings.Contains(view, "│✓") || strings.Contains(view, "img│") {
		t.Fatalf("renderSkillLoadMessage() = %q, want horizontal breathing room inside box", view)
	}
}

func TestRenderSkillLoadMessageSupportsLightTheme(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 80}
	applyTheme(ThemeLight)
	t.Cleanup(func() { applyTheme(ThemeDark) })

	view := stripANSIForTest(tui.renderSkillLoadMessage(protocol.SkillLoadParams{Name: "img", Status: "loading"}))
	for _, want := range []string{"╭", "╰", "◐ LOADING SKILL", "img"} {
		if !strings.Contains(view, want) {
			t.Fatalf("renderSkillLoadMessage() = %q, want substring %q", view, want)
		}
	}
}

func TestRenderAttachmentPanelUsesBox(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 100}
	tui.chat.Attachments = []attachmentItem{{Type: "image", Name: "ScreenShot_2026-05-29_121010_728.png", Size: 161500}}

	panel := stripANSIForTest(tui.renderAttachmentPanel())
	for _, want := range []string{"╭", "╰", "Pending attachments", "ScreenShot_2026-05-29_121010_728.png"} {
		if !strings.Contains(panel, want) {
			t.Fatalf("renderAttachmentPanel() = %q, want substring %q", panel, want)
		}
	}
}

func TestRenderInputAreaSeparatesAttachmentBoxFromComposer(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 80, height: 24}
	tui.initChatComponents()
	tui.chat.Textarea.SetValue("describe this image")
	tui.chat.Attachments = []attachmentItem{{Type: "image", Name: "image.png", Size: 1024}}

	view := stripANSIForTest(tui.renderInputArea())
	attachmentStart := strings.Index(view, "Pending attachments")
	inputStart := strings.LastIndex(view, "describe this image")
	if attachmentStart < 0 || inputStart < 0 || !(attachmentStart < inputStart) {
		t.Fatalf("renderInputArea() = %q, want attachment box before input box", view)
	}
	if strings.Contains(view, "Input") {
		t.Fatalf("renderInputArea() = %q, should not show redundant input title", view)
	}
	if strings.Contains(view, "@") {
		t.Fatalf("renderInputArea() = %q, should not advertise @ file command", view)
	}
}

func TestCtrlJInsertsNewline(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24}
	tui.initChatComponents()
	tui.chat.Textarea.SetValue("第一行")

	_, cmd := tui.updateChatKeyNormal("ctrl+j", tea.KeyPressMsg{})
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	if got := tui.chat.Textarea.Value(); got != "第一行\n" {
		t.Fatalf("textarea value = %q, want newline appended", got)
	}
}

func TestCachedStreamingTextMatchesFullRender(t *testing.T) {
	tui := &TUI{width: 40}
	msg := &chatMsg{Role: "assistant", Streaming: true}
	chunks := []string{"hello", " world this is a long line", " that wraps", "\nsecond", " line", "\n\nthird", "\n", "after trailing", " 中文字符"}
	content := ""
	for _, chunk := range chunks {
		content += chunk
		got := tui.cachedStreamingText(msg, content, 12)
		want := renderStreamingText(content, 12)
		if got != want {
			t.Fatalf("cachedStreamingText mismatch after %q\ngot:\n%q\nwant:\n%q", chunk, got, want)
		}
	}
	// 重复渲染同一内容应直接复用缓存且保持一致。
	got := tui.cachedStreamingText(msg, content, 12)
	want := renderStreamingText(content, 12)
	if got != want {
		t.Fatalf("cachedStreamingText cached mismatch\ngot:\n%q\nwant:\n%q", got, want)
	}
}

func TestRenderStreamingTextExpandsTabsBeforeWrapping(t *testing.T) {
	got := renderStreamingText("if ok {\n\treturn true", 12)
	if strings.Contains(got, "\t") {
		t.Fatalf("renderStreamingText() = %q, should not contain tabs", got)
	}
	if !strings.Contains(got, "    return") {
		t.Fatalf("renderStreamingText() = %q, want tab expanded indentation", got)
	}
}

func TestReasoningDetailClipsSourceBeforeRendering(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100}
	tui.chat.ShowReasoningDetail = true

	var lines []string
	for i := 0; i < reasoningDetailSourceLines+20; i++ {
		lines = append(lines, fmt.Sprintf("line-old-%03d", i))
	}
	lines = append(lines, "line-new")
	got := stripANSIForTest(tui.renderThinkingBox(strings.Join(lines, "\n"), true, time.Now(), time.Time{}))
	if strings.Contains(got, "line-old-000") {
		t.Fatalf("renderThinkingBox() included clipped old reasoning: %q", got)
	}
	if !strings.Contains(got, "line-new") {
		t.Fatalf("renderThinkingBox() = %q, want newest reasoning line", got)
	}
}

func TestReasoningCompletedDetailClipsHeadBeforeMarkdown(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100}
	tui.chat.ShowReasoningDetail = true

	var lines []string
	lines = append(lines, "line-new")
	for i := 0; i < reasoningDetailSourceLines+20; i++ {
		lines = append(lines, "line-old")
	}
	lines = append(lines, "line-clipped")
	got := stripANSIForTest(tui.renderThinkingBox(strings.Join(lines, "\n"), false, time.Now(), time.Now()))
	if !strings.Contains(got, "line-new") {
		t.Fatalf("renderThinkingBox() = %q, want first reasoning line", got)
	}
	if strings.Contains(got, "line-clipped") {
		t.Fatalf("renderThinkingBox() included clipped tail reasoning: %q", got)
	}
}

func TestRecentTextStreamActiveOnlySuppressesNearStreamingText(t *testing.T) {
	now := time.Now()
	tui := &TUI{lastTextStreamAt: now.Add(-textStreamSpinnerSuppressWindow / 2)}
	tui.chat.Messages = []chatMsg{{Role: "assistant", Streaming: true, Content: "hello"}}
	if !tui.recentTextStreamActive(now) {
		t.Fatalf("recentTextStreamActive() = false, want true for recent streaming assistant")
	}

	tui.lastTextStreamAt = now.Add(-textStreamSpinnerSuppressWindow * 2)
	if tui.recentTextStreamActive(now) {
		t.Fatalf("recentTextStreamActive() = true for stale text stream")
	}

	tui.lastTextStreamAt = now
	tui.chat.Messages = []chatMsg{{Role: "assistant", Streaming: false, Content: "done"}}
	if tui.recentTextStreamActive(now) {
		t.Fatalf("recentTextStreamActive() = true without streaming message")
	}
}

func TestUsageNotificationPrefersEstimatedContextTokens(t *testing.T) {
	tui := &TUI{}

	tui.handleUsageNotification(protocol.UsageParams{
		InputTokens:            160000,
		OutputTokens:           120,
		ContextTokens:          160120,
		EstimatedContextTokens: 98000,
		ContextWindow:          1000000,
	})

	if got, want := tui.contextTokens, 98000; got != want {
		t.Fatalf("contextTokens = %d, want %d", got, want)
	}
	if got, want := tui.lastInputTok, 160000; got != want {
		t.Fatalf("lastInputTok = %d, want provider input %d", got, want)
	}
}

func TestUsageNotificationFallsBackToProviderContextTokens(t *testing.T) {
	tui := &TUI{}

	tui.handleUsageNotification(protocol.UsageParams{
		InputTokens:   42000,
		OutputTokens:  200,
		ContextTokens: 42200,
		ContextWindow: 400000,
	})

	if got, want := tui.contextTokens, 42200; got != want {
		t.Fatalf("contextTokens = %d, want fallback %d", got, want)
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

func TestWaitingAfterToolWithCompletedToolShowsCompactSpinner(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24}
	tui.initChatComponents()
	tui.chat.Loading = true
	tui.chat.Phase = phaseWaitingAfterTool
	tui.chat.PhaseStart = time.Now().Add(-time.Second)
	block := tui.ensureToolBlock()
	block.Add(&toolEntry{ID: "1", Name: "Read", Intent: "读取文件", Status: toolDone, StartedAt: time.Now().Add(-2 * time.Second), EndedAt: time.Now().Add(-time.Second)})

	tui.syncContent()
	view := stripANSIForTest(tui.chat.Viewport.View())
	if strings.Contains(view, "工具已完成") || strings.Contains(view, "子任务已完成") {
		t.Fatalf("view = %q, waiting after completed tool should use compact empty spinner", view)
	}
	if !strings.Contains(view, "1.0s") {
		t.Fatalf("view = %q, want compact spinner elapsed time", view)
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
		if !strings.Contains(line, "当前子任务") && !strings.Contains(line, "工具调用") {
			continue
		}
		if strings.Contains(line, "   │") {
			t.Fatalf("subtask section divider has a visible right gap: %q", line)
		}
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
	tui.attachmentStatus.Root = t.TempDir()
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
