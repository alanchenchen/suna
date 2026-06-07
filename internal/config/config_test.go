package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/skill"
)

func TestGuardWorkspaceNormalizesExistingDirectory(t *testing.T) {
	workspace := t.TempDir()
	cfg := &Config{Guard: GuardConfig{Workspace: workspace}}

	if err := cfg.NormalizeGuard(); err != nil {
		t.Fatalf("NormalizeGuard() error = %v", err)
	}
	want := cleanSymlinkPath(t, workspace)
	if got := cfg.Guard.Workspace; got != want {
		t.Fatalf("Guard.Workspace = %q, want %q", got, want)
	}
}

func TestGuardWorkspaceRejectsMissingDirectory(t *testing.T) {
	cfg := &Config{Guard: GuardConfig{Workspace: filepath.Join(t.TempDir(), "missing")}}
	if err := cfg.NormalizeGuard(); err == nil {
		t.Fatalf("NormalizeGuard() error = nil, want non-nil")
	}
}

func TestConfigSaveKeepsGuardWorkspace(t *testing.T) {
	cfg, path, workspace := newSaveTestConfig(t, 0)

	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	data := readFile(t, path)
	want := `workspace = "` + cleanSymlinkPath(t, workspace) + `"`
	if !strings.Contains(data, want) {
		t.Fatalf("saved config = %q, want substring %q", data, want)
	}
}

func TestConfigSaveOmitsDefaultMaxModelRPS(t *testing.T) {
	cfg, path, _ := newSaveTestConfig(t, 0)

	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	data := readFile(t, path)
	if strings.Contains(data, "max_model_rps") {
		t.Fatalf("saved config = %q, should not contain max_model_rps", data)
	}
	if got := cfg.GetMaxModelRPS(); got != DefaultMaxModelRPS {
		t.Fatalf("GetMaxModelRPS() = %d, want %d", got, DefaultMaxModelRPS)
	}
}

func TestConfigSaveKeepsCustomMaxModelRPS(t *testing.T) {
	cfg, path, _ := newSaveTestConfig(t, 20)

	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	data := readFile(t, path)
	if !strings.Contains(data, "max_model_rps = 20") {
		t.Fatalf("saved config = %q, want custom max_model_rps", data)
	}
}

func TestConfigReasoningRoundTripsInlineTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	workspace := mkdir(t, filepath.Join(dir, "workspace"))
	cfg := &Config{
		ActiveModel: "openai/gpt-5",
		Models: []ModelConfig{{
			Provider: "openai",
			Model:    "gpt-5",
			Reasoning: map[string]any{
				"reasoning": map[string]any{"effort": "high"},
			},
		}},
		Guard:   GuardConfig{Mode: "ask", Workspace: workspace},
		UI:      UIConfig{Theme: "auto", Locale: "en"},
		DataDir: dir,
	}

	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	data := readFile(t, path)
	if !strings.Contains(data, `reasoning = { reasoning = { effort = "high" } }`) {
		t.Fatalf("saved config = %q, want inline reasoning table", data)
	}
	var loaded Config
	if err := LoadTOML(path, &loaded); err != nil {
		t.Fatalf("LoadTOML() error = %v", err)
	}
	got := loaded.Models[0].Reasoning["reasoning"].(map[string]any)["effort"]
	if got != "high" {
		t.Fatalf("reasoning.effort = %#v, want %q", got, "high")
	}
}

func TestConfigReasoningSavesInlineThinkingTable(t *testing.T) {
	dir := t.TempDir()
	workspace := mkdir(t, filepath.Join(dir, "workspace"))
	cfg := &Config{
		ActiveModel: "DF/GLM-5.1",
		Models: []ModelConfig{{
			Provider:  "DF",
			Model:     "GLM-5.1",
			BaseURL:   "https://www.dreamfield.top/v1",
			Strengths: []string{"文本模型", "coding"},
			Reasoning: map[string]any{
				"thinking": map[string]any{"type": "disabled"},
			},
		}},
		Guard:   GuardConfig{Mode: "ask", Workspace: workspace},
		UI:      UIConfig{Theme: "auto", Locale: "en"},
		DataDir: dir,
	}
	path := filepath.Join(t.TempDir(), "config.toml")

	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	saved := readFile(t, path)
	if !strings.Contains(saved, `reasoning = { thinking = { type = "disabled" } }`) {
		t.Fatalf("saved config = %q, want inline thinking table", saved)
	}
	if strings.Contains(saved, "[models.reasoning") {
		t.Fatalf("saved config = %q, should not contain nested reasoning table", saved)
	}
	var loaded Config
	if err := LoadTOML(path, &loaded); err != nil {
		t.Fatalf("LoadTOML() error = %v", err)
	}
	thinking, ok := loaded.Models[0].Reasoning["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("thinking = %#v, want map", loaded.Models[0].Reasoning["thinking"])
	}
	if got := thinking["type"]; got != "disabled" {
		t.Fatalf("thinking.type = %#v, want %q", got, "disabled")
	}
}

