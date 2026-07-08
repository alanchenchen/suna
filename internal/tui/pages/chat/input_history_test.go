package chat

import "testing"

func TestBrowseInputHistoryUsesUserMessagesWithoutStorage(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{Placeholder: "message"})
	m.Messages = []Msg{
		{Role: "system", Content: "ignored"},
		{Role: "user", Content: UserMessageContent{Text: "first"}},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "second"},
	}

	if ok := m.BrowseInputHistory(-1); !ok {
		t.Fatal("BrowseInputHistory(-1) = false, want true")
	}
	if got, want := m.Textarea.Value(), "second"; got != want {
		t.Fatalf("textarea = %q, want %q", got, want)
	}
	if ok := m.BrowseInputHistory(-1); !ok {
		t.Fatal("BrowseInputHistory(-1) second = false, want true")
	}
	if got, want := m.Textarea.Value(), "first"; got != want {
		t.Fatalf("textarea = %q, want %q", got, want)
	}
	if ok := m.BrowseInputHistory(1); !ok {
		t.Fatal("BrowseInputHistory(1) = false, want true")
	}
	if got, want := m.Textarea.Value(), "second"; got != want {
		t.Fatalf("textarea = %q, want %q", got, want)
	}
}

func TestBrowseInputHistoryDoesNotStealNonEmptyDraft(t *testing.T) {
	var m Model
	m.InitComponents(ComponentDeps{Placeholder: "message"})
	m.Messages = []Msg{{Role: "user", Content: "old"}}
	m.Textarea.SetValue("draft")

	if ok := m.BrowseInputHistory(-1); ok {
		t.Fatal("BrowseInputHistory(-1) = true, want false for non-empty draft")
	}
	if got, want := m.Textarea.Value(), "draft"; got != want {
		t.Fatalf("textarea = %q, want %q", got, want)
	}
}
