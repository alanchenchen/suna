package overlaylist

import (
	"strings"
	"testing"
)

func TestSetItemsUpdatesConfiguredTitleCount(t *testing.T) {
	model := New("skills", []Item{testItem("one")}, testDelegate{}, 40, 10)
	model.SetTitleCount("Skills", " items")
	model.SetItems([]Item{testItem("one"), testItem("two")})

	if got, want := model.List().Title, "Skills · 2 items"; got != want {
		t.Fatalf("Title = %q, want %q", got, want)
	}
}

func TestSetItemsKeepsQuitKeybindingsDisabled(t *testing.T) {
	model := New("skills", nil, testDelegate{}, 40, 10)
	if model.List().KeyMap.Quit.Enabled() || model.List().KeyMap.ForceQuit.Enabled() {
		t.Fatal("quit keybindings must remain disabled for an overlay list")
	}
	model.SetItems([]Item{testItem("one")})
	if model.List().KeyMap.Quit.Enabled() || model.List().KeyMap.ForceQuit.Enabled() {
		t.Fatal("SetItems must not enable quit keybindings")
	}
}

func TestItemCountAndVisibleCount(t *testing.T) {
	model := New("skills", []Item{testItem("webpack"), testItem("release")}, testDelegate{}, 40, 10)
	if got, want := model.ItemCount(), 2; got != want {
		t.Fatalf("ItemCount() = %d, want %d", got, want)
	}
	model.List().SetFilterText("webpack")
	if got, want := model.VisibleCount(), 1; got != want {
		t.Fatalf("VisibleCount() = %d, want %d; items=%s", got, want, strings.Join([]string{"webpack", "release"}, ", "))
	}
}
