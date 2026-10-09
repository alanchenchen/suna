package chat

import (
	"reflect"
	"strings"
	"testing"
)

func TestRetainedHeaderPropagationHydratesBody(t *testing.T) {
	for _, action := range []string{"scroll", "copy"} {
		t.Run(action, func(t *testing.T) {
			var m Model
			m.InitComponents(ComponentDeps{})
			m.Viewport.SetHeight(2)
			calls := 0
			deps := retainedTestDeps(&calls)
			body := "full\nsecond\nthird"
			m.AppendMessage(Msg{Role: "assistant", Content: "first"})
			m.AppendMessage(Msg{Role: "assistant", Content: body})
			for i := 0; i < 10; i++ {
				m.AppendMessage(Msg{Role: "panel", Content: "tail"})
			}
			m.SyncTranscript(deps)
			m.SetTranscriptYOffset(0)
			m.ManualScrollPaused = true
			cached := m.Messages[1].Render
			renderAssistant := deps.RenderAssistant
			deps.RenderAssistant = func(msg *Msg) string {
				// 前驱只改变标题分组，后继的正文缓存元数据必须保留。
				if msg == &m.Messages[1] && !reflect.DeepEqual(msg.Render, cached) {
					t.Fatalf("header propagation changed body cache: got %+v want %+v", msg.Render, cached)
				}
				return renderAssistant(msg)
			}
			m.Messages[0] = Msg{Role: "panel", Content: "p"}
			m.MarkMessageDirty(0)
			calls = 0
			m.SyncTranscript(deps)
			// 前驱缩短一行并结束分组，新增标题与窗口重叠，但正文起点仍在窗口之外。
			slot, endSlot := m.transcriptMessageRange(1)
			_, windowEnd := m.desiredTranscriptWindowRange()
			if m.transcript.heights.prefix(slot) >= windowEnd || m.transcript.heights.prefix(slot+1) < windowEnd {
				t.Fatal("fixture does not have header-only window overlap")
			}
			if calls != 1 {
				t.Fatalf("body hydration render calls = %d want 1", calls)
			}
			bodyStart := m.transcript.heights.prefix(slot + 1)
			if action == "scroll" {
				m.SetTranscriptYOffset(bodyStart)
				if got := m.Viewport.GetContent(); !strings.Contains(got, body) {
					t.Fatalf("scrolled body = %q want full body %q", got, body)
				}
			} else {
				if got := m.SelectionPlainText(bodyStart, m.transcript.heights.prefix(endSlot)-1); got != body {
					t.Fatalf("copied body = %q want %q", got, body)
				}
			}
		})
	}
}

func TestRetainedPartialTextDoesNotCountAsHeld(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	m.Viewport.SetHeight(2)
	calls := 0
	deps := retainedTestDeps(&calls)
	m.AppendMessage(Msg{Role: "assistant", Content: "first"})
	m.AppendMessage(Msg{Role: "assistant", Content: "full\nsecond\nthird"})
	for i := 0; i < 10; i++ {
		m.AppendMessage(Msg{Role: "panel", Content: "tail"})
	}
	m.SyncTranscript(deps)
	m.SetTranscriptYOffset(0)
	m.Messages[0] = Msg{Role: "panel", Content: "p"}
	m.MarkMessageDirty(0)
	// 单独刷新用于检查补水前的中间状态，正文元数据不能使消息被标为完整保留。
	m.refreshTranscriptMessage(0, false)
	m.refreshTranscriptMessage(1, false)
	slot, endSlot := m.transcriptMessageRange(1)
	if m.TranscriptBlocks[slot].Text == "" || m.TranscriptBlocks[slot+1].LineCount <= 0 || m.TranscriptBlocks[slot+1].Text != "" {
		t.Fatal("fixture does not contain a rendered header and metadata-only body")
	}
	if m.transcript.held[1] {
		t.Fatal("metadata-only body counted as held")
	}
	// 即使保留标记陈旧，复制正文仍必须按实际缺失文本触发补水。
	m.transcript.held[1] = true
	if got := m.SelectionPlainText(m.transcript.heights.prefix(slot+1), m.transcript.heights.prefix(endSlot)-1); got != "full\nsecond\nthird" {
		t.Fatalf("copied body = %q want full body", got)
	}
}

func TestRetainedOnlyAssistantRoleReplacementClearsResponseNav(t *testing.T) {
	for _, role := range []string{"panel", "user"} {
		t.Run(role, func(t *testing.T) {
			var m Model
			m.InitComponents(ComponentDeps{})
			m.Viewport.SetHeight(2)
			calls := 0
			deps := retainedTestDeps(&calls)
			m.AppendMessage(Msg{Role: "assistant", Content: "first\nsecond\nthird\nfourth"})
			m.SyncTranscript(deps)
			if !m.ResponseNavAvailable || m.LastAssistantLineCount <= m.Viewport.Height() {
				t.Fatal("fixture does not have response navigation")
			}
			m.Messages[0] = Msg{Role: role, Content: "replacement"}
			m.MarkMessageDirty(0)
			m.SyncTranscript(deps)
			if m.ResponseNavAvailable || m.LastAssistantStartLine != 0 || m.LastAssistantLineCount != 0 || m.LastAssistantMsgIndex != -1 {
				t.Fatalf("stale response navigation: available=%t start=%d lines=%d index=%d", m.ResponseNavAvailable, m.LastAssistantStartLine, m.LastAssistantLineCount, m.LastAssistantMsgIndex)
			}
			if m.JumpToLastAssistantStart() {
				t.Fatal("removed assistant remains navigable")
			}
		})
	}
}

func TestTranscriptBlockLineRangeMatchesForwardExtraction(t *testing.T) {
	for _, text := range []string{"", "line", "one\ntwo", "one\ntwo\n", "\n\n", strings.Repeat("long line\n", 100)} {
		block := transcriptBlock{Text: text, LineCount: blockLines(text)}
		for start := 0; start <= block.LineCount; start++ {
			for end := start; end <= block.LineCount; end++ {
				got := transcriptBlockLineRange(block, start, end)
				want := blockLineRange(text, start, end)
				if strings.Join(got, "\n") != strings.Join(want, "\n") || len(got) != len(want) {
					t.Fatalf("range [%d,%d) got %q, want %q", start, end, got, want)
				}
			}
		}
	}
}
