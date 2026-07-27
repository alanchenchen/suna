package chat

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"

	"github.com/alanchenchen/suna/internal/protocol"
)

func TestSkillItemFilterValueUsesNameAndDescriptionOnly(t *testing.T) {
	item := skillItem{skill: protocol.SkillInfo{
		Name:        "webpack-notes",
		Description: "Organize build knowledge",
		Path:        "/private/notes",
		Reasons:     []string{"invalid webpack reference"},
		Error:       "webpack error",
	}}

	got := item.FilterValue()
	for _, want := range []string{"webpack-notes", "Organize build knowledge"} {
		if !strings.Contains(got, want) {
			t.Fatalf("FilterValue() = %q, want %q", got, want)
		}
	}
	for _, unwanted := range []string{"/private/notes", "invalid webpack reference", "webpack error"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("FilterValue() = %q, must not contain %q", got, unwanted)
		}
	}
}

func TestMCPItemFilterValueUsesNameAndTransportOnly(t *testing.T) {
	item := mcpItem{server: protocol.MCPServerInfo{
		Name:      "workspace-files",
		Transport: "stdio",
		Command:   "private-command --webpack",
		Error:     "webpack connection error",
	}}

	got := item.FilterValue()
	for _, want := range []string{"workspace-files", "stdio"} {
		if !strings.Contains(got, want) {
			t.Fatalf("FilterValue() = %q, want %q", got, want)
		}
	}
	for _, unwanted := range []string{"private-command", "webpack connection error"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("FilterValue() = %q, must not contain %q", got, unwanted)
		}
	}
}

func TestNativeListUsesSelectionRail(t *testing.T) {
	m := Model{}
	m.SetSkills([]protocol.SkillInfo{{Name: "first", Description: "first skill", Valid: true}})
	m.InitNativeLists(false, ListStyles{}, testListText())
	m.SkillsList.List().SetSize(48, 4)
	view := strings.Join(m.NativeListRows(NativeListSkills, ListStyles{Cursor: lipgloss.NewStyle()}, testListText(), 48), "\n")
	if !strings.Contains(view, "▎") {
		t.Fatalf("list view = %q, want shared selection rail", view)
	}
	if strings.Contains(view, "▶") {
		t.Fatalf("list view = %q, must not use legacy arrow cursor", view)
	}
}

func TestNativeDelegateUsesCompactSingleLineAndVisibleActiveState(t *testing.T) {
	m := Model{}
	m.SetSkills([]protocol.SkillInfo{{Name: "active-skill", Description: "long description", Valid: true, Enabled: true}})
	m.InitNativeLists(false, ListStyles{OK: lipgloss.NewStyle().Bold(true)}, testListText())
	m.SkillsList.List().SetSize(60, 4)
	view := strings.Join(m.NativeListRows(NativeListSkills, ListStyles{OK: lipgloss.NewStyle().Bold(true)}, testListText(), 60), "\n")
	if !strings.Contains(view, "✓") {
		t.Fatalf("active skill view = %q, want visible enabled mark", view)
	}
	if strings.Contains(view, "\x1b[1mactive\x1b") {
		t.Fatalf("active skill view = %q, must not append redundant active label", view)
	}
	if strings.Count(strings.TrimSpace(view), "\n") != 0 {
		t.Fatalf("list row = %q, want one compact line", view)
	}
}

func TestNativeDelegateKeepsLineWithinAvailableWidth(t *testing.T) {
	delegate := nativeDelegate{styles: ListStyles{}, text: testListText()}
	line := delegate.renderLine(20, "▎ ", "✓", lipgloss.NewStyle(), "very-long-skill-name", lipgloss.NewStyle(), "a very long description")
	if got := lipgloss.Width(line); got > 20 {
		t.Fatalf("rendered line width = %d, want <= 20; line=%q", got, line)
	}
}

func TestNativeListKeyMapUsesArrowNavigationOnly(t *testing.T) {
	keys := nativeListKeyMap(testListText())
	if got, want := strings.Join(keys.CursorUp.Keys(), ","), "up"; got != want {
		t.Fatalf("CursorUp keys = %q, want %q", got, want)
	}
	if got, want := strings.Join(keys.CursorDown.Keys(), ","), "down"; got != want {
		t.Fatalf("CursorDown keys = %q, want %q", got, want)
	}
	for name, binding := range map[string]key.Binding{
		"PrevPage": keys.PrevPage, "NextPage": keys.NextPage, "GoToStart": keys.GoToStart,
		"GoToEnd": keys.GoToEnd, "ShowFullHelp": keys.ShowFullHelp, "CloseFullHelp": keys.CloseFullHelp,
	} {
		if binding.Enabled() {
			t.Fatalf("%s must be disabled", name)
		}
	}
	if keys.AcceptWhileFiltering.Enabled() {
		t.Fatal("AcceptWhileFiltering must be disabled for immediate filter actions")
	}
}

func TestNativeListFilterUpdatesTitleWithVisibleAndTotalCounts(t *testing.T) {
	m := Model{}
	m.SetSkills([]protocol.SkillInfo{{Name: "webpack", Valid: true}, {Name: "release", Valid: true}})
	m.InitNativeLists(false, ListStyles{}, testListText())
	m.SkillsList.List().SetFilterText("webpack")
	m.SkillsList.SetTitleCount("Skills", " items")
	_ = m.NativeListRows(NativeListSkills, ListStyles{}, testListText(), 60)

	if got, want := m.SkillsList.CountText(), "1 / 2 items"; got != want {
		t.Fatalf("CountText() = %q, want %q", got, want)
	}
	if got, want := m.SkillsList.List().Title, "Skills · 1 / 2 items"; got != want {
		t.Fatalf("title = %q, want %q", got, want)
	}
}
