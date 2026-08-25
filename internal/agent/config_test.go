package agent

import (
	"encoding/json"
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
	"github.com/alanchenchen/suna/internal/skill"
)

func decodeModelInput(t *testing.T, raw string) protocol.ConfigModel {
	t.Helper()
	var model protocol.ConfigModel
	if err := json.Unmarshal([]byte(raw), &model); err != nil {
		t.Fatalf("Unmarshal(ConfigModel) error = %v", err)
	}
	return model
}

func TestAgentSkillStoreSaveUpdatesConfigSnapshotAndModTime(t *testing.T) {
	dir := t.TempDir()
	cfg := newAgentConfig(dir, []config.ModelConfig{openAIModel("gpt-4o-mini")}, "openai/gpt-4o-mini")
	cfg.Skills = map[string]skill.Record{"writer": {Enabled: false}}
	if err := cfg.Save(cfg.ConfigPath()); err != nil {
		t.Fatal(err)
	}
	a := &Agent{cfg: cfg}
	if info, err := os.Stat(cfg.ConfigPath()); err == nil {
		a.configModTime = info.ModTime()
	}
	store := agentSkillStore{agent: a}
	if err := store.SaveSkillRecords(map[string]skill.Record{"writer": {Enabled: true}}); err != nil {
		t.Fatalf("SaveSkillRecords() error = %v", err)
	}
	if !a.Config().Skills["writer"].Enabled {
		t.Fatal("Agent config Skill enabled = false, want true")
	}
	if _, changed, err := a.reloadConfigFromDiskIfNeededLocked(); err != nil || changed {
		t.Fatalf("reloadConfigFromDiskIfNeededLocked() = changed %v, err %v; want false,nil", changed, err)
	}
}

func TestUpdateConfigPatchPreservesMissingOptionalFieldsAndClearsExplicitValues(t *testing.T) {
	dir := t.TempDir()
	existing := openAIModel("gpt-4o-mini")
	existing.Protocol = config.ModelProtocolAnthropic
	existing.AuthMode = config.AuthModeBoth
	existing.Strengths = []string{"general", "vision"}
	existing.SubtaskFor = []string{"provider-a/**"}
	existing.Reasoning = map[string]any{"effort": "high"}
	cfg := newAgentConfig(dir, []config.ModelConfig{existing}, existing.Ref())
	mustSaveCredential(t, dir, "openai", "test-openai-key")
	a := &Agent{cfg: cfg}

	updated, err := a.UpdateConfig(ConfigSetParams{
		Action:   protocol.ConfigActionUpsertModel,
		ModelRef: existing.Ref(),
		Model:    decodeModelInput(t, `{"base_url":"https://api.example.com"}`),
	})
	if err != nil {
		t.Fatalf("UpdateConfig(preserve) error = %v", err)
	}
	got := updated.Models[0]
	if got.AuthMode != config.AuthModeBoth || !reflect.DeepEqual(got.Strengths, existing.Strengths) || !reflect.DeepEqual(got.SubtaskFor, existing.SubtaskFor) || !reflect.DeepEqual(got.Reasoning, existing.Reasoning) {
		t.Fatalf("preserved model = %#v, want optional fields from %#v", got, existing)
	}

	updated, err = a.UpdateConfig(ConfigSetParams{
		Action:   protocol.ConfigActionUpsertModel,
		ModelRef: existing.Ref(),
		Model:    decodeModelInput(t, `{"auth_mode":"default","strengths":[],"subtask_for":[],"reasoning":{}}`),
	})
	if err != nil {
		t.Fatalf("UpdateConfig(clear) error = %v", err)
	}
	got = updated.Models[0]
	if got.AuthMode != "" || len(got.Strengths) != 0 || len(got.SubtaskFor) != 0 || len(got.Reasoning) != 0 {
		t.Fatalf("cleared model = %#v, want default/empty optional fields", got)
	}
}

func TestUpdateConfigNewModelUsesDefaultsForMissingOptionalFields(t *testing.T) {
	dir := t.TempDir()
	cfg := newAgentConfig(dir, nil, "")
	mustSaveCredential(t, dir, "provider-a", "test-api-key")
	a := &Agent{cfg: cfg}
	updated, err := a.UpdateConfig(ConfigSetParams{
		Action: protocol.ConfigActionUpsertModel,
		Model:  decodeModelInput(t, `{"provider":"provider-a","model":"model-a","base_url":"https://api.example.com/v1","context_window":128000,"max_output_tokens":8192}`),
	})
	if err != nil {
		t.Fatalf("UpdateConfig() error = %v", err)
	}
	got := updated.Models[0]
	if got.ProtocolOrDefault() != config.ModelProtocolOpenAIChat || got.AuthMode != "" || len(got.Strengths) != 0 || len(got.SubtaskFor) != 0 || len(got.Reasoning) != 0 {
		t.Fatalf("new model defaults = %#v", got)
	}
}

