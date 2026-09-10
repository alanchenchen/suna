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

func TestInjectToolImagesInjectsAcrossTurns(t *testing.T) {
	working := memory.NewWorkingMemory()
	// 历史里已有同 source 的摘要（上一轮 read_image 后摘要化的结果）。
	working.AddMessage(model.NewTextMessage(model.RoleUser, "see [image: shot.png, image/png, source=/a/shot.png]"))
	r := &Runner{}
	r.toolImages = append(r.toolImages, imageBlock("/a/shot.png"))
	r.injectToolImages(working)

	// 跨轮重读应放行注入：模型可以重看图片（含图片更新后的新版），
	// 跨轮摘要去重由 agent 清理层保证，注入层不再检查历史摘要。
	msgs := working.Messages()
	if len(msgs) != 2 {
		t.Fatalf("messages len = %d, want 2 (re-read injects even if summary exists)", len(msgs))
	}
	last := msgs[len(msgs)-1]
	if len(last.Content) != 1 || last.Content[0].Type != model.ContentImage {
		t.Fatalf("injected content should contain the image block")
	}
}
