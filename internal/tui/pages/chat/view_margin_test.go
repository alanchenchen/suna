package chat

import (
	"strings"
	"testing"
)

func TestViewEndsWithStatusBar(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	view := m.View(ViewDeps{
		Width:          20,
		MiniPet:        "\n\n",
		TopMeta:        "model",
		Conn:           "●",
		Content:        "message",
		Separator:      strings.Repeat("-", 20),
		InputSeparator: "  " + strings.Repeat("-", 16),
		InputArea:      "▌ 输入消息...",
		StatusBar:      "  ctx ?/400k (0%)",
	})
	lines := strings.Split(view, "\n")
	if len(lines) == 0 {
		t.Fatal("View() returned no lines")
	}
	last := lines[len(lines)-1]
	if last != "  ctx ?/400k (0%)" {
		t.Fatalf("last line = %q, want status bar", last)
	}
}

func TestPreInputHintRendersAboveInputSeparator(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	view := m.View(ViewDeps{
		Width:          24,
		MiniPet:        "a\nb\nc",
		TopMeta:        "model",
		Conn:           "●",
		Content:        "message",
		Separator:      strings.Repeat("-", 24),
		InputSeparator: "  " + strings.Repeat("-", 20),
		PreInputHint:   "  可恢复上次会话",
		InputArea:      "  ▌ 输入消息...",
		StatusBar:      "  ctx ?/400k (0%)",
	})
	lines := strings.Split(view, "\n")
	var hintIdx, sepIdx, inputIdx int = -1, -1, -1
	for i, line := range lines {
		switch line {
		case "  可恢复上次会话":
			hintIdx = i
		case "  " + strings.Repeat("-", 20):
			sepIdx = i
		case "  ▌ 输入消息...":
			inputIdx = i
		}
	}
	if hintIdx < 0 || sepIdx < 0 || inputIdx < 0 {
		t.Fatalf("view = %q, want hint, input separator and input area", view)
	}
	if !(hintIdx < sepIdx && sepIdx < inputIdx) {
		t.Fatalf("indexes hint=%d sep=%d input=%d, want hint above separator above input", hintIdx, sepIdx, inputIdx)
	}
}

func TestCommandSuggestionsRenderAboveInputSeparator(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{})
	view := m.View(ViewDeps{
		Width:              32,
		MiniPet:            "a\nb\nc",
		TopMeta:            "model",
		Conn:               "●",
		Content:            "message",
		Separator:          strings.Repeat("-", 32),
		InputSeparator:     "  " + strings.Repeat("-", 28),
		InputArea:          "  ▌ /",
		CommandSuggestions: "  /new\n  /model",
		StatusBar:          "  ctx ?/400k (0%)",
	})
	lines := strings.Split(view, "\n")
	indexes := map[string]int{}
	for i, line := range lines {
		indexes[line] = i
	}
	firstSuggestion := indexes["  /new"]
	lastSuggestion := indexes["  /model"]
	separator := indexes["  "+strings.Repeat("-", 28)]
	input := indexes["  ▌ /"]
	if !(firstSuggestion < lastSuggestion && lastSuggestion < separator && separator < input) {
		t.Fatalf("indexes first=%d last=%d separator=%d input=%d, want suggestions above separator above input; view = %q", firstSuggestion, lastSuggestion, separator, input, view)
	}
}

func TestComputeLayoutMatchesViewRows(t *testing.T) {
	const height = 18
	inputArea := strings.Join([]string{
		"  ▌ 输入消息...",
		"  Enter 发送",
	}, "\n")
	suggestions := strings.Join([]string{
		"╭────╮",
		"│ /new │",
		"│ help │",
		"╰────╯",
	}, "\n")
	preHint := "  可恢复上次会话"

	layout := ComputeLayout(LayoutInput{
		Width:              40,
		Height:             height,
		InputAreaHeight:    RenderedLineCount(inputArea),
		SuggestionHeight:   RenderedLineCount(suggestions),
		PreInputHintHeight: RenderedLineCount(preHint),
	})
	var m Model
	m.InitComponents(ComponentDeps{})
	view := m.View(ViewDeps{
		Width:              40,
		MiniPet:            "a\nb\nc",
		TopMeta:            "model",
		Conn:               "●",
		Content:            strings.TrimRight(strings.Repeat("content\n", layout.ViewportHeight), "\n"),
		Separator:          strings.Repeat("-", 40),
		InputSeparator:     "  " + strings.Repeat("-", 36),
		PreInputHint:       preHint,
		InputArea:          inputArea,
		CommandSuggestions: suggestions,
		StatusBar:          "  ctx ?/400k (0%)",
	})
	if got := len(strings.Split(view, "\n")); got != height {
		t.Fatalf("rendered rows = %d, want %d; view = %q", got, height, view)
	}
}
