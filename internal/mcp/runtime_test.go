package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/config"
)

func TestDefaultServerWorkdirIsStableAndPrivate(t *testing.T) {
	root := t.TempDir()
	r := NewRuntime(config.MCPConfig{})
	r.defaultWorkdirsDir = filepath.Join(root, "workdirs")

	first, err := r.defaultServerWorkdir("my server/prod")
	if err != nil {
		t.Fatalf("defaultServerWorkdir() error = %v", err)
	}
	second, err := r.defaultServerWorkdir("my server/prod")
	if err != nil {
		t.Fatalf("defaultServerWorkdir() second error = %v", err)
	}
	if first != second {
		t.Fatalf("workdir changed: %q != %q", first, second)
	}
	if filepath.Dir(first) != filepath.Join(root, "workdirs") || strings.Contains(filepath.Base(first), "/") {
		t.Fatalf("unsafe workdir path: %q", first)
	}
	info, err := os.Stat(first)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0700 {
		t.Fatalf("workdir mode = %o, want 700", mode)
	}
	other, err := r.defaultServerWorkdir("my server-prod")
	if err != nil {
		t.Fatalf("defaultServerWorkdir() other error = %v", err)
	}
	if other == first {
		t.Fatal("different server IDs collided")
	}
}

func TestDefaultServerWorkdirNameIsBounded(t *testing.T) {
	r := NewRuntime(config.MCPConfig{})
	r.defaultWorkdirsDir = t.TempDir()
	name := strings.Repeat("workspace-", 100)
	dir, err := r.defaultServerWorkdir(name)
	if err != nil {
		t.Fatalf("defaultServerWorkdir() error = %v", err)
	}
	if got := len(filepath.Base(dir)); got > 64 {
		t.Fatalf("workdir basename length = %d, want <= 64", got)
	}
}

func TestExplicitServerWorkdirDoesNotCreateDefaultRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workdirs")
	r := NewRuntime(config.MCPConfig{})
	r.defaultWorkdirsDir = root
	sc := config.MCPServerConfig{CWD: t.TempDir(), Command: "command-that-does-not-exist"}
	if err := r.startServer(context.Background(), "explicit", sc); err == nil {
		t.Fatal("startServer() unexpectedly succeeded")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("default root exists after explicit CWD start, err = %v", err)
	}
}
func TestSetActiveFalseUpdatesConfigSnapshot(t *testing.T) {
	r := NewRuntime(config.MCPConfig{Servers: map[string]config.MCPServerConfig{
		"github": {Enabled: true, Transport: TransportStdio, Command: "npx", Args: []string{"server"}},
	}})

	if err := r.SetActive(context.Background(), "github", false); err != nil {
		t.Fatalf("SetActive() error = %v", err)
	}
	cfg := r.Config()
	if got := cfg.Servers["github"].Enabled; got {
		t.Fatalf("Config().Servers[github].Enabled = %t, want false", got)
	}

	cfg.Servers["github"] = config.MCPServerConfig{Enabled: true}
	if got := r.Config().Servers["github"].Enabled; got {
		t.Fatalf("Config() returned mutable snapshot, enabled = %t, want false", got)
	}
}
