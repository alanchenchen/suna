package agent

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/media"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/protocol"
)

func TestUpdateConfigEditingModelToDifferentProviderUsesOnlyNewProviderCredential(t *testing.T) {
	dir := t.TempDir()
	cfg := newAgentConfig(dir, []config.ModelConfig{openAIModel("gpt-4o-mini")}, "openai/gpt-4o-mini")
	mustSaveCredential(t, dir, "openai", "sk-openai")
	mustSaveCredential(t, dir, "anthropic", "sk-anthropic")
	a := &Agent{cfg: cfg}

	updated, err := a.UpdateConfig(ConfigSetParams{
		Action:   protocol.ConfigActionUpsertModel,
		ModelRef: "openai/gpt-4o-mini",
		Model: ConfigModel{
			Provider:        "anthropic",
			Protocol:        config.ModelProtocolAnthropic,
			Model:           "claude-sonnet-4",
			BaseURL:         "https://api.anthropic.com",
			ContextWindow:   200000,
			MaxOutputTokens: 8192,
		},
	})
	if err != nil {
		t.Fatalf("UpdateConfig() error = %v", err)
	}
	if got, want := updated.Models[0].APIKey, "sk-anthropic"; got != want {
		t.Fatalf("updated model API key = %q, want new provider credential %q", got, want)
	}
	if got := loadModelCredential(t, dir, "anthropic", "claude-sonnet-4"); got != "sk-anthropic" {
		t.Fatalf("reloaded model API key = %q, want scoped anthropic credential", got)
	}
}

func TestUpdateConfigEditingModelToProviderWithoutCredentialFails(t *testing.T) {
	dir := t.TempDir()
	cfg := newAgentConfig(dir, []config.ModelConfig{openAIModel("gpt-4o-mini")}, "openai/gpt-4o-mini")
	mustSaveCredential(t, dir, "openai", "sk-openai")
	a := &Agent{cfg: cfg}

	_, err := a.UpdateConfig(ConfigSetParams{
		Action:   protocol.ConfigActionUpsertModel,
		ModelRef: "openai/gpt-4o-mini",
		Model: ConfigModel{
			Provider:        "anthropic",
			Protocol:        config.ModelProtocolAnthropic,
			Model:           "claude-sonnet-4",
			BaseURL:         "https://api.anthropic.com",
			ContextWindow:   200000,
			MaxOutputTokens: 8192,
		},
	})
	if err == nil {
		t.Fatal("UpdateConfig() error = nil, want missing new provider credential error")
	}
	if !strings.Contains(err.Error(), "missing api_key") {
		t.Fatalf("UpdateConfig() error = %v, want missing API key error", err)
	}
}

func TestUpdateConfigDeleteActiveModelSelectsFirstRemainingOrClearsDefault(t *testing.T) {
	dir := t.TempDir()
	cfg := newAgentConfig(dir, []config.ModelConfig{openAIModel("gpt-4o-mini"), openAIModel("gpt-4o")}, "openai/gpt-4o-mini")
	mustSaveCredential(t, dir, "openai", "sk-openai")
	a := &Agent{cfg: cfg}

	updated, err := a.UpdateConfig(ConfigSetParams{Action: protocol.ConfigActionDeleteModel, ModelRef: "openai/gpt-4o-mini"})
	if err != nil {
		t.Fatalf("UpdateConfig() deleting active model: %v", err)
	}
	if got, want := updated.ActiveModel, "openai/gpt-4o"; got != want {
		t.Fatalf("ActiveModel after deleting active model = %q, want first remaining model %q", got, want)
	}

	updated, err = a.UpdateConfig(ConfigSetParams{Action: protocol.ConfigActionDeleteModel, ModelRef: "openai/gpt-4o"})
	if err != nil {
		t.Fatalf("UpdateConfig() deleting last model: %v", err)
	}
	if got := updated.ActiveModel; got != "" {
		t.Fatalf("ActiveModel after deleting last model = %q, want empty", got)
	}
}

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

