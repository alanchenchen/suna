package tui

import (
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/alanchenchen/suna/internal/protocol"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
)

func newIncrementalTestTUI() *TUI {
	t := &TUI{i18n: newTranslator(LocaleZH), width: 80, height: 26, mode: uipage.Chat, ready: true}
	t.currentSession = protocol.SessionInfo{ID: "session"}
	t.initChatComponents()
	return t
}

// 增量窗口、行数与回复导航应和显式失效后的完整同步一致。
func assertRootTranscriptParity(t *testing.T, tui *TUI) {
	t.Helper()
	text := tui.chat.SelectionPlainText(0, tui.chat.TranscriptTotalLines-1)
	total, offset := tui.chat.TranscriptTotalLines, tui.chat.TranscriptYOffset
	start, lines, index := tui.chat.LastAssistantStartLine, tui.chat.LastAssistantLineCount, tui.chat.LastAssistantMsgIndex
	tui.chat.InvalidateTranscript()
	tui.syncContent()
	if got := tui.chat.SelectionPlainText(0, tui.chat.TranscriptTotalLines-1); got != text {
		t.Fatalf("incremental transcript differs from rebuild: %q != %q", text, got)
	}
	if total != tui.chat.TranscriptTotalLines || offset != tui.chat.TranscriptYOffset || start != tui.chat.LastAssistantStartLine || lines != tui.chat.LastAssistantLineCount || index != tui.chat.LastAssistantMsgIndex {
		t.Fatalf("layout/nav differs after rebuild: total=%d offset=%d start=%d lines=%d index=%d", total, offset, start, lines, index)
	}
}

func TestRootIncrementalLateToolAndHistoryReplacement(t *testing.T) {
	tui := newIncrementalTestTUI()
	tui.handleToolStartNotification(protocol.ToolStartParams{SessionID: "session", ID: "late", Tool: "readfile", Intent: "旧块"})
	tui.appendNonToolMessage(chatMsg{Role: "assistant", Content: "后续回复"})
	tui.syncContent()
	if !tui.handleNotificationMsg(toolEndMsg{Params: protocol.ToolEndParams{SessionID: "session", ID: "late", Tool: "readfile", Error: true, Result: "late failure"}}) {
		t.Fatal("late tool end did not classify body change")
	}
	tui.syncContent()
	if text := tui.chat.SelectionPlainText(0, tui.chat.TranscriptTotalLines-1); !strings.Contains(text, "✗") {
		t.Fatalf("late tool result not rendered: %q", text)
	}
	assertRootTranscriptParity(t, tui)
	for _, text := range []string{"替换历史一", "替换历史二"} {
		tui.applySessionSnapshot(protocol.SessionSnapshot{Session: protocol.SessionInfo{ID: "session"}, Messages: []protocol.SnapshotMessage{{Role: "assistant", Content: text}}})
		tui.syncContent()
		got := tui.chat.SelectionPlainText(0, tui.chat.TranscriptTotalLines-1)
		if !strings.Contains(got, text) || strings.Contains(got, "后续回复") {
			t.Fatalf("snapshot replacement stale: %q", got)
		}
		assertRootTranscriptParity(t, tui)
	}
}

func TestRootIncrementalSameNameThemeAndMarkdownLines(t *testing.T) {
	old := currentTheme
	t.Cleanup(func() { applyThemePalette(old) })
	tui := newIncrementalTestTUI()
	tui.appendNonToolMessage(chatMsg{Role: "assistant", Content: "正文\n\n```go\nvar x = 1\n```\n"})
	tui.syncContent()
	msg := &tui.chat.Messages[0]
	if msg.Render.Width != max(20, tui.width-6) {
		t.Fatalf("markdown width = %d", msg.Render.Width)
	}
	if got := chatpage.RenderedLineCount(tui.renderAssistantMessage(msg)); got != msg.Render.LineCount {
		t.Fatalf("indented line count = %d, cache = %d", got, msg.Render.LineCount)
	}
	before := msg.Render.Output
	palette := currentTheme
	palette.Text = color.RGBA{R: 240, G: 90, B: 130, A: 255}
	palette.Surface = color.RGBA{R: 30, G: 45, B: 60, A: 255}
	applyThemePalette(palette)
	tui.syncContent()
	if msg.Render.Output == before || msg.Render.Theme != old.Name {
		t.Fatal("same-name palette change reused stale markdown")
	}
	assertRootTranscriptParity(t, tui)
	before = msg.Render.Output
	tui.applyDetectedBackground(tea.BackgroundColorMsg{Color: color.RGBA{R: 245, G: 245, B: 245, A: 255}})
	if msg.Render.Output == before {
		t.Fatal("background adaptation reused stale markdown")
	}
	assertRootTranscriptParity(t, tui)
}

