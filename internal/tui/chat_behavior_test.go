package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/tui/components/toolview"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
	tuitransport "github.com/alanchenchen/suna/internal/tui/transport"
)

func TestCancellingAllowsDraftEditingButHandleSendIsNoop(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), mode: uipage.Chat, cancelling: true}
	tui.initChatComponents()
	tui.chat.Loading = true
	tui.chat.Textarea.SetValue("不应发送")

	if cmd := tui.handleSend(); cmd != nil {
		t.Fatal("handleSend returned a command while cancelling")
	}
	if tui.currentInputPolicy().Locked {
		t.Fatal("composer is locked while cancelling")
	}
	if len(tui.chat.Messages) != 0 || tui.chat.Textarea.Value() != "不应发送" {
		t.Fatal("handleSend mutated composer or transcript while cancelling")
	}
}

func TestCancelTransportFailureKeepsCancellingState(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), mode: uipage.Chat, cancelling: true}
	tui.initChatComponents()
	tui.chat.Loading = true

	tui.handleProtocolResultMsg(cancelResultMsg{Err: fmt.Errorf("connection closed")})
	if !tui.cancelling || !tui.chat.Loading || tui.currentRunCanControl {
		t.Fatalf("cancelling/loading/control = %v/%v/%v", tui.cancelling, tui.chat.Loading, tui.currentRunCanControl)
	}
}

func TestAgentCancellingWaitsForFinalNotification(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), mode: uipage.Chat, currentRunCanControl: true}
	tui.initChatComponents()
	started := time.Now().Add(-time.Second)
	entry := &toolview.Entry{ID: "exec-1", Name: "Exec", Status: toolview.StatusRunning, StartedAt: started}
	tui.chat.ActiveTools = map[string]*toolview.Entry{"exec-1": entry}
	tui.chat.ToolStartTimes = map[string]time.Time{"exec-1": started}
	tui.chat.Loading = true

	tui.handleAgentRunNotification(protocol.AgentRunParams{RunID: "run-1", State: protocol.AgentRunState("cancelling")})
	if !tui.cancelling || !tui.chat.Loading || tui.currentRunCanControl {
		t.Fatalf("cancelling/loading/control = %v/%v/%v", tui.cancelling, tui.chat.Loading, tui.currentRunCanControl)
	}
	if entry.Status != toolview.StatusCancelling || tui.currentInputPolicy().AllowCancel || tui.currentInputPolicy().Locked {
		t.Fatalf("tool/policy = %v/%#v", entry.Status, tui.currentInputPolicy())
	}

	tui.handleAgentRunNotification(protocol.AgentRunParams{RunID: "run-1", State: protocol.AgentRunCancelled})
	if tui.cancelling || tui.chat.Loading || entry.Status != toolview.StatusCancelled || tui.chatSpinnerTicking {
		t.Fatalf("final cancelling/loading/tool/spinner = %v/%v/%v/%v", tui.cancelling, tui.chat.Loading, entry.Status, tui.chatSpinnerTicking)
	}
	messages := len(tui.chat.Messages)
	tui.completedRunID = ""
	tui.handleAgentRunNotification(protocol.AgentRunParams{RunID: "run-1", State: protocol.AgentRunCancelled})
	if len(tui.chat.Messages) != messages {
		t.Fatalf("duplicate cancelled notice appended: %d -> %d", messages, len(tui.chat.Messages))
	}
}

func TestEscEntersCancellingWithoutPrematureNotice(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), mode: uipage.Chat, currentRunCanControl: true}
	tui.initChatComponents()
	tui.chat.Loading = true

	_, _ = tui.updateChatEsc()
	if !tui.cancelling || !tui.chat.Loading {
		t.Fatalf("cancelling/loading = %v/%v", tui.cancelling, tui.chat.Loading)
	}
	if len(tui.chat.Messages) != 0 {
		t.Fatalf("premature cancellation notice count = %d", len(tui.chat.Messages))
	}
}

