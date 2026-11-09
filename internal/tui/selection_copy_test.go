package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
)

func copyTestTUI(t *testing.T, width int, content string) *TUI {
	t.Helper()
	ui := &TUI{i18n: newTranslator(LocaleZH), mode: uipage.Chat, ready: true, width: width, height: 40}
	ui.initChatComponents()
	ui.chat.AppendMessage(chatpage.Msg{Role: "assistant", Content: content})
	ui.syncContent()
	return ui
}

func copyTestLine(t *testing.T, ui *TUI, contains string) int {
	t.Helper()
	cursor := 0
	for _, block := range ui.chat.TranscriptBlocks {
		if block.LineCount > 0 {
			for i, line := range strings.Split(block.Text, "\n") {
				if strings.Contains(stripANSIForTest(line), contains) {
					return cursor + i
				}
			}
		}
		cursor += block.LineCount
	}
	t.Fatalf("rendered line containing %q not found", contains)
	return -1
}

func copyTestRange(t *testing.T, ui *TUI, start, end int) string {
	t.Helper()
	var copied string
	ui.clipboardWrite = func(value string) error { copied = value; return nil }
	ui.selection.Begin(start, SelectionRegionTranscript)
	ui.selection.Extend(end, ui.chat.TranscriptTotalLines)
	ui.selection.Finish()
	ui.updateChatKey("y", tea.KeyPressMsg(tea.Key{Code: 'y'}))
	if ui.selection.HasAny() || ui.copyFeedbackText == "" {
		t.Fatal("copy did not clear selection and show feedback")
	}
	return copied
}

func TestCopyRenderedSelectionPreservesContentStructure(t *testing.T) {
	for _, tt := range []struct {
		name, content, first, last, want string
	}{
		{
			name:    "code block",
			content: "```python\nif ready:\n    run()\n```",
			first:   "if ready:", last: "run()",
			want: "if ready:\n    run()",
		},
		{
			name:    "nested code only",
			content: "```python\nif ready:\n    if enabled:\n        run()\n```",
			first:   "if enabled:", last: "run()",
			want: "    if enabled:\n        run()",
		},
		{
			name:    "paragraphs",
			content: "First paragraph\n\nSecond paragraph",
			first:   "First paragraph", last: "Second paragraph",
			want: "First paragraph\n\nSecond paragraph",
		},
		{
			name:    "nested list",
			content: "- parent\n  - child\n\nAfter list",
			first:   "parent", last: "After list",
			want: "• parent\n  • child\n\n\nAfter list",
		},
		{
			name:    "quote",
			content: "> quoted\n>\n> next paragraph",
			first:   "quoted", last: "next paragraph",
			want: "│ quoted\n│\n│ next paragraph",
		},
		{
			name:    "rendered emphasis",
			content: "**First** line\nSecond line",
			first:   "First line", last: "Second line",
			want: "First line Second line",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ui := copyTestTUI(t, 80, tt.content)
			start, end := copyTestLine(t, ui, tt.first), copyTestLine(t, ui, tt.last)
			if got := copyTestRange(t, ui, start, end); got != tt.want {
				t.Fatalf("clipboard = %q, want %q", got, tt.want)
			}
			if got := ui.chat.Messages[0].Content; got != tt.content {
				t.Fatalf("message content = %q, want unchanged %q", got, tt.content)
			}
		})
	}
}

func TestCopyRenderedSelectionRetainsCurrentWidthWrapping(t *testing.T) {
	const content = "This is a longer paragraph whose display wrapping should remain in copied text without any terminal margin or right padding."
	var copies []string
	for _, width := range []int{48, 100} {
		ui := copyTestTUI(t, width, content)
		got := copyTestRange(t, ui, 0, ui.chat.TranscriptTotalLines-1)
		if !strings.HasPrefix(got, "● Suna\n\n") || strings.HasSuffix(got, "\n") {
			t.Fatalf("clipboard = %q, want selected header without outer blank lines", got)
		}
		body := strings.TrimPrefix(got, "● Suna\n\n")
		if strings.Join(strings.Split(body, "\n"), " ") != content {
			t.Fatalf("copied body = %q, want only displayed wrapping of %q", body, content)
		}
		for _, line := range strings.Split(body, "\n") {
			if line != strings.TrimSpace(line) {
				t.Fatalf("copied line = %q, want no layout padding", line)
			}
		}
		copies = append(copies, body)
	}
	if copies[0] == copies[1] {
		t.Fatalf("copies = %q, want different wrapping at different widths", copies)
	}
}

func TestCopyInputSelectionPreservesDraftWhitespace(t *testing.T) {
	ui := selectionTestTUI(t)
	ui.chat.Textarea.SetValue("    first  \n\n        second\t")
	// textarea 会在接收草稿时展开 tab；复制必须保持它实际保存的内容。
	draft := ui.chat.Textarea.Value()
	var copied string
	ui.clipboardWrite = func(value string) error { copied = value; return nil }
	ui.selection.Begin(0, SelectionRegionInput)
	ui.selection.Extend(2, 3)
	ui.selection.Finish()
	ui.copySelection()
	if copied != draft {
		t.Fatalf("clipboard = %q, want unchanged draft %q", copied, draft)
	}
}

func TestCopyRenderedTableRetainsCellAlignment(t *testing.T) {
	ui := copyTestTUI(t, 80, "| Name | Value |\n| --- | --- |\n| alpha | 1 |\n| beta | 22 |")
	var displayLines []string
	collecting := false
	for _, block := range ui.chat.TranscriptBlocks {
		if block.MsgIndex != 0 {
			continue
		}
		for _, line := range strings.Split(block.Text, "\n") {
			line = strings.TrimRight(strings.TrimPrefix(stripANSIForTest(line), transcriptBlockIndent), " \t")
			if strings.Contains(line, "Name") {
				collecting = true
			}
			if collecting {
				displayLines = append(displayLines, line)
			}
			if strings.Contains(line, "beta") {
				collecting = false
			}
		}
	}
	want := strings.Join(displayLines, "\n")
	if !strings.Contains(want, "│") || !strings.Contains(want, "Name") || !strings.Contains(want, "alpha") {
		t.Fatalf("rendered table = %q, want visible columns and rows", want)
	}
	start := copyTestLine(t, ui, "Name")
	end := copyTestLine(t, ui, "beta")
	// 实际选区从表头到最后一行，不扩成整条回复，也不返回 Markdown 分隔行。
	if got := copyTestRange(t, ui, start, end); got != want {
		t.Fatalf("clipboard = %q, want rendered table %q", got, want)
	}
}
