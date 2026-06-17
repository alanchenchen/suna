package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/alanchenchen/suna/internal/protocol"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
	tuitransport "github.com/alanchenchen/suna/internal/tui/transport"
)

func TestThinkingBoxCollapsedWhileStreamingAndStopsElapsed(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100}
	started := time.Now().Add(-2 * time.Second)
	ended := started.Add(1500 * time.Millisecond)

	streaming := stripANSIForTest(tui.renderThinkingBox("第一段\n第二段\n最终判断", true, started, time.Time{}))
	if strings.Contains(streaming, "第一段") || strings.Contains(streaming, "第二段") {
		t.Fatalf("renderThinkingBox(streaming) = %q, should not show hidden reasoning lines", streaming)
	}
	if !strings.Contains(streaming, "最终判断") || !strings.Contains(streaming, "Ctrl+R") {
		t.Fatalf("renderThinkingBox(streaming) = %q, want compact summary and Ctrl+R hint", streaming)
	}

	completed := stripANSIForTest(tui.renderThinkingBox("第一段\n第二段\n最终判断", false, started, ended))
	if !strings.Contains(completed, "1.5s") {
		t.Fatalf("renderThinkingBox(completed) = %q, want fixed duration", completed)
	}
	if strings.Contains(completed, "第一段") || strings.Contains(completed, "第二段") {
		t.Fatalf("renderThinkingBox(completed) = %q, should not show hidden reasoning lines", completed)
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

	tui.handleLocalNotification(localNotification{method: protocol.NotifyStream, params: []byte(`{"chunk":"hello"}`)})
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
	if count := strings.Count(view, "◎ 思考"); count != 1 {
		t.Fatalf("strings.Count(view, %q) = %d, want %d; view = %q", "◎ 思考", count, 1, view)
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

func TestRunningToolSuppressesDuplicateStatusLine(t *testing.T) {
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
	if strings.Contains(view, "Esc 取消") {
		t.Fatalf("view = %q, should not contain duplicate bottom status line", view)
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
	boxEnd := strings.LastIndex(view, "╰")
	inputStart := strings.LastIndex(view, "describe this image")
	if boxEnd < 0 || inputStart < 0 || boxEnd >= inputStart {
		t.Fatalf("renderInputArea() = %q, want attachment box before composer", view)
	}
	between := view[boxEnd:inputStart]
	if !strings.Contains(between, "──") {
		t.Fatalf("renderInputArea() = %q, want separator between attachment box and composer", view)
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