func TestRootIncrementalStreamFinishScrollSelectionAndNav(t *testing.T) {
	tui := newIncrementalTestTUI()
	tui.appendStreamMessage("assistant", "```text\n"+strings.Repeat("正文换行\n", 60)+"```\n")
	tui.syncContent()
	tui.chat.ScrollTranscript(-15)
	tui.chat.ManualScrollPaused = true
	tui.chat.FollowBottom = false
	offset := tui.chat.TranscriptYOffset
	tui.handleNotificationMsg(agentRunMsg{Params: protocol.AgentRunParams{SessionID: "session", State: protocol.AgentRunDone}})
	tui.syncContent()
	if tui.chat.Messages[0].Streaming || tui.chat.Messages[0].Stream != nil || !tui.chat.ResponseNavAvailable || tui.chat.TranscriptYOffset != offset {
		t.Fatal("stream finish lost scroll position or response nav")
	}
	tui.selection.Begin(offset, SelectionRegionTranscript)
	tui.selection.Extend(offset+2, tui.chat.TranscriptTotalLines)
	tui.syncSelectionRange()
	if tui.chat.SelectionStart != offset || tui.chat.SelectionEnd != offset+2 || tui.chat.SelectionPlainText(offset, offset+2) == "" {
		t.Fatal("selection did not track transcript lines")
	}
	assertRootTranscriptParity(t, tui)
	if !tui.chat.JumpToLastAssistantStart() || tui.chat.TranscriptYOffset != tui.chat.LastAssistantStartLine {
		t.Fatal("response navigation failed")
	}
}

func TestRootNoBodyNotificationsAndInputSelectionSkipBody(t *testing.T) {
	tui := newIncrementalTestTUI()
	tui.appendNonToolMessage(chatMsg{Role: "assistant", Content: "保留正文"})
	tui.syncContent()
	tui.chat.ManualScrollPaused = true
	for _, msg := range []tea.Msg{
		usageMsg{Params: protocol.UsageParams{SessionID: "session", InputTokens: 10}},
		skillLoadMsg{Params: protocol.SkillLoadParams{Name: "review", Status: "loading"}},
		skillReviewMsg{Params: protocol.SkillReviewParams{Name: "review", Status: "running"}},
		agentDeltaMsg{Params: protocol.AgentDeltaParams{SessionID: "foreign", Content: "迟到"}},
		toolGuardMsg{Params: protocol.ToolGuardParams{ToolCallID: "missing"}},
	} {
		tui.Update(msg)
		if tui.transcriptSyncDirty || tui.chat.NewContentWhilePaused {
			t.Fatalf("no-body event scheduled transcript: %T", msg)
		}
	}
	tui.handleProtocolResultMsg(attachmentStatusResultMsg{})
	if tui.transcriptSyncDirty {
		t.Fatal("status method response scheduled body sync")
	}
	tui.chat.Textarea.SetValue("第一行\n第二行")
	tui.layoutChat()
	tui.handleInputSelectionMouse(tea.MouseClickMsg{Button: tea.MouseLeft, Y: tui.inputSelectionStartY()})
	tui.handleInputSelectionMouse(tea.MouseMotionMsg{Button: tea.MouseLeft, Y: tui.inputSelectionStartY() + 1})
	if tui.transcriptSyncDirty {
		t.Fatal("input selection scheduled body sync")
	}
	tui.flushScheduledTranscriptSync()
	if tui.chat.SelectionStart != -1 {
		t.Fatal("input selection leaked into transcript")
	}
}
