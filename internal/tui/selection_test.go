package tui

import "testing"

func TestSelectionBeginSetsAnchorAndActive(t *testing.T) {
	var s Selection
	s.Begin(10, SelectionRegionTranscript)
	if !s.Active {
		t.Fatal("Begin: Active = false, want true")
	}
	if s.AnchorLine != 10 || s.EndLine != 10 {
		t.Fatalf("Begin: anchor/end = %d/%d, want 10/10", s.AnchorLine, s.EndLine)
	}
	if s.HasSelection {
		t.Fatal("Begin: HasSelection = true, want false (拖动中未定格)")
	}
}

func TestSelectionExtendUpdatesEndLine(t *testing.T) {
	var s Selection
	s.Begin(10, SelectionRegionTranscript)
	s.Extend(15, 100)
	if s.EndLine != 15 {
		t.Fatalf("Extend: EndLine = %d, want 15", s.EndLine)
	}
	// 继续扩展
	s.Extend(20, 100)
	if s.EndLine != 20 {
		t.Fatalf("Extend: EndLine = %d, want 20", s.EndLine)
	}
}

func TestSelectionExtendClampsOutOfRange(t *testing.T) {
	var s Selection
	s.Begin(5, SelectionRegionTranscript)
	s.Extend(-3, 100)
	if s.EndLine != 0 {
		t.Fatalf("Extend(-3): EndLine = %d, want 0", s.EndLine)
	}
	s.Extend(999, 100)
	if s.EndLine != 99 {
		t.Fatalf("Extend(999): EndLine = %d, want 99", s.EndLine)
	}
}

func TestSelectionExtendIgnoredWhenNotActive(t *testing.T) {
	var s Selection
	// 未 Begin 时 Extend 应被忽略
	s.Extend(5, 100)
	if s.HasAny() {
		t.Fatal("Extend without Begin: HasAny = true, want false")
	}
}

func TestSelectionFinishStopsDraggingAndKeepsSelection(t *testing.T) {
	var s Selection
	s.Begin(10, SelectionRegionTranscript)
	s.Extend(12, 100)
	s.Finish()
	if s.Active {
		t.Fatal("Finish: Active = true, want false")
	}
	if !s.HasSelection {
		t.Fatal("Finish: HasSelection = false, want true")
	}
}

func TestSelectionClearResetsAll(t *testing.T) {
	var s Selection
	s.Begin(10, SelectionRegionTranscript)
	s.Extend(12, 100)
	s.Finish()
	s.Clear()
	if s.HasAny() {
		t.Fatal("Clear: HasAny = true, want false")
	}
	if s.AnchorLine != 0 || s.EndLine != 0 {
		t.Fatalf("Clear: anchor/end = %d/%d, want 0/0", s.AnchorLine, s.EndLine)
	}
}

func TestSelectionLineRangeOrdersReverseDrag(t *testing.T) {
	var s Selection
	s.Begin(20, SelectionRegionTranscript)
	s.Extend(5, 100)
	start, end := s.LineRange()
	if start != 5 || end != 20 {
		t.Fatalf("LineRange(reverse) = %d..%d, want 5..20", start, end)
	}
}

func TestSelectionLineRangeEmptyWithoutSelection(t *testing.T) {
	var s Selection
	start, end := s.LineRange()
	if start != 0 || end != -1 {
		t.Fatalf("LineRange(empty) = %d..%d, want 0..-1", start, end)
	}
}

func TestSelectionContains(t *testing.T) {
	var s Selection
	s.Begin(10, SelectionRegionTranscript)
	s.Extend(12, 100)
	for _, tc := range []struct {
		line int
		want bool
	}{{9, false}, {10, true}, {11, true}, {12, true}, {13, false}} {
		if got := s.Contains(tc.line); got != tc.want {
			t.Fatalf("Contains(%d) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

func TestSelectionContainsReverseDrag(t *testing.T) {
	var s Selection
	s.Begin(12, SelectionRegionTranscript)
	s.Extend(10, 100)
	if !s.Contains(10) || !s.Contains(11) || !s.Contains(12) {
		t.Fatal("Contains(reverse): 10..12 should be selected")
	}
	if s.Contains(9) || s.Contains(13) {
		t.Fatal("Contains(reverse): out-of-range line selected")
	}
}
