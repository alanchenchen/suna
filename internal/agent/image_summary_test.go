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

func TestMergeImageSummariesReplacesExistingSource(t *testing.T) {
	// 同一图片（同 source）已有旧摘要（旧 size），本轮读到更新后的图（新 size）。
	text := "[image: shot.png, image/png, 1.2KB, source=attachment:shot.png]"
	summaries := []string{"[image: shot.png, image/png, 3.4KB, source=attachment:shot.png]"}
	got := mergeImageSummaries(text, summaries)

	if strings.Contains(got, "1.2KB") {
		t.Fatalf("got %q, want old size replaced", got)
	}
	if !strings.Contains(got, "3.4KB") {
		t.Fatalf("got %q, want new size in summary", got)
	}
	if strings.Count(got, "source=attachment:shot.png") != 1 {
		t.Fatalf("got %q, want exactly one summary for the source (no accumulation)", got)
	}
}

func TestMergeImageSummariesAppendsNewSources(t *testing.T) {
	text := "[image: a.png, image/png, source=attachment:a.png]"
	summaries := []string{
		"[image: a.png, image/png, source=attachment:a.png]",
		"[image: b.png, image/png, source=attachment:b.png]",
	}
	got := mergeImageSummaries(text, summaries)

	if strings.Count(got, "source=attachment:a.png") != 1 {
		t.Fatalf("got %q, want existing summary kept once", got)
	}
	if !strings.Contains(got, "source=attachment:b.png") {
		t.Fatalf("got %q, want new summary appended", got)
	}
}
