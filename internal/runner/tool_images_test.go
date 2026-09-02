package runner

import (
	"context"
	"testing"

	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/tools"
)

type imageExecutor struct {
	results []tools.Result
}

func (e imageExecutor) ExecuteTool(ctx context.Context, call ToolExecution) tools.Result {
	if len(e.results) == 0 {
		return tools.TextResult("done")
	}
	res := e.results[0]
	e.results = e.results[1:]
	return res
}

func imageBlock(path string) model.ContentBlock {
	return model.ContentBlock{Type: model.ContentImage, Media: &model.MediaRef{Kind: model.MediaPath, Path: path, Name: "shot.png", MimeType: "image/png", Size: 10}}
}

func TestInjectToolImagesAddsSingleUserMessage(t *testing.T) {
	working := memory.NewWorkingMemory()
	working.AddMessage(model.NewTextMessage(model.RoleUser, "analyze"))
	r := &Runner{Executor: imageExecutor{results: []tools.Result{
		{Content: "image loaded", Images: []model.ContentBlock{imageBlock("/a/shot.png")}},
		{Content: "image loaded", Images: []model.ContentBlock{imageBlock("/b/other.png")}},
	}}}
	// 模拟工具结果循环后的注入入口：先收集图片再注入。
	r.toolImages = append(r.toolImages, imageBlock("/a/shot.png"), imageBlock("/b/other.png"))
	r.injectToolImages(working)

	msgs := working.Messages()
	if len(msgs) != 2 {
		t.Fatalf("messages len = %d, want 2 (user + injected user)", len(msgs))
	}
	last := msgs[len(msgs)-1]
	if last.Role != model.RoleUser {
		t.Fatalf("last message role = %s, want user", last.Role)
	}
	if len(last.Content) != 2 {
		t.Fatalf("injected content blocks = %d, want 2", len(last.Content))
	}
}

func TestInjectToolImagesDeduplicatesBySource(t *testing.T) {
	working := memory.NewWorkingMemory()
	working.AddMessage(model.NewTextMessage(model.RoleUser, "analyze"))
	r := &Runner{}
	r.toolImages = append(r.toolImages, imageBlock("/a/shot.png"), imageBlock("/a/shot.png"))
	r.injectToolImages(working)

	msgs := working.Messages()
	if len(msgs) != 2 {
		t.Fatalf("messages len = %d, want 2 (duplicate image merged into one message)", len(msgs))
	}
	last := msgs[len(msgs)-1]
	if len(last.Content) != 1 {
		t.Fatalf("injected content blocks = %d, want 1 (deduplicated)", len(last.Content))
	}
}

func TestInjectToolImagesSkipsWhenSummaryAlreadyInContext(t *testing.T) {
	working := memory.NewWorkingMemory()
	working.AddMessage(model.NewTextMessage(model.RoleUser, "see [image: shot.png, source=/a/shot.png]"))
	r := &Runner{}
	r.toolImages = append(r.toolImages, imageBlock("/a/shot.png"))
	r.injectToolImages(working)

	msgs := working.Messages()
	if len(msgs) != 1 {
		t.Fatalf("messages len = %d, want 1 (image already summarized, skip injection)", len(msgs))
	}
}