func TestUpdateConfigAddsModelWithExistingProviderCredential(t *testing.T) {
	dir := t.TempDir()
	cfg := newAgentConfig(dir, []config.ModelConfig{openAIModel("gpt-4o-mini")}, "openai/gpt-4o-mini")
	mustSaveCredential(t, dir, "openai", "sk-openai")
	a := &Agent{cfg: cfg}

	updated, err := a.UpdateConfig(ConfigSetParams{
		Action: protocol.ConfigActionUpsertModel,
		Model: ConfigModel{
			Provider:        "openai",
			Model:           "gpt-4o",
			BaseURL:         "https://api.openai.com/v1",
			ContextWindow:   128000,
			MaxOutputTokens: 8192,
		},
	})
	if err != nil {
		t.Fatalf("UpdateConfig() error = %v", err)
	}
	if got, want := len(updated.Models), 2; got != want {
		t.Fatalf("configured model count = %d, want %d", got, want)
	}
	if got, want := updated.Models[1].APIKey, "sk-openai"; got != want {
		t.Fatalf("new model API key = %q, want shared provider key %q", got, want)
	}
	if got, want := loadModelCredential(t, dir, "openai", "gpt-4o"), "sk-openai"; got != want {
		t.Fatalf("reloaded new model API key = %q, want shared provider key %q", got, want)
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
	return memory.NewWorker(memory.NewExtractQueue(store.DB()), memory.NewMemoryStore(store.DB()), store.DB(), func(ref string) (*model.ModelBinding, error) { return router.Bind(ref) })
}

func TestModelRoutingSummaryFiltersSubtaskFor(t *testing.T) {
	cfg := newAgentConfig(t.TempDir(), []config.ModelConfig{
		{Provider: "openai", Model: "gpt-4.1", BaseURL: "https://api.example.com/v1", ContextWindow: 400000, MaxOutputTokens: 8192, APIKey: "sk-test"},
		{Provider: "DF", Model: "MiniMax-M3", BaseURL: "https://api.example.com/v1", ContextWindow: 1000000, MaxOutputTokens: 8192, APIKey: "sk-test", SubtaskFor: []string{"openai/**"}},
		{Provider: "DF", Model: "glm-5.2", BaseURL: "https://api.example.com/v1", ContextWindow: 1000000, MaxOutputTokens: 8192, APIKey: "sk-test", SubtaskFor: []string{"DF/**"}},
	}, "openai/gpt-4.1")
	router, err := model.NewRouter(cfg, media.NewStore(t.TempDir()))
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	a := &Agent{cfg: cfg, router: router, modelRef: "openai/gpt-4.1"}

	summary := a.modelRoutingSummary()
	if !strings.Contains(summary, "DF/MiniMax-M3") {
		t.Fatalf("modelRoutingSummary() = %q, want matching subtask model", summary)
	}
	if strings.Contains(summary, "DF/glm-5.2") {
		t.Fatalf("modelRoutingSummary() = %q, should hide non-matching subtask model", summary)
	}
	if !strings.Contains(summary, "openai/gpt-4.1") {
		t.Fatalf("modelRoutingSummary() = %q, active model should remain spawnable for itself", summary)
	}
}

func TestAvailableModelRefsFiltersSubtaskFor(t *testing.T) {
	cfg := newAgentConfig(t.TempDir(), []config.ModelConfig{
		{Provider: "openai", Model: "gpt-4.1", BaseURL: "https://api.example.com/v1", ContextWindow: 400000, MaxOutputTokens: 8192, APIKey: "sk-test"},
		{Provider: "DF", Model: "MiniMax-M3", BaseURL: "https://api.example.com/v1", ContextWindow: 1000000, MaxOutputTokens: 8192, APIKey: "sk-test", SubtaskFor: []string{"openai/**"}},
		{Provider: "DF", Model: "glm-5.2", BaseURL: "https://api.example.com/v1", ContextWindow: 1000000, MaxOutputTokens: 8192, APIKey: "sk-test", SubtaskFor: []string{"DF/**"}},
	}, "openai/gpt-4.1")
	router, err := model.NewRouter(cfg, media.NewStore(t.TempDir()))
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	a := &Agent{cfg: cfg, router: router, modelRef: "openai/gpt-4.1"}

	got := a.availableModelRefs()
	want := []string{"DF/MiniMax-M3", "openai/gpt-4.1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("availableModelRefs() = %#v, want %#v", got, want)
	}
}

func TestUpdateConfigRouterBuildFailureLeavesRuntimeAndDiskUnchanged(t *testing.T) {
	dir := t.TempDir()
	cfg := newAgentConfig(dir, []config.ModelConfig{openAIModel("gpt-4o-mini")}, "openai/gpt-4o-mini")
	mustSaveCredential(t, dir, "openai", "sk-openai")
	if err := config.LoadCredentials(cfg); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(cfg.ConfigPath()); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	beforeDisk, err := os.ReadFile(cfg.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	router, err := model.NewRouter(cfg, media.NewStore(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{cfg: cfg, router: router}

	_, err = a.UpdateConfig(ConfigSetParams{
		Action:   protocol.ConfigActionUpsertModel,
		ModelRef: "openai/gpt-4o-mini",
		Model:    ConfigModel{Provider: "openai", Model: "gpt-4o-mini", BaseURL: "", ContextWindow: 128000, MaxOutputTokens: 8192},
	})
	if err == nil {
		t.Fatal("UpdateConfig() error = nil, want Router build failure")
	}
	if a.cfg != cfg || a.router != router {
		t.Fatal("failed UpdateConfig() published a new runtime snapshot")
	}
	afterDisk, err := os.ReadFile(cfg.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(afterDisk, beforeDisk) {
		t.Fatalf("config.toml changed after failed update:\n got %q\nwant %q", afterDisk, beforeDisk)
	}
}

func TestReloadConfigRouterBuildFailureLeavesRuntimeSnapshotUnchanged(t *testing.T) {
	dir := t.TempDir()
	cfg := newAgentConfig(dir, []config.ModelConfig{openAIModel("gpt-4o-mini")}, "openai/gpt-4o-mini")
	mustSaveCredential(t, dir, "openai", "sk-openai")
	if err := config.LoadCredentials(cfg); err != nil {
		t.Fatal(err)
	}
	router, err := model.NewRouter(cfg, media.NewStore(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	invalid := newAgentConfig(dir, []config.ModelConfig{{Provider: "openai", Model: "gpt-4o-mini", BaseURL: "", ContextWindow: 128000, MaxOutputTokens: 8192}}, "openai/gpt-4o-mini")
	if err := invalid.Save(filepath.Join(dir, "config.toml")); err != nil {
		t.Fatal(err)
	}
	a := &Agent{cfg: cfg, router: router}

	if _, err := a.ReloadConfigFromDiskIfNeeded(); err == nil {
		t.Fatal("ReloadConfigFromDiskIfNeeded() error = nil, want Router build failure")
	}
	if a.cfg != cfg || a.router != router || !a.configModTime.IsZero() {
		t.Fatal("failed reload changed the published runtime snapshot")
	}
}

func TestReloadConfigFromDiskUsesScopedDataDirCredentials(t *testing.T) {
	defaultRoot := t.TempDir()
	t.Setenv("HOME", defaultRoot)
	defaultDir := config.DefaultDataDir()
	if err := os.MkdirAll(defaultDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(defaultDir, "credentials.toml"), []byte("[openai\napi_key = \"broken\"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	scopedDir := t.TempDir()
	stored := newAgentConfig(scopedDir, []config.ModelConfig{openAIModel("gpt-4o-mini")}, "openai/gpt-4o-mini")
	if err := stored.Save(stored.ConfigPath()); err != nil {
		t.Fatal(err)
	}
	mustSaveCredential(t, scopedDir, "openai", "sk-scoped")
	cfg, err := config.LoadFromDataDir(stored.ConfigPath(), scopedDir)
	if err != nil {
		t.Fatalf("LoadFromDataDir() error = %v", err)
	}
	router, err := model.NewRouter(cfg, media.NewStore(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{cfg: cfg, router: router}

	reloaded, err := a.ReloadConfigFromDiskIfNeeded()
	if err != nil {
		t.Fatalf("ReloadConfigFromDiskIfNeeded() error = %v", err)
	}
	if got, want := reloaded.DataDir, scopedDir; got != want {
		t.Fatalf("reloaded DataDir = %q, want %q", got, want)
	}
	if got, want := reloaded.Models[0].APIKey, "sk-scoped"; got != want {
		t.Fatalf("reloaded API key = %q, want scoped credential %q", got, want)
	}
}

func TestUpdateConfigPreservesSubtaskFor(t *testing.T) {
	dir := t.TempDir()
	cfg := newAgentConfig(dir, []config.ModelConfig{openAIModel("gpt-4o-mini")}, "openai/gpt-4o-mini")
	mustSaveCredential(t, dir, "openai", "sk-openai")
	a := &Agent{cfg: cfg}

	updated, err := a.UpdateConfig(ConfigSetParams{
		Action:   protocol.ConfigActionUpsertModel,
		ModelRef: "openai/gpt-4o-mini",
		Model: ConfigModel{
			Provider:        "openai",
			Model:           "gpt-4o-mini",
			BaseURL:         "https://api.openai.com/v1",
			ContextWindow:   128000,
			MaxOutputTokens: 8192,
			SubtaskFor:      []string{"openai/**", "anthropic/**"},
		},
	})
	if err != nil {
		t.Fatalf("UpdateConfig() error = %v", err)
	}
	got := updated.Models[0].SubtaskFor
	want := []string{"openai/**", "anthropic/**"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SubtaskFor = %#v, want %#v", got, want)
	}
}
