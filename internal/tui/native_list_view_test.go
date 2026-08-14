package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	"charm.land/lipgloss/v2"

	"github.com/alanchenchen/suna/internal/protocol"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
)

func TestNativeListHeaderShowsFilteredCountWhileEditing(t *testing.T) {
	tui := New(LocaleEN)
	tui.chat.SetSkills([]protocol.SkillInfo{{Name: "webpack", Valid: true}, {Name: "release", Valid: true}})
	tui.chat.InitNativeLists(false, tui.nativeListStyles(), tui.nativeListText())
	listModel := tui.chat.SkillsList.List()
	listModel.SetSize(60, 2)
	listModel.SetFilterText("webpack")
	listModel.SetFilterState(list.Filtering)

	view := tui.nativeListHeader(tui.chat.SkillsList)
	for _, want := range []string{"Filter:", "1 / 2 items"} {
		if !strings.Contains(view, want) {
			t.Fatalf("header = %q, want %q", view, want)
		}
	}
}

func TestNativeListHeaderDoesNotAddEllipsisWhileFiltering(t *testing.T) {
	tui := New(LocaleEN)
	tui.chat.SetSkills([]protocol.SkillInfo{{Name: "webpack", Valid: true}})
	tui.chat.InitNativeLists(false, tui.nativeListStyles(), tui.nativeListText())
	listModel := tui.chat.SkillsList.List()
	listModel.SetSize(28, 1)
	listModel.SetFilterText("a-long-filter-value")
	listModel.SetFilterState(list.Filtering)

	view := tui.nativeListHeader(tui.chat.SkillsList)
	if strings.Contains(view, "…") {
		t.Fatalf("header = %q, must not add ellipsis while filtering", view)
	}
	if !strings.Contains(view, "0 / 1 items") {
		t.Fatalf("header = %q, want filtered count", view)
	}
}

func TestNativeListOverlayKeepsChromeForNoMatches(t *testing.T) {
	tui := New(LocaleEN)
	tui.width = 100
	tui.height = 30
	tui.chat.SetSkills([]protocol.SkillInfo{{Name: "webpack", Valid: true}})
	tui.chat.InitNativeLists(false, tui.nativeListStyles(), tui.nativeListText())
	listModel := tui.chat.SkillsList.List()
	listModel.SetFilterText("missing")
	listModel.SetFilterState(list.Filtering)

	view := tui.renderNativeListOverlay(chatpage.NativeListSkills, &tui.chat.SkillsList, tui.width, tui.nativeListText().Toggle, "tui.skills.empty", "", "")
	for _, want := range []string{"Filter:", "0 / 1 items", "No matching items", "↑↓"} {
		if !strings.Contains(view, want) {
			t.Fatalf("overlay = %q, want %q", view, want)
		}
	}
}

func TestSkillsOverlayActionTextFollowsSelectedScopeInNormalAndFilteringModes(t *testing.T) {

	tui := New(LocaleEN)
	tui.width = 100
	tui.height = 30
	tui.chat.SetSkills([]protocol.SkillInfo{
		{Name: "global-skill", Scope: "global", Valid: true, CanToggle: true},
		{Name: "project-skill", Scope: "project", Valid: true, CanToggle: true},
	})
	tui.chat.InitNativeLists(false, tui.nativeListStyles(), tui.nativeListText())

	globalView := stripANSIForTest(tui.renderSkillsOverlay(tui.width))
	if !strings.Contains(globalView, "Enter/Space toggle") {
		t.Fatalf("global footer = %q, want toggle action", globalView)
	}

	tui.chat.SkillsList.List().CursorDown()
	projectView := stripANSIForTest(tui.renderSkillsOverlay(tui.width))
	if !strings.Contains(projectView, "project-managed") {
		t.Fatalf("project footer = %q, want project-managed action", projectView)
	}
	if strings.Contains(projectView, "Enter project-managed") || strings.Contains(projectView, "Enter/Space project-managed") {
		t.Fatalf("project footer = %q, must not advertise an actionable key", projectView)
	}

	listModel := tui.chat.SkillsList.List()
	listModel.SetFilterText("global-skill")
	listModel.SetFilterState(list.Filtering)
	globalFilteringView := stripANSIForTest(tui.renderSkillsOverlay(tui.width))
	if !strings.Contains(globalFilteringView, "Enter toggle") {
		t.Fatalf("global filtering footer = %q, want toggle action", globalFilteringView)
	}

	listModel.SetFilterText("project-skill")
	projectFilteringView := stripANSIForTest(tui.renderSkillsOverlay(tui.width))
	if !strings.Contains(projectFilteringView, "project-managed") {
		t.Fatalf("project filtering footer = %q, want project-managed action", projectFilteringView)
	}
	if strings.Contains(projectFilteringView, "Enter project-managed") || strings.Contains(projectFilteringView, "Enter/Space project-managed") {
		t.Fatalf("project filtering footer = %q, must not advertise an actionable key", projectFilteringView)
	}
}

func TestNativeListOverlayShrinksToContent(t *testing.T) {
	tui := New(LocaleEN)
	tui.width = 100
	tui.height = 30
	tui.chat.SetSkills([]protocol.SkillInfo{{Name: "webpack", Description: "organize notes", Valid: true}})
	tui.chat.InitNativeLists(false, tui.nativeListStyles(), tui.nativeListText())

	view := tui.renderNativeListOverlay(chatpage.NativeListSkills, &tui.chat.SkillsList, tui.width, tui.nativeListText().Toggle, "tui.skills.empty", "", "")
	if got, wantMax := lipgloss.Height(view), 10; got > wantMax {
		t.Fatalf("overlay height = %d, want <= %d; view=%q", got, wantMax, view)
	}
}

func TestNativeListOverlayUsesCurrentBubblesPage(t *testing.T) {
	tui := New(LocaleEN)
	tui.width = 100
	tui.height = 30
	skills := make([]protocol.SkillInfo, 0, 12)
	for i := 0; i < 12; i++ {
		skills = append(skills, protocol.SkillInfo{Name: "skill-" + string(rune('a'+i)), Valid: true})
	}
	tui.chat.SetSkills(skills)
	tui.chat.InitNativeLists(false, tui.nativeListStyles(), tui.nativeListText())
	listModel := tui.chat.SkillsList.List()
	listModel.SetSize(60, 3)
	for i := 0; i < 4; i++ {
		listModel.CursorDown()
	}

	rows := tui.chat.NativeListRows(chatpage.NativeListSkills, tui.nativeListStyles(), tui.nativeListText(), 60)
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "skill-e") {
		t.Fatalf("rows = %q, want current Bubbles page item", joined)
	}
	if !strings.Contains(joined, "▎") {
		t.Fatalf("rows = %q, want selected shared rail", joined)
	}
}

func TestNativeListWidthNeverExceedsViewportContent(t *testing.T) {
	for _, viewport := range []int{1, 5, 12, 40, 120} {
		if got, wantMax := nativeListWidth(viewport, 82), max(1, viewport-6); got > wantMax {
			t.Fatalf("nativeListWidth(%d, 82) = %d, want <= %d", viewport, got, wantMax)
		}
	}
}
