package mcptools

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/alanchenchen/suna/internal/mcp"
	"github.com/alanchenchen/suna/internal/tools"
)

func TestFormatResultSessionAttachmentIsolation(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	p := &Provider{}
	item := mcp.Content{Type: "image", Data: base64.StdEncoding.EncodeToString([]byte("image")), MimeType: "image/png", Name: "../../same.png"}
	var wg sync.WaitGroup
	for _, dir := range []string{first, second} {
		dir := dir
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := tools.WithExecutionContext(context.Background(), tools.ExecutionContext{AttachmentDir: dir})
			result := p.formatResult(ctx, "server", "tool", mcp.CallResult{Content: []mcp.Content{item}})
			if !strings.Contains(result, dir) {
				t.Errorf("result path does not belong to session: %s", result)
			}
		}()
	}
	wg.Wait()
	for _, dir := range []string{first, second} {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("session directory files = %d, err=%v", len(entries), err)
		}
		if entries[0].Name() != filepath.Base(entries[0].Name()) {
			t.Fatal("saved name escaped its session directory")
		}
		if mode, _ := entries[0].Info(); mode.Mode().Perm() != 0600 {
			t.Fatalf("attachment mode = %o, want 0600", mode.Mode().Perm())
		}
	}
}

func TestFormatResultDoesNotPersistWithoutContextOrForText(t *testing.T) {
	dir := t.TempDir()
	p := &Provider{}
	binary := mcp.Content{Type: "image", Data: base64.StdEncoding.EncodeToString([]byte("x")), MimeType: "image/png"}
	got := p.formatResult(context.Background(), "s", "t", mcp.CallResult{Content: []mcp.Content{binary}})
	if !strings.Contains(got, "omitted") || len(mustReadDir(t, dir)) != 0 {
		t.Fatalf("binary without context was persisted or wrong result: %s", got)
	}
	ctx := tools.WithExecutionContext(context.Background(), tools.ExecutionContext{AttachmentDir: dir})
	got = p.formatResult(ctx, "s", "t", mcp.CallResult{Content: []mcp.Content{{Type: "text", Text: "hello"}}})
	if got != "hello" || len(mustReadDir(t, dir)) != 0 {
		t.Fatalf("text result persisted or changed: %s", got)
	}
}

func TestSaveBinaryInvalidBase64AndDuplicateNames(t *testing.T) {
	dir := t.TempDir()
	ctx := tools.WithExecutionContext(context.Background(), tools.ExecutionContext{AttachmentDir: dir})
	p := &Provider{}
	bad := p.formatResult(ctx, "s", "t", mcp.CallResult{Content: []mcp.Content{{Type: "image", Data: "not-base64", MimeType: "image/png"}}})
	if !strings.Contains(bad, "decode failed") || len(mustReadDir(t, dir)) != 0 {
		t.Fatalf("invalid base64 result: %s", bad)
	}
	item := mcp.Content{Type: "image", Data: base64.StdEncoding.EncodeToString([]byte("x")), MimeType: "image/png", Name: "../../fixed.png"}
	p.formatResult(ctx, "s", "t", mcp.CallResult{Content: []mcp.Content{item}})
	p.formatResult(ctx, "s", "t", mcp.CallResult{Content: []mcp.Content{item}})
	entries := mustReadDir(t, dir)
	if len(entries) != 2 {
		t.Fatalf("duplicate MCP names overwrote files: %d", len(entries))
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "fixed.png")); !os.IsNotExist(err) {
		t.Fatal("malicious MCP name escaped attachment directory")
	}
}

func mustReadDir(t *testing.T, dir string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}
