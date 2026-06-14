package chat

import (
	"fmt"
	"strings"
	"testing"
)

func TestSyncTranscriptUsesWindowedViewportContent(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	m.Viewport.SetHeight(5)
	m.Viewport.SetWidth(80)
	for i := 0; i < 100; i++ {
		m.AppendMessage(Msg{Role: "panel", Content: fmt.Sprintf("line-%03d", i)})
	}

	m.SyncTranscript(TranscriptDeps{Width: 80})

	if got, wantMax := len(strings.Split(m.Viewport.GetContent(), "\n")), 15; got > wantMax {
		t.Fatalf("viewport content lines = %d, want at most %d", got, wantMax)
	}
	if m.TranscriptTotalLines <= len(strings.Split(m.Viewport.GetContent(), "\n")) {
		t.Fatalf("total lines = %d should exceed window content", m.TranscriptTotalLines)
	}
}

func TestSetTranscriptYOffsetSkipsUnchangedWindow(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	m.Viewport.SetHeight(5)
	m.Viewport.SetWidth(80)
	for i := 0; i < 20; i++ {
		m.AppendMessage(Msg{Role: "panel", Content: fmt.Sprintf("line-%03d", i)})
	}
	m.SyncTranscript(TranscriptDeps{Width: 80})
	sig := m.TranscriptWindowSignature
	content := m.Viewport.GetContent()

	m.SetTranscriptYOffset(m.TranscriptYOffset)

	if m.TranscriptWindowSignature != sig {
		t.Fatalf("window signature changed on unchanged offset: got %+v want %+v", m.TranscriptWindowSignature, sig)
	}
	if got := m.Viewport.GetContent(); got != content {
		t.Fatalf("viewport content changed on unchanged offset")
	}
}

func TestTrimMarkdownRenderCacheKeepsVisibleAndRecent(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	m.Viewport.SetHeight(4)
	m.Viewport.SetWidth(80)
	large := strings.Repeat("x", 2*1024*1024)
	for i := 0; i < 20; i++ {
		m.Messages = append(m.Messages, Msg{
			Role:    "assistant",
			Content: fmt.Sprintf("message-%02d", i),
			Render:  MsgRenderCache{Output: large},
		})
		m.TranscriptBlocks = append(m.TranscriptBlocks, transcriptBlock{MsgIndex: i, Text: fmt.Sprintf("message-%02d", i), LineCount: 1})
	}
	m.recomputeTranscriptLayout()
	m.SetTranscriptYOffset(10)

	m.trimMarkdownRenderCache()

	if m.Messages[10].Render.Output == "" {
		t.Fatalf("visible message cache was trimmed")
	}
	for i := 14; i < 20; i++ {
		if m.Messages[i].Render.Output == "" {
			t.Fatalf("recent message %d cache was trimmed", i)
		}
	}
	trimmed := false
	for i := 0; i < 10; i++ {
		if m.Messages[i].Render.Output == "" {
			trimmed = true
			break
		}
	}
	if !trimmed {
		t.Fatalf("old offscreen caches were not trimmed")
	}
}

func TestRenderTranscriptReturnsFullTextWhenCacheWasTrimmed(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	m.Viewport.SetHeight(2)
	m.Viewport.SetWidth(80)
	m.AppendMessage(Msg{
		Role:    "assistant",
		Content: "old cached answer",
		Render:  MsgRenderCache{Width: 72, Theme: "test", LineCount: 1},
	})

	view := m.RenderTranscript(TranscriptDeps{
		Width:         80,
		MarkdownWidth: 72,
		Theme:         "test",
		RenderAssistant: func(msg *Msg) string {
			content, _ := msg.Content.(string)
			return content
		},
	})

	if !strings.Contains(view, "old cached answer") {
		t.Fatalf("RenderTranscript() = %q, want full assistant content", view)
	}
}

func TestScrollTranscriptReusesContentWithinOverscanWindow(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	m.Viewport.SetHeight(5)
	m.Viewport.SetWidth(80)
	for i := 0; i < 40; i++ {
		m.AppendMessage(Msg{Role: "panel", Content: fmt.Sprintf("line-%03d", i)})
	}
	m.SyncTranscript(TranscriptDeps{Width: 80})
	m.SetTranscriptYOffset(10)
	content := m.Viewport.GetContent()
	sig := m.TranscriptWindowSignature
	oldViewportOffset := m.Viewport.YOffset()

	m.ScrollTranscript(1)

	if m.TranscriptWindowSignature != sig {
		t.Fatalf("window content signature changed within overscan window: got %+v want %+v", m.TranscriptWindowSignature, sig)
	}
	if got := m.Viewport.GetContent(); got != content {
		t.Fatalf("viewport content changed within overscan window")
	}
	if got, want := m.Viewport.YOffset(), oldViewportOffset+1; got != want {
		t.Fatalf("viewport offset = %d, want %d", got, want)
	}
}
