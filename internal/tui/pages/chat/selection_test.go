package chat

import (
	"strings"
	"testing"
)

// selectionTestBlocks 构造 3 块 transcript（各 2 行），内容带 ANSI 颜色码验证剥离。
func selectionTestBlocks() Model {
	var m Model
	m.InitComponents(ComponentDeps{})
	m.TranscriptBlocks = []transcriptBlock{
		{MsgIndex: 0, Text: "\x1b[31mline-0-0\x1b[0m\nline-0-1", LineCount: 2},
		{MsgIndex: 1, Text: "line-1-0\nline-1-1", LineCount: 2},
		{MsgIndex: 2, Text: "line-2-0\nline-2-1", LineCount: 2},
	}
	m.TranscriptTotalLines = 6
	return m
}

func TestSelectionPlainTextExtractsRange(t *testing.T) {
	m := selectionTestBlocks()
	got := m.SelectionPlainText(0, 2)
	want := "line-0-0\nline-0-1\nline-1-0"
	if got != want {
		t.Fatalf("SelectionPlainText(0,2) = %q, want %q", got, want)
	}
}

func TestSelectionPlainTextStripsANSI(t *testing.T) {
	m := selectionTestBlocks()
	got := m.SelectionPlainText(0, 0)
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("SelectionPlainText = %q, want ANSI stripped", got)
	}
	if got != "line-0-0" {
		t.Fatalf("SelectionPlainText(0,0) = %q, want line-0-0", got)
	}
}

func TestSelectionPlainTextEmptyRange(t *testing.T) {
	m := selectionTestBlocks()
	if got := m.SelectionPlainText(5, 3); got != "" {
		t.Fatalf("SelectionPlainText(5,3) = %q, want empty (end < start)", got)
	}
	if got := m.SelectionPlainText(0, -1); got != "" {
		t.Fatalf("SelectionPlainText(0,-1) = %q, want empty", got)
	}
}

func TestSelectionPlainTextClampsOutOfRange(t *testing.T) {
	m := selectionTestBlocks()
	got := m.SelectionPlainText(0, 999)
	if !strings.HasPrefix(got, "line-0-0") || !strings.HasSuffix(got, "line-2-1") {
		t.Fatalf("SelectionPlainText(0,999) = %q, want clamped to last line", got)
	}
}

func TestSelectionPlainTextEmptyTranscript(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	if got := m.SelectionPlainText(0, 5); got != "" {
		t.Fatalf("SelectionPlainText on empty transcript = %q, want empty", got)
	}
}

func TestSelectionPlainTextSingleBlockRange(t *testing.T) {
	m := selectionTestBlocks()
	got := m.SelectionPlainText(1, 1)
	if got != "line-0-1" {
		t.Fatalf("SelectionPlainText(1,1) = %q, want line-0-1", got)
	}
	got = m.SelectionPlainText(2, 3)
	if got != "line-1-0\nline-1-1" {
		t.Fatalf("SelectionPlainText(2,3) = %q, want block 1 both lines", got)
	}
}
