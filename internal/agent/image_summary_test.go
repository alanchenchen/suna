package agent

import (
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
)

func TestMediaProjectionPreservesExactMessageAndBlockPositions(t *testing.T) {
	ref := model.MediaRef{Kind: model.MediaAttachment, Name: "example.png", Path: "example.png", MimeType: "image/png"}
	original := []memory.Message{memory.NewTextMessage(model.RoleUser, "same text source=attachment:example.png must remain"), {Role: model.RoleUser, Origin: memory.OriginUser, Content: []model.ContentBlock{{Type: model.ContentText, Text: "same text"}, {Type: model.ContentImage, Media: &ref}, {Type: model.ContentText, Text: "second text block"}}}}
	got := memory.ProjectMessages(original)
	if got[0].Text() != original[0].Text() {
		t.Fatal("source substring changed an unrelated message")
	}
	if got[1].Text() != "same text\n[image: example.png, image/png, source=attachment:example.png]\nsecond text block" || got[1].Origin != memory.OriginUser {
		t.Fatalf("projected=%+v", got[1])
	}
	if original[1].Content[1].Type != model.ContentImage {
		t.Fatal("checkpoint projection removed current-run image")
	}
	again := memory.ProjectMessages(got)
	if again[1].Text() != got[1].Text() || strings.Count(again[1].Text(), "source=attachment:example.png") != 1 {
		t.Fatal("projection accumulated references")
	}
}
