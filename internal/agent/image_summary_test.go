package agent

import (
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
)

func imageBlockWith(name, path string) model.ContentBlock {
	return model.ContentBlock{Type: model.ContentImage, Media: &model.MediaRef{Kind: model.MediaAttachment, Path: path, Name: name, MimeType: "image/png", Size: 2048}}
}

func TestReplaceToolImagesWithSummariesConvertsAllImageMessages(t *testing.T) {
	a := &Agent{working: memory.NewWorkingMemory()}
	a.working.AddMessage(model.NewTextMessage(model.RoleUser, "look at these"))
	a.working.AddMessage(model.Message{
		Role:    model.RoleUser,
		Content: []model.ContentBlock{{Type: model.ContentText, Text: "look at these"}, imageBlockWith("a.png", "/att/a.png")},
	})
	a.working.AddMessage(model.Message{
		Role:    model.RoleUser,
		Content: []model.ContentBlock{imageBlockWith("b.png", "/att/b.png"), imageBlockWith("c.png", "/att/c.png")},
	})

	a.replaceToolImagesWithSummaries()

	msgs := a.working.Messages()
	if len(msgs) != 3 {
		t.Fatalf("messages len = %d, want 3", len(msgs))
	}
	// 纯文本消息不受影响。
	if msgs[0].Text() != "look at these" {
		t.Fatalf("msgs[0] = %q, want unchanged text", msgs[0].Text())
	}
	// 文本 + 图片：保留文本并追加摘要。
	got1 := msgs[1].Text()
	if got1 == "" || !strings.Contains(got1, "look at these") || !strings.Contains(got1, "source=attachment:a.png") {
		t.Fatalf("msgs[1] = %q, want text + image summary", got1)
	}
	// 纯图片消息：全部转摘要。
	got2 := msgs[2].Text()
	if !strings.Contains(got2, "source=attachment:b.png") || !strings.Contains(got2, "source=attachment:c.png") {
		t.Fatalf("msgs[2] = %q, want both image summaries", got2)
	}
	// 图片块必须全部移除。
	for _, m := range msgs {
		for _, b := range m.Content {
			if b.Type == model.ContentImage {
				t.Fatalf("message %q still contains image block", m.Text())
			}
		}
	}
}

func TestReplaceToolImagesWithSummariesSkipsWithoutImages(t *testing.T) {
	a := &Agent{working: memory.NewWorkingMemory()}
	a.working.AddMessage(model.NewTextMessage(model.RoleUser, "plain"))
	a.replaceToolImagesWithSummaries()
	if got := a.working.Messages()[0].Text(); got != "plain" {
		t.Fatalf("message = %q, want unchanged", got)
	}
}
