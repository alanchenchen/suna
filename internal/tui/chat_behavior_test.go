package tui

import (
	"strings"
	"testing"
	"time"
)

func TestThinkingBoxCollapsedWhileStreamingAndStopsElapsed(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 100}
	started := time.Now().Add(-2 * time.Second)
	ended := started.Add(1500 * time.Millisecond)

	streaming := stripANSIForTest(tui.renderThinkingBox("第一段\n第二段\n最终判断", true, started, time.Time{}))
	if strings.Contains(streaming, "第一段") || strings.Contains(streaming, "第二段") {
		t.Fatalf("streaming reasoning should stay collapsed unless Ctrl+R expands it:\n%s", streaming)
	}
	if !strings.Contains(streaming, "最终判断") || !strings.Contains(streaming, "Ctrl+R") {
		t.Fatalf("collapsed streaming reasoning should show a compact summary and hint:\n%s", streaming)
	}

	completed := stripANSIForTest(tui.renderThinkingBox("第一段\n第二段\n最终判断", false, started, ended))
	if !strings.Contains(completed, "1.5s") {
		t.Fatalf("completed reasoning should show fixed duration, not live phase elapsed:\n%s", completed)
	}
	if strings.Contains(completed, "第一段") || strings.Contains(completed, "第二段") {
		t.Fatalf("completed reasoning should remain collapsed by default:\n%s", completed)
	}
}

func TestSendingMessageForcesScrollToBottom(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 18}
	tui.initChatComponents()
	for i := 0; i < 40; i++ {
		tui.appendNonToolMessage(chatMsg{role: "system", content: "历史消息"})
	}
	tui.syncContent()
	tui.vp.SetYOffset(0)
	tui.followBottom = false
	tui.ta.SetValue("新的问题")

	tui.handleSend()
	if !tui.vp.AtBottom() {
		t.Fatalf("sending a message should force viewport to bottom; y=%d", tui.vp.YOffset())
	}
	if !tui.followBottom {
		t.Fatalf("sending a message should restore follow-bottom mode")
	}
}

func TestSlashCommandForcesScrollToBottom(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 18}
	tui.initChatComponents()
	for i := 0; i < 40; i++ {
		tui.appendNonToolMessage(chatMsg{role: "system", content: "历史消息"})
	}
	tui.syncContent()
	tui.vp.SetYOffset(0)
	tui.followBottom = false
	tui.ta.SetValue("/compact")

	tui.handleSend()
	if !tui.vp.AtBottom() {
		t.Fatalf("sending a slash command should force viewport to bottom; y=%d", tui.vp.YOffset())
	}
	if !tui.followBottom {
		t.Fatalf("sending a slash command should restore follow-bottom mode")
	}
}

func TestLockedInputShowsStatusPlaceholder(t *testing.T) {
	tui := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 24}
	tui.initChatComponents()
	tui.loading = true
	tui.phase = phaseLLM
	tui.phaseStart = time.Now()
	tui.ta.Blur()

	view := stripANSIForTest(tui.renderInputArea())
	if !strings.Contains(view, "正在回复") || !strings.Contains(view, "Esc") {
		t.Fatalf("locked input should show active status and cancel hint:\n%s", view)
	}
	if tui.ta.Focused() {
		t.Fatalf("textarea should be blurred while input is locked")
	}
}