func TestUpdateConfigPatchFallsBackToExplicitModelRefWithoutDuplicating(t *testing.T) {
	dir := t.TempDir()
	existing := openAIModel("gpt-4o-mini")
	cfg := newAgentConfig(dir, []config.ModelConfig{existing}, existing.Ref())
	mustSaveCredential(t, dir, "openai", "test-openai-key")
	a := &Agent{cfg: cfg}
	updated, err := a.UpdateConfig(ConfigSetParams{
		Action:   protocol.ConfigActionUpsertModel,
		ModelRef: "missing/old-model",
		Model:    decodeModelInput(t, `{"provider":"openai","model":"gpt-4o-mini","base_url":"https://api.example.com/v1"}`),
	})
	if err != nil {
		t.Fatalf("UpdateConfig() error = %v", err)
	}
	if len(updated.Models) != 1 || updated.Models[0].BaseURL != "https://api.example.com/v1" {
		t.Fatalf("updated models = %#v, want one updated model", updated.Models)
	}
}

func TestUpdateConfigEditingModelToDifferentProviderUsesOnlyNewProviderCredential(t *testing.T) {
	dir := t.TempDir()
	cfg := newAgentConfig(dir, []config.ModelConfig{openAIModel("gpt-4o-mini")}, "openai/gpt-4o-mini")
	mustSaveCredential(t, dir, "openai", "test-openai-key")
	mustSaveCredential(t, dir, "anthropic", "test-anthropic-key")
	a := &Agent{cfg: cfg}

	updated, err := a.UpdateConfig(ConfigSetParams{
		Action:   protocol.ConfigActionUpsertModel,
		ModelRef: "openai/gpt-4o-mini",
		Model: protocol.ConfigModel{
			Provider:        "anthropic",
			Protocol:        string(config.ModelProtocolAnthropic),
			AuthMode:        string(config.AuthModeBearer),
			Model:           "claude-sonnet-4",
			BaseURL:         "https://api.anthropic.com",
			ContextWindow:   200000,
			MaxOutputTokens: 8192,
		},
	})
	if err != nil {
		t.Fatalf("UpdateConfig() error = %v", err)
	}
	if got, want := updated.Models[0].APIKey, "test-anthropic-key"; got != want {
		t.Fatalf("updated model API key = %q, want new provider credential %q", got, want)
	}
	if got, want := updated.Models[0].AuthMode, config.AuthModeBearer; got != want {
		t.Fatalf("updated model AuthMode = %q, want %q", got, want)
	}
	savedBytes, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("ReadFile(config.toml) error = %v", err)
	}
	saved := string(savedBytes)
	if !strings.Contains(saved, `auth_mode = "bearer"`) {
		t.Fatalf("saved config = %q, want auth_mode bearer", saved)
	}
	if got := loadModelCredential(t, dir, "anthropic", "claude-sonnet-4"); got != "test-anthropic-key" {
		t.Fatalf("reloaded model API key = %q, want scoped anthropic credential", got)
	}
}

