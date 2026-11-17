package runner

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/media"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/tools"
	"github.com/alanchenchen/suna/internal/tools/builtin"
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
	working.AddMessage(memory.NewTextMessage(model.RoleUser, "analyze"))
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
	working.AddMessage(memory.NewTextMessage(model.RoleUser, "analyze"))
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
	working.AddMessage(memory.NewTextMessage(model.RoleUser, "see [image: shot.png, image/png, source=/a/shot.png]"))
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

type cleanupImageExecutor struct {
	path string
}

func (e cleanupImageExecutor) ExecuteTool(ctx context.Context, call ToolExecution) tools.Result {
	switch call.Name {
	case "read_image":
		return (builtin.ReadImage{}).Execute(ctx, map[string]any{"source": e.path})
	case "cleanup":
		if err := os.Remove(e.path); err != nil {
			return tools.ErrorResult(err.Error())
		}
		return tools.TextResult("temporary image removed")
	default:
		return tools.ErrorResult("unexpected tool")
	}
}

func TestReadImageThenCleanupFinishesSameRunWithoutResendingMissingImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "temporary.png")
	if err := os.WriteFile(path, []byte("test image contents"), 0600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body json.RawMessage
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		bodies = append(bodies, string(body))
		turn := len(bodies)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		switch turn {
		case 1, 2:
			name := "read_image"
			if turn == 2 {
				name = "cleanup"
			}
			fmt.Fprintf(w, "data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-%d\",\"type\":\"function\",\"function\":{\"name\":\"%s\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n", turn, name)
		case 3:
			fmt.Fprint(w, checkpointTextSSE)
		default:
			t.Errorf("unexpected extra model request %d", turn)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()
	router, err := model.NewRouter(&config.Config{Models: []config.ModelConfig{{Provider: "example-provider", Model: "example-model", Protocol: config.ModelProtocolOpenAIChat, BaseURL: server.URL, APIKey: "test-api-key", ContextWindow: 100000, MaxOutputTokens: 1000}}}, media.NewContextResolver(""))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := router.Bind("example-provider/example-model")
	if err != nil {
		t.Fatal(err)
	}
	working := memory.NewWorkingMemory()
	working.AddMessage(memory.NewTextMessage(model.RoleUser, "inspect image, remove it, then finish"))
	r := &Runner{Executor: cleanupImageExecutor{path: path}}
	result, err := r.Run(context.Background(), Request{Binding: binding, Working: working, MaxTurns: 3, MaxTokens: 1000})
	if err != nil || result.FinalText != "complete" || result.HadToolError {
		t.Fatalf("result=%+v err=%v, want successful same-run completion", result, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 3 {
		t.Fatalf("model requests=%d, want 3 with no paid retry", len(bodies))
	}
	image := base64.StdEncoding.EncodeToString([]byte("test image contents"))
	if strings.Contains(bodies[0], image) || !strings.Contains(bodies[1], image) || strings.Contains(bodies[2], image) || strings.Contains(bodies[2], `"content":[]`) {
		t.Fatal("image was not sent exactly while its source was available")
	}
	for _, value := range []string{"temporary image removed", "call-1", "call-2", "source="} {
		if !strings.Contains(bodies[2], value) {
			t.Fatalf("final request missing retained tool fact %q", value)
		}
	}
	images := 0
	for _, message := range working.Messages() {
		for _, block := range message.Content {
			if block.Type == model.ContentImage {
				images++
			}
		}
	}
	if images != 1 {
		t.Fatalf("working image blocks=%d, want 1 until Agent run-end projection", images)
	}
}
