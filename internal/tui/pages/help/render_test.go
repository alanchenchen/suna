package help

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestRenderContentIncludesTranscriptNavigationKeys(t *testing.T) {
	deps := RenderDeps{
		Tr:    func(key string) string { return key },
		Brand: lipgloss.NewStyle(),
		HL:    lipgloss.NewStyle(),
		Dim:   lipgloss.NewStyle(),
		Box:   lipgloss.NewStyle(),
	}
	got := RenderContent(nil, deps)
	for _, want := range []string{"Home", "tui.help.chat_response_start", "End", "tui.help.chat_latest"} {
		if !strings.Contains(got, want) {
			t.Fatalf("RenderContent() = %q, want %q", got, want)
		}
	}
}
