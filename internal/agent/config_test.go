package agent

import (
	"context"
	"testing"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/media"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/protocol"
)

func TestUpdateConfigDeleteModelKeepsCredentialByDefault(t *testing.T) {
	dir := t.TempDir()
	cfg := newAgentConfig(dir, []config.ModelConfig{openAIModel("gpt-4o-mini")}, "openai/gpt-4o-mini")
	mustSaveCredential(t, dir, "openai", "sk-openai")
	a := &Agent{cfg: cfg}

	if _, err := a.UpdateConfig(ConfigSetParams{Action: protocol.ConfigActionDeleteModel, ModelRef: "openai/gpt-4o-mini"}); err != nil {
		t.Fatalf("UpdateConfig() error = %v", err)
	}
	if got := loadModelCredential(t, dir, "openai", "gpt-4o-mini"); got != "sk-openai" {
		t.Fatalf("loaded API key = %q, want %q", got, "sk-openai")
	}
}

func TestUpdateConfigDeleteLastProviderModelCanDeleteCredential(t *testing.T) {
	dir := t.TempDir()
	cfg := newAgentConfig(dir, []config.ModelConfig{openAIModel("gpt-4o-mini")}, "openai/gpt-4o-mini")
	mustSaveCredential(t, dir, "openai", "sk-openai")
	a := &Agent{cfg: cfg}

	if _, err := a.UpdateConfig(ConfigSetParams{Action: protocol.ConfigActionDeleteModel, ModelRef: "openai/gpt-4o-mini", DeleteAPIKey: true}); err != nil {
		t.Fatalf("UpdateConfig() error = %v", err)
	}
	if got := loadModelCredential(t, dir, "openai", "gpt-4o-mini"); got != "" {
		t.Fatalf("loaded API key = %q, want empty", got)
	}
}

func TestUpdateConfigDoesNotDeleteCredentialWhenProviderStillUsed(t *testing.T) {
	dir := t.TempDir()
	cfg := newAgentConfig(dir, []config.ModelConfig{openAIModel("gpt-4o-mini"), openAIModel("gpt-4o")}, "openai/gpt-4o-mini")
	mustSaveCredential(t, dir, "openai", "sk-openai")
	a := &Agent{cfg: cfg}

	if _, err := a.UpdateConfig(ConfigSetParams{Action: protocol.ConfigActionDeleteModel, ModelRef: "openai/gpt-4o-mini", DeleteAPIKey: true}); err != nil {
		t.Fatalf("UpdateConfig() error = %v", err)
	}
	if got := loadModelCredential(t, dir, "openai", "gpt-4o"); got != "sk-openai" {
		t.Fatalf("loaded API key = %q, want %q", got, "sk-openai")
	}
}

func TestReloadRouterUpdatesMemoryWorkerProvider(t *testing.T) {
	dir := t.TempDir()
	cfg := newAgentConfig(dir, []config.ModelConfig{openAIModel("gpt-4o-mini"), anthropicModel("claude-sonnet")}, "openai/gpt-4o-mini")
	if err := cfg.Save(cfg.ConfigPath()); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	mustSaveCredential(t, dir, "openai", "sk-openai")
	mustSaveCredential(t, dir, "anthropic", "sk-anthropic")
	if err := config.LoadCredentials(cfg); err != nil {
		t.Fatalf("LoadCredentials() error = %v", err)
	}
	worker := newMemoryWorker(t, cfg)
	router, err := model.NewRouter(cfg, media.NewStore(t.TempDir()))
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	a := &Agent{cfg: cfg, router: router, mediaStore: media.NewStore(t.TempDir()), compressor: memory.NewCompressor(model.NewRoutedProvider(router)), extractWorker: worker}

	initial := worker.Provider()
	if initial == nil {
		t.Fatalf("worker.Provider() = nil, want OpenAIResponsesProvider")
	}
	if got := initial.ContextWindow(); got != 128000 {
		t.Fatalf("worker.Provider().ContextWindow() = %d, want 128000", got)
	}
	if _, err := a.UpdateConfig(ConfigSetParams{Action: protocol.ConfigActionActivateModel, ActiveModel: "anthropic/claude-sonnet"}); err != nil {
		t.Fatalf("UpdateConfig() error = %v", err)
	}
	updated := worker.Provider()
	if updated == nil {
		t.Fatalf("worker.Provider() after update = nil, want AnthropicProvider")
	}
	if got := updated.ContextWindow(); got != 200000 {
		t.Fatalf("worker.Provider() after update ContextWindow() = %d, want 200000", got)
	}
	if updated == initial {
		t.Fatalf("worker.Provider() after update reused initial provider, want replacement")
	}
}

func TestReloadRouterClearsMemoryWorkerProviderWithoutActiveModel(t *testing.T) {
	provider := fakeProvider{}
	worker := memory.NewWorker(nil, nil, nil, provider)
	a := &Agent{extractWorker: worker}

	if err := a.reloadRouterLocked(&config.Config{}); err != nil {
		t.Fatalf("reloadRouterLocked() error = %v", err)
	}
	if got := worker.Provider(); got != nil {
		t.Fatalf("worker.Provider() = %T, want nil", got)
	}
}

func newAgentConfig(dir string, models []config.ModelConfig, activeModel string) *config.Config {
	return &config.Config{
		ActiveModel: activeModel,
		Models:      models,
		UI:          config.UIConfig{Theme: "auto", Locale: "en"},
		Guard:       config.GuardConfig{Mode: "ask"},
		DataDir:     dir,
	}
}

func openAIModel(name string) config.ModelConfig {
	return config.ModelConfig{Provider: "openai", Model: name, BaseURL: "https://api.openai.com/v1", ContextWindow: 128000, MaxOutputTokens: 8192}
}

func anthropicModel(name string) config.ModelConfig {
	return config.ModelConfig{Provider: "anthropic", Model: name, BaseURL: "https://api.anthropic.com", ContextWindow: 200000, MaxOutputTokens: 8192}
}

func mustSaveCredential(t *testing.T, dir, provider, key string) {
	t.Helper()
	if err := config.SaveCredential(dir, provider, key); err != nil {
		t.Fatalf("SaveCredential(%q) error = %v", provider, err)
	}
}

func loadModelCredential(t *testing.T, dir, provider, modelName string) string {
	t.Helper()
	loaded := &config.Config{Models: []config.ModelConfig{{Provider: provider, Model: modelName, ContextWindow: 128000, MaxOutputTokens: 8192}}, DataDir: dir}
	if err := config.LoadCredentials(loaded); err != nil {
		t.Fatalf("LoadCredentials() error = %v", err)
	}
	return loaded.Models[0].APIKey
}

func newMemoryWorker(t *testing.T, cfg *config.Config) *memory.Worker {
	t.Helper()
	store, err := memory.NewStore(config.DataDirDBPath(cfg.DataDir))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	router, err := model.NewRouter(cfg, media.NewStore(t.TempDir()))
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	return memory.NewWorker(memory.NewExtractQueue(store.DB()), memory.NewMemoryStore(store.DB()), store.DB(), model.NewRoutedProvider(router))
}

type fakeProvider struct{}

func (fakeProvider) Complete(context.Context, *model.CompletionRequest) (<-chan model.Chunk, error) {
	ch := make(chan model.Chunk)
	close(ch)
	return ch, nil
}

func (fakeProvider) EstimateTokens(string) int { return 0 }

func (fakeProvider) ContextWindow() int   { return 128000 }
func (fakeProvider) MaxOutputTokens() int { return 8192 }
