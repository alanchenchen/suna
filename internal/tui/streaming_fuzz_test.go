package tui

import (
	"strings"
	"testing"
)

func FuzzAppendStreamingDeltaMatchesFullRender(f *testing.F) {
	f.Add("hello world", byte(3), 20)
	f.Add("line1\nline2\n", byte(2), 12)
	f.Add("中文🙂emoji and ascii", byte(5), 16)
	f.Add(strings.Repeat("x", 256), byte(7), 40)
	f.Fuzz(func(t *testing.T, text string, splitByte byte, width int) {
		if width < 1 {
			width = 1
		}
		if width > 120 {
			width = 120
		}
		step := int(splitByte%16) + 1
		lines := []string{""}
		lastWidth := 0
		pendingNewlines := 0
		for start := 0; start < len(text); {
			end := start + step
			if end > len(text) {
				end = len(text)
			}
			appendStreamingDelta(&lines, &lastWidth, &pendingNewlines, text[start:end], width)
			start = end
		}
		got := strings.Join(lines, "\n")
		want := renderStreamingText(text, width)
		if got != want {
			t.Fatalf("incremental render mismatch\ngot:  %q\nwant: %q", got, want)
		}
	})
}
