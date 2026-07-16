package agent

import (
	"context"
	"testing"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
)

func TestCompactSyncsRuntimeBeforeBindingSessionModel(t *testing.T) {
	cfg := &config.Config{Models: []config.ModelConfig{{
		Provider:        "test",
		Model:           "current",
		BaseURL:         "https://example.invalid/v1",
		ContextWindow:   128000,
		MaxOutputTokens: 8192,
		APIKey:          "test-key",
	}}}
	router, err := model.NewRouter(cfg, nil)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	root := &Agent{cfg: cfg, router: router, compressor: memory.NewCompressor()}
	session := &Agent{
		runtime: root,
		// 模拟配置重载前 session 保存的旧 router；syncRuntime 后必须改用 root 的新 router。
		router:   nil,
		modelRef: "test/current",
		working:  memory.NewWorkingMemory(),
	}
	session.working.AddMessage(model.NewTextMessage(model.RoleUser, "keep this turn"))

	_, _, contextWindow, _, _, err := session.Compact(context.Background())
	if err != nil {
		t.Fatalf("Compact() error = %v, want runtime-synced router to bind session model", err)
	}
	if contextWindow != 128000 {
		t.Fatalf("Compact() context window = %d, want 128000 from current runtime binding", contextWindow)
	}
}