func TestConfigSaveWritesSkillsAsFlatObjectMap(t *testing.T) {
	dir := t.TempDir()
	workspace := mkdir(t, filepath.Join(dir, "workspace"))
	cfg := &Config{
		ActiveModel: "test/model",
		Models:      []ModelConfig{{Provider: "test", Model: "model"}},
		Guard:       GuardConfig{Mode: "ask", Workspace: workspace},
		UI:          UIConfig{Theme: "auto", Locale: "en"},
		Skills: map[string]skill.Record{
			"img":         {Enabled: true, Reasons: []string{"contains binary or obfuscated content", "includes scripts/ helper files"}},
			"code-review": {Enabled: true},
			"needs.quote": {Enabled: false, Reasons: []string{"mentions sensitive environment variables or tokens"}},
		},
		DataDir: dir,
	}
	path := filepath.Join(dir, "config.toml")

	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	data := readFile(t, path)
	if strings.Contains(data, "[skills]\n") {
		t.Fatalf("saved config = %q, should not contain standalone [skills] table", data)
	}
	for _, section := range []string{"[skills.code-review]", "[skills.img]", "[skills.\"needs.quote\"]"} {
		if !strings.Contains(data, section) {
			t.Fatalf("saved config = %q, want section %s", data, section)
		}
	}
	if !strings.Contains(data, `reasons = ["contains binary or obfuscated content", "includes scripts/ helper files"]`) {
		t.Fatalf("saved config = %q, want inline reasons array", data)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !loaded.Skills["img"].Enabled || len(loaded.Skills["img"].Reasons) != 2 {
		t.Fatalf("loaded img skill = %#v, want enabled with reasons", loaded.Skills["img"])
	}
	if loaded.Skills["needs.quote"].Enabled {
		t.Fatalf("loaded needs.quote skill = %#v, want disabled", loaded.Skills["needs.quote"])
	}
}

func TestDeleteCredentialRemovesOnlyRequestedProvider(t *testing.T) {
	dir := t.TempDir()
	mustSaveCredential(t, dir, "openai", "sk-openai")
	mustSaveCredential(t, dir, "anthropic", "sk-anthropic")

	if err := DeleteCredential(dir, "openai"); err != nil {
		t.Fatalf("DeleteCredential() error = %v", err)
	}
	creds := loadCredentials(t, dir)
	if _, ok := creds["openai"]; ok {
		t.Fatalf("credentials[openai] exists in %#v, want absent", creds)
	}
	if got := creds["anthropic"].APIKey; got != "sk-anthropic" {
		t.Fatalf("credentials[anthropic].APIKey = %q, want %q", got, "sk-anthropic")
	}
}

func TestDeleteCredentialMissingProviderIsNoop(t *testing.T) {
	dir := t.TempDir()
	mustSaveCredential(t, dir, "openai", "sk-openai")

	if err := DeleteCredential(dir, "missing"); err != nil {
		t.Fatalf("DeleteCredential() error = %v", err)
	}
	creds := loadCredentials(t, dir)
	if got := creds["openai"].APIKey; got != "sk-openai" {
		t.Fatalf("credentials[openai].APIKey = %q, want %q", got, "sk-openai")
	}
}

func newSaveTestConfig(t *testing.T, maxModelRPS int) (*Config, string, string) {
	t.Helper()
	dir := t.TempDir()
	workspace := mkdir(t, filepath.Join(dir, "workspace"))
	cfg := &Config{
		ActiveModel: "test/model",
		Models:      []ModelConfig{{Provider: "test", Model: "model"}},
		Guard:       GuardConfig{Mode: "ask", Workspace: workspace},
		UI:          UIConfig{Theme: "auto", Locale: "en"},
		MaxModelRPS: maxModelRPS,
		DataDir:     dir,
	}
	return cfg, filepath.Join(dir, "config.toml"), workspace
}

func mkdir(t *testing.T, path string) string {
	t.Helper()
	if err := os.Mkdir(path, 0755); err != nil {
		t.Fatalf("Mkdir(%q) error = %v", path, err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	return string(data)
}

func cleanSymlinkPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q) error = %v", path, err)
	}
	return filepath.Clean(resolved)
}

func mustSaveCredential(t *testing.T, dir, provider, key string) {
	t.Helper()
	if err := SaveCredential(dir, provider, key); err != nil {
		t.Fatalf("SaveCredential(%q) error = %v", provider, err)
	}
}

func loadCredentials(t *testing.T, dir string) credentialsFile {
	t.Helper()
	creds, err := readCredentials(dir)
	if err != nil {
		t.Fatalf("readCredentials() error = %v", err)
	}
	return creds
}