func TestGuardVisibleInFinalChatViewAndLocksComposer(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), mode: uipage.Chat, ready: true, width: 100, height: 30}
	tui.initChatComponents()
	tui.chat.EnqueueGuardConfirm(&chatpage.GuardConfirmView{ID: "guard-1", Tool: "writefile", Risk: "high", Reason: "needs confirmation"})
	_ = tui.syncInputFocus()

	view := stripANSIForTest(tui.viewChat())
	for _, want := range []string{"安全确认", "正在等待安全确认", "←→ 选择 · Enter 确认所选 · Esc 拒绝"} {
		if !strings.Contains(view, want) {
			t.Fatalf("viewChat() = %q, want %q", view, want)
		}
	}
	if tui.chat.Textarea.Focused() {
		t.Fatal("textarea.Focused() = true while final chat view has an active guard")
	}
}

func TestLeaveCurrentSessionForWelcomeReleasesChatRuntime(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), mode: uipage.Chat, width: 80, height: 24}
	tui.initChatComponents()
	tui.currentSession = protocol.SessionInfo{ID: "session-1"}
	tui.chat.Messages = []chatpage.Msg{{Role: "assistant", Content: strings.Repeat("x", 4096)}}
	tui.chat.ActiveTools = map[string]*toolEntry{"tool-1": {ID: "tool-1", Result: strings.Repeat("y", 4096)}}
	tui.chat.ToolStartTimes = map[string]time.Time{"tool-1": time.Now()}
	tui.chat.CurrentToolBlock = &toolBlock{Entries: map[string]*toolview.Entry{}}
	tui.chat.InteractionQueue = []chatpage.Interaction{{ID: "ask-1"}}
	tui.chat.Attachments = []attachmentItem{{Name: "image.png"}}
	tui.chat.Viewport.SetContentLines([]string{"rendered transcript"})
	tui.selectionMode = true

	tui.leaveCurrentSessionForWelcome()

	if tui.mode != uipage.Welcome {
		t.Fatalf("mode = %q, want welcome", tui.mode)
	}
	if tui.currentSession.ID != "" {
		t.Fatalf("current session = %q, want empty", tui.currentSession.ID)
	}
	if tui.selectionMode {
		t.Fatal("selectionMode = true after leaving Chat for Welcome")
	}
	if len(tui.chat.Messages) != 0 || len(tui.chat.ActiveTools) != 0 || len(tui.chat.InteractionQueue) != 0 || len(tui.chat.Attachments) != 0 {
		t.Fatal("chat runtime retains session data after leaving for welcome")
	}
	if tui.chat.CurrentToolBlock != nil {
		t.Fatal("current tool block was retained")
	}
	if got := strings.TrimSpace(tui.chat.Viewport.View()); got != "" {
		t.Fatalf("viewport content = %q, want blank viewport", got)
	}
}

func TestWelcomeDropsLateChatRuntimeNotifications(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), mode: uipage.Welcome}
	tui.handleNotificationMsg(agentDeltaMsg{Params: protocol.AgentDeltaParams{Content: "late output"}})
	tui.handleNotificationMsg(toolStartMsg{Params: protocol.ToolStartParams{ID: "tool-1", Tool: "exec"}})
	tui.handleNotificationMsg(skillLoadMsg{Params: protocol.SkillLoadParams{Name: "review", Status: "done"}})
	tui.handleNotificationMsg(memoryListMsg{Params: protocol.MemoryListResult{Memories: []protocol.MemoryItem{{Content: "late memory"}}}})

	if len(tui.chat.Messages) != 0 {
		t.Fatalf("messages = %d, want no late chat messages", len(tui.chat.Messages))
	}
	if len(tui.chat.ActiveTools) != 0 {
		t.Fatalf("active tools = %d, want no late tool state", len(tui.chat.ActiveTools))
	}
	if len(tui.chat.Memories) != 0 {
		t.Fatalf("memories = %d, want no late memory state", len(tui.chat.Memories))
	}
}

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

func TestRegisteredSlashCommandDoesNotAppendUserMessage(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 18, localCli: tuitransport.NewClient()}
	tui.initChatComponents()
	tui.chat.Textarea.SetValue("/mcp")

	tui.handleSend()

	if got := len(tui.chat.Messages); got != 0 {
		t.Fatalf("messages = %d after /mcp, want 0", got)
	}
	if !tui.chat.MCPOverlayOpen {
		t.Fatal("MCPOverlayOpen = false after /mcp, want true")
	}
}

