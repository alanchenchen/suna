package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

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
	// Windows 的 FileMode.Perm 不表示 ACL，不能用 POSIX mode 数值判断目录私有性。
	if runtime.GOOS != "windows" {
		if mode := info.Mode().Perm(); mode != 0700 {
			t.Fatalf("workdir mode = %o, want 700", mode)
		}
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
	client, err := r.openServer(context.Background(), "explicit", sc)
	if client != nil {
		client.Close()
	}
	if err == nil {
		t.Fatal("openServer() unexpectedly succeeded")
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

func TestRuntimeStartsEnabledServersConcurrently(t *testing.T) {
	cfg := config.MCPConfig{Servers: map[string]config.MCPServerConfig{
		"a": {Enabled: true},
		"b": {Enabled: true},
	}}
	r := NewRuntime(cfg)
	entered := make(chan string, 2)
	release := make(chan struct{})
	r.openServerFn = func(ctx context.Context, name string, _ config.MCPServerConfig) (*Client, error) {
		entered <- name
		select {
		case <-release:
			return &Client{id: name, tools: []Tool{{Server: name, Name: "tool"}}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	r.SetCatalogSync(func(context.Context) error { return nil })
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("enabled MCP servers did not start concurrently")
		}
	}
	close(release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestDisableWinsOverInFlightStart(t *testing.T) {
	cfg := config.MCPConfig{Servers: map[string]config.MCPServerConfig{"slow": {Enabled: true}}}
	r := NewRuntime(cfg)
	entered := make(chan struct{})
	r.openServerFn = func(ctx context.Context, name string, _ config.MCPServerConfig) (*Client, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	r.SetCatalogSync(func(context.Context) error { return nil })
	_ = r.Start(context.Background())
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("MCP start did not begin")
	}
	if err := r.SetActive(context.Background(), "slow", false); err != nil {
		t.Fatalf("SetActive(false) error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	status := r.Status(context.Background())
	if len(status) != 1 || status[0].State != ServerStateDisabled {
		t.Fatalf("Status() = %#v, want disabled", status)
	}
}

func TestActiveIsPublishedAfterCatalogSync(t *testing.T) {
	cfg := config.MCPConfig{Servers: map[string]config.MCPServerConfig{"one": {Enabled: true}}}
	r := NewRuntime(cfg)
	releaseCatalog := make(chan struct{})
	changes := make(chan ServerInfo, 4)
	r.openServerFn = func(context.Context, string, config.MCPServerConfig) (*Client, error) {
		return &Client{id: "one", tools: []Tool{{Server: "one", Name: "tool"}}}, nil
	}
	r.SetCatalogSync(func(context.Context) error {
		<-releaseCatalog
		return nil
	})
	r.SetOnChange(func(info ServerInfo) { changes <- info })
	_ = r.Start(context.Background())
	select {
	case info := <-changes:
		if info.State != ServerStateStarting {
			t.Fatalf("first state = %q, want starting", info.State)
		}
	case <-time.After(time.Second):
		t.Fatal("starting state not published")
	}
	select {
	case info := <-changes:
		t.Fatalf("state published before catalog sync: %#v", info)
	case <-time.After(30 * time.Millisecond):
	}
	close(releaseCatalog)
	select {
	case info := <-changes:
		if info.State != ServerStateActive || info.ToolCount != 1 {
			t.Fatalf("committed state = %#v, want active with one tool", info)
		}
	case <-time.After(time.Second):
		t.Fatal("active state not published")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestSetConfigCancelsOldGenerationAndStartsNewConfig(t *testing.T) {
	oldConfig := config.MCPServerConfig{Enabled: true, Command: "old"}
	newConfig := config.MCPServerConfig{Enabled: true, Command: "new"}
	r := NewRuntime(config.MCPConfig{Servers: map[string]config.MCPServerConfig{"one": oldConfig}})
	oldEntered := make(chan struct{})
	oldCanceled := make(chan struct{})
	newStarted := make(chan struct{})
	r.openServerFn = func(ctx context.Context, name string, sc config.MCPServerConfig) (*Client, error) {
		switch sc.Command {
		case "old":
			close(oldEntered)
			<-ctx.Done()
			close(oldCanceled)
			return nil, ctx.Err()
		case "new":
			close(newStarted)
			return &Client{id: name, cfg: sc, tools: []Tool{{Server: name, Name: "new-tool"}}}, nil
		default:
			return nil, fmt.Errorf("unexpected command %q", sc.Command)
		}
	}
	r.SetCatalogSync(func(context.Context) error { return nil })
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case <-oldEntered:
	case <-time.After(time.Second):
		t.Fatal("old generation did not start")
	}
	r.SetConfig(config.MCPConfig{Servers: map[string]config.MCPServerConfig{"one": newConfig}})
	select {
	case <-oldCanceled:
	case <-time.After(time.Second):
		t.Fatal("old generation was not canceled")
	}
	select {
	case <-newStarted:
	case <-time.After(time.Second):
		t.Fatal("new generation did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestReloadCatalogFailureKeepsOldClientActive(t *testing.T) {
	cfg := config.MCPConfig{Servers: map[string]config.MCPServerConfig{"one": {Enabled: true}}}
	r := NewRuntime(cfg)
	old := &Client{id: "one", tools: []Tool{{Server: "one", Name: "old-tool"}}}
	r.clients["one"] = old
	r.states["one"] = ServerStateActive
	r.openServerFn = func(context.Context, string, config.MCPServerConfig) (*Client, error) {
		return &Client{id: "one", tools: []Tool{{Server: "one", Name: "new-tool"}}}, nil
	}
	r.SetCatalogSync(func(context.Context) error { return fmt.Errorf("catalog unavailable") })
	if err := r.ReloadServer(context.Background(), "one"); err == nil {
		t.Fatal("ReloadServer() error = nil, want catalog failure")
	}
	r.mu.RLock()
	gotClient := r.clients["one"]
	r.mu.RUnlock()
	if gotClient != old {
		t.Fatal("ReloadServer() did not restore old client")
	}
	status := r.Status(context.Background())
	if len(status) != 1 || status[0].State != ServerStateActive || !strings.Contains(status[0].Error, "catalog unavailable") {
		t.Fatalf("Status() = %#v, want active old client with catalog error", status)
	}
}

func TestDisableCatalogFailureRestoresPreviousDisabledConfig(t *testing.T) {
	cfg := config.MCPConfig{Servers: map[string]config.MCPServerConfig{"one": {Enabled: false}}}
	r := NewRuntime(cfg)
	r.SetCatalogSync(func(context.Context) error { return fmt.Errorf("catalog unavailable") })
	if err := r.SetActive(context.Background(), "one", false); err == nil {
		t.Fatal("SetActive(false) error = nil, want catalog failure")
	}
	if got := r.Config().Servers["one"].Enabled; got {
		t.Fatal("disabled config became enabled after rollback")
	}
	status := r.Status(context.Background())
	if len(status) != 1 || status[0].State != ServerStateDisabled {
		t.Fatalf("Status() = %#v, want disabled", status)
	}
}
