package chat

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"

	"github.com/alanchenchen/suna/internal/protocol"
)

func TestSkillItemKeyIncludesScopeAndPath(t *testing.T) {
	global := skillItem{skill: protocol.SkillInfo{Name: "writer", Scope: "global"}}
	projectA := skillItem{skill: protocol.SkillInfo{Name: "writer", Scope: "project", Path: "/repo/a/writer"}}
	projectB := skillItem{skill: protocol.SkillInfo{Name: "writer", Scope: "project", Path: "/repo/b/writer"}}
	if global.Key() == projectA.Key() || projectA.Key() == projectB.Key() {
		t.Fatalf("Skill keys must distinguish scope and exact project path: %q %q %q", global.Key(), projectA.Key(), projectB.Key())
	}
}

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

func TestNativeDelegateKeepsProjectPathBeforeDescription(t *testing.T) {
	delegate := nativeDelegate{styles: ListStyles{}, text: testListText()}
	view := delegate.renderItem(54, false, skillItem{skill: protocol.SkillInfo{Name: "writer", Scope: "project", Path: "/repo/.agents/skills/writer", Description: "A deliberately long description", Valid: true, Enabled: true}})
	if !strings.Contains(view, "/repo/.agents/skills/writer") {
		t.Fatalf("project Skill row = %q, want exact path visible before truncation", view)
	}
}

func TestNativeDelegateRendersSkillScopeBadgeWithStatusOnOneLine(t *testing.T) {
	tests := []struct {
		name      string
		skill     protocol.SkillInfo
		wantBadge string
		wantMark  string
	}{
		{name: "global active", skill: protocol.SkillInfo{Name: "global-skill", Scope: "global", Valid: true, Enabled: true, CanToggle: true}, wantBadge: "[GLOBAL]", wantMark: "✓"},
		{name: "project issue", skill: protocol.SkillInfo{Name: "project-skill", Scope: "project", Valid: false}, wantBadge: "[PROJECT]", wantMark: "!"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delegate := nativeDelegate{styles: ListStyles{}, text: testListText()}
			view := delegate.renderItem(80, true, skillItem{skill: tt.skill})
			for _, want := range []string{tt.wantBadge, tt.wantMark, tt.skill.Name} {
				if !strings.Contains(view, want) {
					t.Fatalf("skill row = %q, want %q", view, want)
				}
			}
			if strings.Contains(view, "\n") {
				t.Fatalf("skill row = %q, want one line", view)
			}
			if got := lipgloss.Width(view); got > 80 {
				t.Fatalf("skill row width = %d, want <= 80; row=%q", got, view)
			}
		})
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
	if got, want := strings.Join(keys.PrevPage.Keys(), ","), "pgup"; got != want {
		t.Fatalf("PrevPage keys = %q, want %q", got, want)
	}
	if got, want := strings.Join(keys.NextPage.Keys(), ","), "pgdown"; got != want {
		t.Fatalf("NextPage keys = %q, want %q", got, want)
	}
	for name, binding := range map[string]key.Binding{
		"GoToStart": keys.GoToStart,
		"GoToEnd":   keys.GoToEnd, "ShowFullHelp": keys.ShowFullHelp, "CloseFullHelp": keys.CloseFullHelp,
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