func TestAllRegisteredSlashCommandsDoNotAppendUserMessage(t *testing.T) {
	for _, spec := range chatpage.AllCommands() {
		t.Run(spec.Cmd, func(t *testing.T) {
			tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 18, localCli: tuitransport.NewClient()}
			tui.initChatComponents()
			tui.chat.Textarea.SetValue(spec.Cmd)

			tui.handleSend()

			for _, msg := range tui.chat.Messages {
				if msg.Role == "user" {
					t.Fatalf("registered command %q appended a user transcript message", spec.Cmd)
				}
			}
		})
	}
}

func TestRegisteredSlashCommandKeepsAttachmentsAsDraft(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 18, localCli: tuitransport.NewClient()}
	tui.initChatComponents()
	tui.chat.Textarea.SetValue("/mcp")
	tui.chat.Attachments = []attachmentItem{{Name: "note.txt"}}

	tui.handleSend()

	if !tui.chat.MCPOverlayOpen {
		t.Fatal("MCPOverlayOpen = false after /mcp, want true")
	}
	if got := len(tui.chat.Messages); got != 0 {
		t.Fatalf("messages = %d after /mcp with attachment, want 0", got)
	}
	if got := len(tui.chat.Attachments); got != 1 {
		t.Fatalf("attachments = %d after /mcp, want preserved draft", got)
	}
}

