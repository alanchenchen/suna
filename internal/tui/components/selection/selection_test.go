package selection

import (
	"testing"

	"charm.land/lipgloss/v2"
)

func TestRailKeepsFixedWidth(t *testing.T) {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	for _, selected := range []bool{false, true} {
		if got, want := lipgloss.Width(Rail(selected, 2, style)), 4; got != want {
			t.Fatalf("Rail(%v) width = %d, want %d", selected, got, want)
		}
	}
}
