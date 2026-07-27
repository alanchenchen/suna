package overlaylist

import (
	"io"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

type testItem string

func (i testItem) Key() string         { return string(i) }
func (i testItem) FilterValue() string { return string(i) }

type testDelegate struct{}

func (testDelegate) Height() int                         { return 1 }
func (testDelegate) Spacing() int                        { return 0 }
func (testDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }
func (testDelegate) Render(_ io.Writer, _ list.Model, _ int, _ list.Item) {
}

func TestWrapPreservesOwnerForFilterResults(t *testing.T) {
	model := New("skills", []Item{testItem("webpack")}, testDelegate{}, 40, 10)
	cmd := model.Update(tea.KeyPressMsg{Text: "/"})
	if cmd == nil {
		t.Fatal("Update returned nil command")
	}
	msg := cmd()
	wrapped, ok := msg.(Message)
	if !ok {
		t.Fatalf("message type = %T, want overlaylist.Message", msg)
	}
	if got, want := wrapped.Owner, "skills"; got != want {
		t.Fatalf("owner = %q, want %q", got, want)
	}
}
