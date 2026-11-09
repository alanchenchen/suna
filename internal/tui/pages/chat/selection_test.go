package chat

import (
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/tui/components/toolview"
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

func TestSelectionPlainTextCleansOnlyLayoutWhitespace(t *testing.T) {
	for _, tt := range []struct {
		name, rendered, want string
		start, end           int
	}{
		{"nested code only", "  if ready:    \n      if enabled:   \n          run()    ", "    if enabled:\n        run()", 1, 2},
		{"blank rows", "      \n  first    \n \t \n  second\t \n      ", "first\n\nsecond", 0, 4},
		{"list levels", "  • parent     \n    • child    \n      continued    ", "• parent\n  • child\n    continued", 0, 2},
		{"table alignment", "  Name    │ Value   \n  alpha   │ 1       ", "Name    │ Value\nalpha   │ 1", 0, 1},
		{"box alignment", "  ╭──────────╮   \n  │  value   │   \n  ╰──────────╯   ", "╭──────────╮\n│  value   │\n╰──────────╯", 0, 2},
		{"display wrapping", "  first part    \n  second part    ", "first part\nsecond part", 0, 1},
		{"ANSI and Unicode", "  \x1b[31m你好 🌍\x1b[0m    \n      内容    ", "你好 🌍\n    内容", 0, 1},
		{"no outer indent", "text  \n one space   ", "text\n one space", 0, 1},
		{"all whitespace", "    \n\t   ", "", 0, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var m Model
			m.InitComponents(ComponentDeps{})
			m.transcript.deps.CopyIndent = "  "
			m.TranscriptBlocks = []transcriptBlock{{MsgIndex: 0, Text: tt.rendered, LineCount: blockLines(tt.rendered)}}
			m.TranscriptTotalLines = blockLines(tt.rendered)
			if got := m.SelectionPlainText(tt.start, tt.end); got != tt.want {
				t.Fatalf("SelectionPlainText = %q, want %q", got, tt.want)
			}
			if got := m.TranscriptBlocks[0].Text; got != tt.rendered {
				t.Fatalf("rendered block = %q, want unchanged %q", got, tt.rendered)
			}
		})
	}
}

func TestSelectionPlainTextPreservesPanelIndentAcrossBlocks(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	m.Messages = []Msg{{Role: "assistant"}, {Role: "panel"}, {Role: "assistant"}}
	m.transcript.deps.CopyIndent = "  "
	for i, text := range []string{"  first   \n     ", "    indented panel   ", "      nested code   \n  last   "} {
		m.TranscriptBlocks = append(m.TranscriptBlocks, transcriptBlock{MsgIndex: int32(i), Text: text, LineCount: blockLines(text)})
		m.TranscriptTotalLines += blockLines(text)
	}
	const want = "first\n\n    indented panel\n    nested code\nlast"
	if got := m.SelectionPlainText(0, m.TranscriptTotalLines-1); got != want {
		t.Fatalf("SelectionPlainText = %q, want %q", got, want)
	}
}

func TestSelectionPlainTextCleansHydratedHistoryWithoutChangingViewport(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	m.Viewport.SetHeight(3)
	calls := 0
	deps := retainedTestDeps(&calls)
	deps.CopyIndent = "  "
	deps.RenderSunaHeader = func(string) string { return "\n  header\n" }
	render := deps.RenderAssistant
	deps.RenderAssistant = func(msg *Msg) string {
		lines := strings.Split(render(msg), "\n")
		for i := range lines {
			lines[i] = "  " + lines[i] + "    "
		}
		return strings.Join(lines, "\n")
	}
	m.AppendMessage(Msg{Role: "assistant", Content: "if ready:\n    run()"})
	for i := 0; i < 100; i++ {
		m.AppendMessage(Msg{Role: "assistant", Content: "tail"})
	}
	m.SyncTranscript(deps)
	slot, endSlot := m.transcriptMessageRange(0)
	if m.TranscriptBlocks[endSlot-1].Text != "" {
		t.Fatal("fixture kept old body rendered, want offscreen hydration")
	}
	offset, visible := m.TranscriptYOffset, m.Viewport.GetContent()
	start := m.transcript.heights.prefix(slot + 1)
	end := m.transcript.heights.prefix(endSlot) - 1
	if got := m.SelectionPlainText(start, end); got != "if ready:\n    run()" {
		t.Fatalf("copied history = %q, want code without layout padding", got)
	}
	if m.TranscriptYOffset != offset || m.Viewport.GetContent() != visible {
		t.Fatal("copy changed viewport")
	}
	if m.TranscriptBlocks[endSlot-1].Text != "" {
		t.Fatal("copy retained offscreen rendered text")
	}
}

func TestSelectionPlainTextPreservesToolBoxAcrossMessages(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	calls := 0
	deps := retainedTestDeps(&calls)
	deps.CopyIndent = "  "
	deps.RenderSunaHeader = nil
	deps.RenderAssistant = func(msg *Msg) string { return "  " + msg.Content.(string) + "   " }
	deps.RenderToolBlock = func(*toolview.Block) string {
		return "  ╭─────────╮\n  │  done   │\n  ╰─────────╯"
	}
	deps.RenderSubtaskBlock = nil
	m.AppendMessage(Msg{Role: "assistant", Content: "before"})
	block := m.EnsureToolBlock()
	block.Add(&toolview.Entry{ID: "tool", Name: "readfile", Status: toolview.StatusDone})
	m.AppendMessage(Msg{Role: "assistant", Content: "after"})
	m.SyncTranscript(deps)
	const want = "before\n\n╭─────────╮\n│  done   │\n╰─────────╯\nafter"
	if got := m.SelectionPlainText(0, m.TranscriptTotalLines-1); got != want {
		t.Fatalf("SelectionPlainText = %q, want %q", got, want)
	}
}
