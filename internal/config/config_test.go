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