func TestRegisteredSlashCommandTakesPriorityOverCustomAskUser(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 18, localCli: tuitransport.NewClient()}
	tui.initChatComponents()
	tui.chat.EnqueueInteraction(chatpage.Interaction{Kind: chatpage.InteractionAskUser, ID: "ask-1", Ask: &chatpage.AskUserView{ID: "ask-1", AllowCustom: true}})
	tui.chat.Textarea.SetValue("/mcp")

	tui.handleSend()

	if !tui.chat.MCPOverlayOpen {
		t.Fatal("MCPOverlayOpen = false after /mcp during custom AskUser, want true")
	}
	if ask := tui.chat.ActiveAsk(); ask == nil || ask.ID != "ask-1" {
		t.Fatalf("ActiveAsk() = %#v after /mcp, want original AskUser", ask)
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
	if strings.Contains(view, "正在自动压缩上下文") {
		t.Fatalf("compact status line = %q, should not duplicate bottom loading status", view)
	}
	input := stripANSIForTest(tui.renderInputArea())
	if !strings.Contains(input, "正在自动压缩上下文") || !strings.Contains(input, "完成后模型会自动继续") {
		t.Fatalf("renderInputArea() = %q, want compact loading", input)
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
	if strings.Contains(view, "正在压缩上下文") {
		t.Fatalf("viewport = %q, should not duplicate bottom loading status", view)
	}
	input := stripANSIForTest(tui.renderInputArea())
	if !strings.Contains(input, "正在压缩上下文") {
		t.Fatalf("renderInputArea() = %q, want manual compact loading before result", input)
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
	if strings.Contains(view, "正在请求模型") {
		t.Fatalf("view = %q, should not duplicate bottom loading status in transcript", view)
	}
	if strings.Contains(view, "Esc 取消") {
		t.Fatalf("view = %q, should not contain duplicate cancel hint in status line", view)
	}
	input := stripANSIForTest(tui.renderInputArea())
	if !strings.Contains(input, "正在请求模型") || !strings.Contains(input, "Esc 取消") {
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
	view := stripANSIForTest(tui.replaceLiveTranscriptPlaceholders(tui.chat.Viewport.View()))
	if strings.Contains(view, "正在请求主模型继续") {
		t.Fatalf("view = %q, should not duplicate bottom loading status in transcript", view)
	}
	input := stripANSIForTest(tui.renderInputArea())
	if !strings.Contains(input, "子任务已完成，正在请求主模型继续") || !strings.Contains(input, "Esc 取消") {
		t.Fatalf("renderInputArea() = %q, want subtask waiting placeholder", input)
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
	view := stripANSIForTest(tui.replaceLiveTranscriptPlaceholders(tui.chat.Viewport.View()))
	if strings.Contains(view, "执行工具中") {
		t.Fatalf("view = %q, should not repeat tool-specific global status", view)
	}
	if strings.Contains(view, "Esc 取消") {
		t.Fatalf("view = %q, should not contain duplicate bottom status line", view)
	}
	input := stripANSIForTest(tui.renderInputArea())
	if !strings.Contains(input, "执行工具中") || !strings.Contains(input, "Esc 取消") {
		t.Fatalf("renderInputArea() = %q, want tool running placeholder", input)
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

	view := stripANSIForTest(tui.renderSkillLoadMessage(&chatpage.SkillLoadView{Name: "img", Status: "loaded", Duration: time.Millisecond}))
	for _, want := range []string{"╭", "╰", "✓ 已加载 SKILL", "img", "1ms"} {
		if !strings.Contains(view, want) {
			t.Fatalf("renderSkillLoadMessage() = %q, want substring %q", view, want)
		}
	}
	if strings.Contains(view, "│✓") || strings.Contains(view, "img│") {
		t.Fatalf("renderSkillLoadMessage() = %q, want compact space inside box", view)
	}
	if got, want := leadingSpaces(strings.Split(view, "\n")[0]), len(transcriptBlockIndent); got != want {
		t.Fatalf("skill block leading spaces = %d, want %d", got, want)
	}
}

func TestRenderSkillLoadMessageSupportsLightTheme(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleEN), width: 80}
	applyTheme(ThemeLight)
	t.Cleanup(func() { applyTheme(ThemeDark) })

	view := stripANSIForTest(tui.renderSkillLoadMessage(&chatpage.SkillLoadView{Name: "img", Status: "loading", StartedAt: time.Now()}))
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

func TestReasoningRunningDetailRequestStillClipsSource(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100}

	var lines []string
	for i := 0; i < reasoningSummarySourceLines+20; i++ {
		lines = append(lines, fmt.Sprintf("line-old-%03d", i))
	}
	lines = append(lines, "line-new")
	got := stripANSIForTest(tui.renderThinkingBoxMode(strings.Join(lines, "\n"), true, true, time.Now(), time.Time{}))
	if strings.Contains(got, "line-old-000") {
		t.Fatalf("renderThinkingBoxMode() included clipped old reasoning: %q", got)
	}
	if !strings.Contains(got, "line-new") {
		t.Fatalf("renderThinkingBoxMode() = %q, want newest reasoning line", got)
	}
	if strings.Contains(got, "Ctrl+R") {
		t.Fatalf("renderThinkingBoxMode() = %q, running reasoning should not show detail hint", got)
	}
}

func TestReasoningCompletedDetailRendersCompleteContent(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100}

	var lines []string
	lines = append(lines, "line-first")
	for i := 0; i < reasoningSummarySourceLines+20; i++ {
		lines = append(lines, fmt.Sprintf("line-middle-%03d", i))
	}
	lines = append(lines, "line-last")
	got := stripANSIForTest(tui.renderThinkingBoxMode(strings.Join(lines, "\n"), false, true, time.Now(), time.Now()))
	if !strings.Contains(got, "line-first") || !strings.Contains(got, "line-last") {
		t.Fatalf("renderThinkingBoxMode() = %q, want complete reasoning content", got)
	}
	if strings.Contains(got, "Ctrl+R") {
		t.Fatalf("renderThinkingBoxMode() = %q, expanded reasoning should not show detail hint", got)
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

func TestInputPlaceholderHidesForWhitespaceDraft(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24}
	tui.initChatComponents()
	tui.chat.Textarea.SetValue(" ")

	view := stripANSIForTest(tui.renderInputArea())
	if strings.Contains(view, "Ask anything") || strings.Contains(view, "有什么可以帮你") {
		t.Fatalf("renderInputArea() = %q, should hide placeholder for whitespace draft", view)
	}
}

func newScrollableChatForTest(t *testing.T) *TUI {
	t.Helper()
	tui := &TUI{i18n: newTranslator(LocaleZH), mode: uipage.Chat, ready: true, width: 80, height: 18, currentSession: protocol.SessionInfo{ID: "session-1", Status: protocol.SessionStatusRunning}}
	tui.initChatComponents()
	for i := 0; i < 60; i++ {
		tui.appendNonToolMessage(chatMsg{Role: "system", Content: fmt.Sprintf("历史消息-%02d", i)})
	}
	tui.layoutChat()
	tui.syncContent()
	return tui
}

func TestManualScrollKeepsPositionAndMarksNewContent(t *testing.T) {
	tui := newScrollableChatForTest(t)
	tui.chat.SetTranscriptYOffset(10)
	tui.updateTranscriptFollowAfterNavigation()
	before := tui.chat.TranscriptYOffset

	tui.appendNonToolMessage(chatMsg{Role: "assistant", Content: strings.Repeat("新增内容 ", 20), Streaming: true})
	tui.chat.NewContentWhilePaused = true
	tui.transcriptSyncDirty = true
	_ = tui.flushScheduledTranscriptSync()

	if got := tui.chat.TranscriptYOffset; got != before {
		t.Fatalf("TranscriptYOffset = %d after append, want preserved %d", got, before)
	}
	if !tui.chat.ManualScrollPaused || !tui.chat.NewContentWhilePaused {
		t.Fatalf("paused/newContent = %v/%v, want true/true", tui.chat.ManualScrollPaused, tui.chat.NewContentWhilePaused)
	}
	if hint := stripANSIForTest(tui.responseNavHint()); !strings.Contains(hint, "↓ 新内容") || !strings.Contains(hint, "End") {
		t.Fatalf("navigation hint = %q, want new content End marker", hint)
	}
}

func TestPausedFollowMarksSingleLineStreamDeltaAsNewContent(t *testing.T) {
	tui := newScrollableChatForTest(t)
	tui.chat.SetTranscriptYOffset(10)
	tui.updateTranscriptFollowAfterNavigation()
	model, _ := tui.Update(agentDeltaMsg{Params: protocol.AgentDeltaParams{RunID: "run-1", Kind: protocol.AgentDeltaAssistant, Content: "x"}})
	tui = model.(*TUI)
	if !tui.chat.NewContentWhilePaused {
		t.Fatal("single-line stream delta did not mark new content while follow was paused")
	}
}

func TestManualScrollNeverReturnsToBottomWithoutUserAction(t *testing.T) {
	tui := newScrollableChatForTest(t)
	tui.chat.SetTranscriptYOffset(10)
	tui.updateTranscriptFollowAfterNavigation()
	before := tui.chat.TranscriptYOffset

	for i := 0; i < tui.chat.Viewport.Height()*2; i++ {
		tui.appendNonToolMessage(chatMsg{Role: "system", Content: fmt.Sprintf("新增-%02d", i)})
	}
	tui.transcriptSyncDirty = true
	_ = tui.flushScheduledTranscriptSync()

	if got := tui.chat.TranscriptYOffset; got != before {
		t.Fatalf("TranscriptYOffset = %d after large append, want %d", got, before)
	}
	if tui.chat.TranscriptAtBottom() || !tui.chat.ManualScrollPaused {
		t.Fatalf("atBottom/paused = %v/%v, want false/true", tui.chat.TranscriptAtBottom(), tui.chat.ManualScrollPaused)
	}
}

func TestEndReturnsToLatestAndRestoresFollow(t *testing.T) {
	tui := newScrollableChatForTest(t)
	tui.chat.SetTranscriptYOffset(10)
	tui.chat.NewContentWhilePaused = true
	tui.updateTranscriptFollowAfterNavigation()

	_, _ = tui.updateChatKeyNormal("end", tea.KeyPressMsg{})
	if !tui.chat.TranscriptAtBottom() || tui.chat.ManualScrollPaused || !tui.chat.FollowBottom || tui.chat.NewContentWhilePaused {
		t.Fatalf("atBottom/paused/follow/new = %v/%v/%v/%v", tui.chat.TranscriptAtBottom(), tui.chat.ManualScrollPaused, tui.chat.FollowBottom, tui.chat.NewContentWhilePaused)
	}
}

func TestHomeJumpsToLongResponseStartOnlyForEmptyInput(t *testing.T) {
	tui := newScrollableChatForTest(t)
	tui.chat.ResponseNavAvailable = true
	tui.chat.LastAssistantStartLine = 5
	tui.chat.JumpToBottom()

	_, _ = tui.updateChatKeyNormal("home", tea.KeyPressMsg{})
	if got := tui.chat.TranscriptYOffset; got != 5 {
		t.Fatalf("Home offset = %d, want 5", got)
	}
	if !tui.chat.ManualScrollPaused {
		t.Fatal("ManualScrollPaused = false after Home")
	}

	tui.chat.JumpToBottom()
	tui.chat.Textarea.SetValue("draft")
	before := tui.chat.TranscriptYOffset
	_, _ = tui.updateChatKeyNormal("home", tea.KeyPressMsg{})
	if got := tui.chat.TranscriptYOffset; got != before {
		t.Fatalf("Home with draft offset = %d, want %d", got, before)
	}
}

func TestHomeEndKeepTextareaEditingSemanticsWhenNavigationDoesNotApply(t *testing.T) {
	tui := newScrollableChatForTest(t)
	tui.chat.JumpToBottom()
	tui.chat.Textarea.SetValue("draft text")
	tui.chat.Textarea.CursorStart()

	model, _ := tui.updateChatKeyNormal("end", tea.KeyPressMsg(tea.Key{Code: tea.KeyEnd}))
	tui = model.(*TUI)
	if got := tui.chat.Textarea.LineInfo().CharOffset; got != len([]rune("draft text")) {
		t.Fatalf("End cursor offset = %d, want line end", got)
	}
	model, _ = tui.updateChatKeyNormal("home", tea.KeyPressMsg(tea.Key{Code: tea.KeyHome}))
	tui = model.(*TUI)
	if got := tui.chat.Textarea.LineInfo().CharOffset; got != 0 {
		t.Fatalf("Home cursor offset = %d, want line start", got)
	}
}

func TestRunTerminalKeepsManualReadingPosition(t *testing.T) {
	tui := newScrollableChatForTest(t)
	tui.activeRunID = "run-1"
	tui.runStartedAt = time.Now()
	tui.chat.SetTranscriptYOffset(10)
	tui.updateTranscriptFollowAfterNavigation()
	before := tui.chat.TranscriptYOffset

	tui.handleAgentRunNotification(protocol.AgentRunParams{RunID: "run-1", State: protocol.AgentRunDone})
	if got := tui.chat.TranscriptYOffset; got != before {
		t.Fatalf("run terminal offset = %d, want preserved %d", got, before)
	}
	if !tui.chat.ManualScrollPaused {
		t.Fatal("run terminal cleared manual follow pause")
	}
}

func TestSendingMessageReturnsToBottom(t *testing.T) {
	tui := newScrollableChatForTest(t)
	tui.chat.SetTranscriptYOffset(10)
	tui.updateTranscriptFollowAfterNavigation()
	tui.chat.Textarea.SetValue("new request")
	_ = tui.handleSend()
	tui.syncContent()
	if !tui.chat.TranscriptAtBottom() || tui.chat.ManualScrollPaused {
		t.Fatalf("send atBottom/paused = %v/%v, want true/false", tui.chat.TranscriptAtBottom(), tui.chat.ManualScrollPaused)
	}
}

func TestNavigationMarkerIsCenteredAndUsesSingleState(t *testing.T) {
	tui := newScrollableChatForTest(t)
	tui.chat.ResponseNavAvailable = true
	tui.chat.JumpToBottom()
	start := stripANSIForTest(tui.responseNavHint())
	if !strings.Contains(start, "↑ 查看本轮开头") || !strings.Contains(start, "Home") || len(start)-len(strings.TrimLeft(start, " ")) == 0 {
		t.Fatalf("start marker = %q, want centered Home marker", start)
	}

	tui.chat.SetTranscriptYOffset(10)
	tui.updateTranscriptFollowAfterNavigation()
	latest := stripANSIForTest(tui.responseNavHint())
	if !strings.Contains(latest, "↓ 回到最新") || !strings.Contains(latest, "End") || strings.Contains(latest, "查看本轮开头") {
		t.Fatalf("latest marker = %q, want single End marker", latest)
	}
}

func TestScrollingBackToBottomRestoresFollow(t *testing.T) {
	tui := newScrollableChatForTest(t)
	tui.chat.SetTranscriptYOffset(10)
	tui.chat.NewContentWhilePaused = true
	tui.updateTranscriptFollowAfterNavigation()

	tui.chat.JumpToBottom()
	tui.updateTranscriptFollowAfterNavigation()
	if tui.chat.ManualScrollPaused || !tui.chat.FollowBottom || tui.chat.NewContentWhilePaused {
		t.Fatalf("paused/follow/new = %v/%v/%v, want false/true/false", tui.chat.ManualScrollPaused, tui.chat.FollowBottom, tui.chat.NewContentWhilePaused)
	}
}
