package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuardWorkspaceNormalize(t *testing.T) {
	workspace := t.TempDir()
	cfg := &Config{Guard: GuardConfig{Workspace: workspace}}
	if err := cfg.NormalizeGuard(); err != nil {
		t.Fatalf("NormalizeGuard error: %v", err)
	}
	want, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatalf("EvalSymlinks workspace: %v", err)
	}
	if cfg.Guard.Workspace != filepath.Clean(want) {
		t.Fatalf("workspace = %q, want %q", cfg.Guard.Workspace, filepath.Clean(want))
	}
}

func TestGuardWorkspaceRejectsMissingDirectory(t *testing.T) {
	cfg := &Config{Guard: GuardConfig{Workspace: filepath.Join(t.TempDir(), "missing")}}
	if err := cfg.NormalizeGuard(); err == nil {
		t.Fatalf("NormalizeGuard missing workspace succeeded, want error")
	}
}

func TestSaveKeepsGuardWorkspace(t *testing.T) {
	dir := t.TempDir()
	workspace := filepath.Join(dir, "workspace")
	if err := os.Mkdir(workspace, 0755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	cfg := &Config{
		ActiveModel: "test/model",
		Models:      []ModelConfig{{Provider: "test", Model: "model"}},
		Guard:       GuardConfig{Mode: "ask", Workspace: workspace},
		UI:          UIConfig{Theme: "auto", Locale: "en"},
		DataDir:     dir,
	}
	path := filepath.Join(dir, "config.toml")
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	want, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatalf("EvalSymlinks workspace: %v", err)
	}
	if !strings.Contains(string(data), `workspace = "`+filepath.Clean(want)+`"`) {
		t.Fatalf("saved config missing workspace: %s", string(data))
	}
}

func TestReasoningRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	workspace := filepath.Join(dir, "workspace")
	if err := os.Mkdir(workspace, 0755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
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
		t.Fatalf("Save error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if !strings.Contains(string(data), `reasoning = { reasoning = { effort = "high" } }`) {
		t.Fatalf("reasoning not saved as inline table: %s", string(data))
	}
	var loaded Config
	if err := LoadTOML(path, &loaded); err != nil {
		t.Fatalf("LoadTOML error: %v", err)
	}
	got := loaded.Models[0].Reasoning["reasoning"].(map[string]any)["effort"]
	if got != "high" {
		t.Fatalf("reasoning effort = %#v", got)
	}
}

func TestReasoningSavesInlineThinkingTable(t *testing.T) {
	dir := t.TempDir()
	workspace := filepath.Join(dir, "workspace")
	if err := os.Mkdir(workspace, 0755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
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
		t.Fatalf("Save error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	saved := string(data)
	if !strings.Contains(saved, `reasoning = { thinking = { type = "disabled" } }`) {
		t.Fatalf("reasoning not saved as inline table: %s", saved)
	}
	if strings.Contains(saved, "[models.reasoning") {
		t.Fatalf("reasoning saved as nested table: %s", saved)
	}
	var loaded Config
	if err := LoadTOML(path, &loaded); err != nil {
		t.Fatalf("LoadTOML error: %v", err)
	}
	thinking, ok := loaded.Models[0].Reasoning["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("thinking = %#v", loaded.Models[0].Reasoning["thinking"])
	}
	if got := thinking["type"]; got != "disabled" {
		t.Fatalf("thinking.type = %#v", got)
	}
}
