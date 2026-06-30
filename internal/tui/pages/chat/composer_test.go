package chat

import (
	"testing"
	"time"
)

func TestSetStatusLabelKeepsPhaseStartForSameLoadingLabel(t *testing.T) {
	var m Model
	first := time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC)
	second := first.Add(5 * time.Second)

	m.SetStatusLabel("waiting", first)
	m.SetStatusLabel("waiting", second)

	if m.PhaseStart != first {
		t.Fatalf("PhaseStart = %v, want %v", m.PhaseStart, first)
	}
}

func TestSetStatusLabelResetsPhaseStartForNewLabel(t *testing.T) {
	var m Model
	first := time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC)
	second := first.Add(5 * time.Second)

	m.SetStatusLabel("waiting", first)
	m.SetStatusLabel("reviewing", second)

	if m.PhaseStart != second {
		t.Fatalf("PhaseStart = %v, want %v", m.PhaseStart, second)
	}
	if m.StatusLabel != "reviewing" {
		t.Fatalf("StatusLabel = %q, want reviewing", m.StatusLabel)
	}
}
