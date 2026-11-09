package memory

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/model"
)

func TestSessionStatePreservesOrderedToolsAndMedia(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.DB().Exec(`INSERT INTO sessions(id,created_at,updated_at) VALUES ('s',datetime('now'),datetime('now'))`)
	if err != nil {
		t.Fatal(err)
	}
	states := NewSessionStateStore(store.DB())
	messages := toolHistory()
	messages = append(messages, Message{Role: model.RoleUser, Origin: OriginTool, Content: []model.ContentBlock{{Type: model.ContentText, Text: "before"}, {Type: model.ContentImage, Media: &model.MediaRef{Kind: model.MediaPath, Path: "example.png", Name: "example.png"}}, {Type: model.ContentText, Text: "after"}}})
	if err = states.Save(context.Background(), "s", "summary", messages); err != nil {
		t.Fatal(err)
	}
	got, err := states.Load(context.Background(), "s")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != len(messages) || got.Messages[3].ToolStatus != "failed" || len(got.Messages[1].ToolCalls) != 2 {
		t.Fatalf("got state=%#v", got)
	}
	text := got.Messages[len(got.Messages)-1].Text()
	if !strings.Contains(text, "before\n[image:") || !strings.HasSuffix(text, "after") {
		t.Fatalf("got media=%q", text)
	}
	if messages[len(messages)-1].Content[1].Type != model.ContentImage {
		t.Fatal("projection modified original images")
	}
}
func TestSessionStateEncodingMatchesProjectedHistory(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err = store.DB().Exec(`INSERT INTO sessions(id,created_at,updated_at) VALUES ('s',datetime('now'),datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	observed := NewTextMessage(model.RoleAssistant, "original prose\n")
	observed.Observation = &Observation{Text: strings.Repeat("<evidence>\"\\\n中文\u2028\u2029", 2000), Calls: []ToolLabel{{Name: "readfile", Intent: "inspect", Status: "completed"}}}
	for _, tt := range []struct {
		name     string
		messages []Message
	}{
		{name: "nil"},
		{name: "empty", messages: []Message{}},
		{name: "tools and media", messages: append(toolHistory(), Message{Role: model.RoleUser, Content: []model.ContentBlock{{Type: model.ContentImage, Media: &model.MediaRef{Kind: model.MediaURL, URL: "https://example.com/image.png", Name: "image.png"}}}})},
		{name: "observation and escapes", messages: []Message{observed, NewTextMessage(model.RoleUser, "invalid UTF8: \xff")}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			want, err := json.Marshal(ProjectMessages(tt.messages))
			if err != nil {
				t.Fatal(err)
			}
			if err = NewSessionStateStore(store.DB()).Save(context.Background(), "s", "summary", tt.messages); err != nil {
				t.Fatal(err)
			}
			var got string
			if err = store.DB().QueryRow(`SELECT messages FROM session_state WHERE session_id = 's'`).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Fatal("stored JSON differs from projected history")
			}
		})
	}
}

func TestRecoveryRetainsKnownEmptyResultWithoutInventingMissingResult(t *testing.T) {
	messages := toolHistory()[:3]
	messages[2].TextContent = ""
	messages[2].Content = nil
	got := RecoverMessages(messages)
	if len(got) != 2 || len(got[1].ToolCalls) != 0 || !strings.Contains(got[1].Text(), "Result: unknown") {
		t.Fatalf("got=%#v", got)
	}
	if !strings.Contains(got[1].Text(), "Observed result (completed):") {
		t.Fatal("empty real result was discarded")
	}
}
