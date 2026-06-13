package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchDefaultExcludeSkipsNestedBuildDirs(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg", "node_modules", "dep"), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "node_modules", "dep", "a.txt"), []byte("needle"), 0644); err != nil {
		t.Fatalf("WriteFile(excluded) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("needle"), 0644); err != nil {
		t.Fatalf("WriteFile(keep) error = %v", err)
	}

	res := Search{}.Execute(context.Background(), map[string]any{"path": root, "query": "needle"})
	if res.IsError {
		t.Fatalf("Search.Execute() error = %s", res.Error)
	}
	if strings.Contains(res.Content, "node_modules") {
		t.Fatalf("Search.Execute() content = %q, want default exclude to skip node_modules", res.Content)
	}
	if !strings.Contains(res.Content, "keep.txt") {
		t.Fatalf("Search.Execute() content = %q, want keep.txt match", res.Content)
	}
}

func TestSearchTruncatesAtMaxMatches(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x\nx\nx\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	res := Search{}.Execute(context.Background(), map[string]any{"path": root, "query": "x", "max_matches": float64(2)})
	if res.IsError {
		t.Fatalf("Search.Execute() error = %s", res.Error)
	}
	if !res.Truncated {
		t.Fatalf("Search.Execute().Truncated = false, want true")
	}
	if got := res.Metadata["matches"]; got != 2 {
		t.Fatalf("metadata matches = %#v, want 2", got)
	}
}

func TestSearchSkipsBinaryFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "bin.dat"), []byte{'n', 0, 'e', 'e', 'd', 'l', 'e'}, 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	res := Search{}.Execute(context.Background(), map[string]any{"path": root, "query": "needle"})
	if res.IsError {
		t.Fatalf("Search.Execute() error = %s", res.Error)
	}
	if strings.Contains(res.Content, "bin.dat") {
		t.Fatalf("Search.Execute() content = %q, want binary file skipped", res.Content)
	}
}

func TestSearchNoMatchesAddsDiagnosticsWithoutChangingMetadataContract(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("haystack"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	res := Search{}.Execute(context.Background(), map[string]any{"path": root, "query": "needle"})
	if res.IsError {
		t.Fatalf("Search.Execute() error = %s", res.Error)
	}
	if !strings.Contains(res.Content, "Search diagnostics:") {
		t.Fatalf("Search.Execute() content = %q, want diagnostics", res.Content)
	}
	if got := res.Metadata["kind"]; got != "search_result" {
		t.Fatalf("metadata kind = %#v, want search_result", got)
	}
	if got := res.Metadata["matches"]; got != 0 {
		t.Fatalf("metadata matches = %#v, want 0", got)
	}
	if _, ok := res.Metadata["files_scanned"]; !ok {
		t.Fatalf("metadata missing files_scanned: %#v", res.Metadata)
	}
}