func TestUpdateConfigEditingModelToProviderWithoutCredentialFails(t *testing.T) {
	dir := t.TempDir()
	cfg := newAgentConfig(dir, []config.ModelConfig{openAIModel("gpt-4o-mini")}, "openai/gpt-4o-mini")
	mustSaveCredential(t, dir, "openai", "test-openai-key")
	a := &Agent{cfg: cfg}

	_, err := a.UpdateConfig(ConfigSetParams{
		Action:   protocol.ConfigActionUpsertModel,
		ModelRef: "openai/gpt-4o-mini",
		Model: protocol.ConfigModel{
			Provider:        "anthropic",
			Protocol:        string(config.ModelProtocolAnthropic),
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
	mustSaveCredential(t, dir, "openai", "test-openai-key")
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
	mustSaveCredential(t, dir, "openai", "test-openai-key")
	a := &Agent{cfg: cfg}

	if _, err := a.UpdateConfig(ConfigSetParams{Action: protocol.ConfigActionDeleteModel, ModelRef: "openai/gpt-4o-mini"}); err != nil {
		t.Fatalf("UpdateConfig() error = %v", err)
	}
	if got := loadModelCredential(t, dir, "openai", "gpt-4o-mini"); got != "test-openai-key" {
		t.Fatalf("loaded API key = %q, want %q", got, "test-openai-key")
	}
}

func TestUpdateConfigDeleteLastProviderModelCanDeleteCredential(t *testing.T) {
	dir := t.TempDir()
	cfg := newAgentConfig(dir, []config.ModelConfig{openAIModel("gpt-4o-mini")}, "openai/gpt-4o-mini")
	mustSaveCredential(t, dir, "openai", "test-openai-key")
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
	mustSaveCredential(t, dir, "openai", "test-openai-key")
	a := &Agent{cfg: cfg}

	if _, err := a.UpdateConfig(ConfigSetParams{Action: protocol.ConfigActionDeleteModel, ModelRef: "openai/gpt-4o-mini", DeleteAPIKey: true}); err != nil {
		t.Fatalf("UpdateConfig() error = %v", err)
	}
	if got := loadModelCredential(t, dir, "openai", "gpt-4o"); got != "test-openai-key" {
		t.Fatalf("loaded API key = %q, want %q", got, "test-openai-key")
	}
}

func TestUpdateConfigAddsModelWithExistingProviderCredential(t *testing.T) {
	dir := t.TempDir()
	cfg := newAgentConfig(dir, []config.ModelConfig{openAIModel("gpt-4o-mini")}, "openai/gpt-4o-mini")
	mustSaveCredential(t, dir, "openai", "test-openai-key")
	a := &Agent{cfg: cfg}

	updated, err := a.UpdateConfig(ConfigSetParams{
		Action: protocol.ConfigActionUpsertModel,
		Model: protocol.ConfigModel{
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
	if got, want := updated.Models[1].APIKey, "test-openai-key"; got != want {
		t.Fatalf("new model API key = %q, want shared provider key %q", got, want)
	}
	if got, want := loadModelCredential(t, dir, "openai", "gpt-4o"), "test-openai-key"; got != want {
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
		{Provider: "openai", Model: "gpt-4.1", BaseURL: "https://api.example.com/v1", ContextWindow: 400000, MaxOutputTokens: 8192, APIKey: "test-api-key"},
		{Provider: "anthropic", Model: "claude-sonnet-4", BaseURL: "https://api.anthropic.com", ContextWindow: 200000, MaxOutputTokens: 8192, APIKey: "test-api-key", SubtaskFor: []string{"openai/**"}},
		{Provider: "anthropic", Model: "claude-3-5-haiku-20241022", BaseURL: "https://api.anthropic.com", ContextWindow: 200000, MaxOutputTokens: 8192, APIKey: "test-api-key", SubtaskFor: []string{"anthropic/**"}},
	}, "openai/gpt-4.1")
	router, err := model.NewRouter(cfg, media.NewStore(t.TempDir()))
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	a := &Agent{cfg: cfg, router: router, modelRef: "openai/gpt-4.1"}

	summary := a.modelRoutingSummary()
	if !strings.Contains(summary, "anthropic/claude-sonnet-4") {
		t.Fatalf("modelRoutingSummary() = %q, want matching subtask model", summary)
	}
	if strings.Contains(summary, "anthropic/claude-3-5-haiku-20241022") {
		t.Fatalf("modelRoutingSummary() = %q, should hide non-matching subtask model", summary)
	}
	if !strings.Contains(summary, "openai/gpt-4.1") {
		t.Fatalf("modelRoutingSummary() = %q, active model should remain spawnable for itself", summary)
	}
}

func TestAvailableModelRefsFiltersSubtaskFor(t *testing.T) {
	cfg := newAgentConfig(t.TempDir(), []config.ModelConfig{
		{Provider: "openai", Model: "gpt-4.1", BaseURL: "https://api.example.com/v1", ContextWindow: 400000, MaxOutputTokens: 8192, APIKey: "test-api-key"},
		{Provider: "anthropic", Model: "claude-sonnet-4", BaseURL: "https://api.anthropic.com", ContextWindow: 200000, MaxOutputTokens: 8192, APIKey: "test-api-key", SubtaskFor: []string{"openai/**"}},
		{Provider: "anthropic", Model: "claude-3-5-haiku-20241022", BaseURL: "https://api.anthropic.com", ContextWindow: 200000, MaxOutputTokens: 8192, APIKey: "test-api-key", SubtaskFor: []string{"anthropic/**"}},
	}, "openai/gpt-4.1")
	router, err := model.NewRouter(cfg, media.NewStore(t.TempDir()))
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	a := &Agent{cfg: cfg, router: router, modelRef: "openai/gpt-4.1"}

	got := a.availableModelRefs()
	want := []string{"anthropic/claude-sonnet-4", "openai/gpt-4.1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("availableModelRefs() = %#v, want %#v", got, want)
	}
}

func TestUpdateConfigRouterBuildFailureLeavesRuntimeAndDiskUnchanged(t *testing.T) {
	dir := t.TempDir()
	cfg := newAgentConfig(dir, []config.ModelConfig{openAIModel("gpt-4o-mini")}, "openai/gpt-4o-mini")
	mustSaveCredential(t, dir, "openai", "test-openai-key")
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
		Model:    protocol.ConfigModel{Provider: "openai", Model: "gpt-4o-mini", BaseURL: "", ContextWindow: 128000, MaxOutputTokens: 8192},
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
	mustSaveCredential(t, dir, "openai", "test-openai-key")
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
	mustSaveCredential(t, scopedDir, "openai", "test-scoped-key")
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
	if got, want := reloaded.Models[0].APIKey, "test-scoped-key"; got != want {
		t.Fatalf("reloaded API key = %q, want scoped credential %q", got, want)
	}
}

func TestUpdateConfigPreservesSubtaskFor(t *testing.T) {
	dir := t.TempDir()
	cfg := newAgentConfig(dir, []config.ModelConfig{openAIModel("gpt-4o-mini")}, "openai/gpt-4o-mini")
	mustSaveCredential(t, dir, "openai", "test-openai-key")
	a := &Agent{cfg: cfg}

	updated, err := a.UpdateConfig(ConfigSetParams{
		Action:   protocol.ConfigActionUpsertModel,
		ModelRef: "openai/gpt-4o-mini",
		Model: protocol.ConfigModel{
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
