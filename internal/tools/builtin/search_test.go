package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchWorkspaceScopeSkipsDependencyDirs(t *testing.T) {
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

	res := Search{}.Execute(context.Background(), map[string]any{"path": root, "pattern": "needle"})
	if res.IsError {
		t.Fatalf("Search.Execute() error = %s", res.Error)
	}
	if strings.Contains(res.Content, "node_modules") {
		t.Fatalf("Search.Execute() content = %q, want workspace scope to skip node_modules", res.Content)
	}
	if !strings.Contains(res.Content, "keep.txt") {
		t.Fatalf("Search.Execute() content = %q, want keep.txt match", res.Content)
	}
}

func TestSearchDepsScopeIncludesDependencyDirs(t *testing.T) {
	root := t.TempDir()
	depDir := filepath.Join(root, "node_modules", "dep")
	if err := os.MkdirAll(depDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(depDir, "index.js"), []byte("export const needle = true\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	res := Search{}.Execute(context.Background(), map[string]any{"path": root, "pattern": "needle", "scope": "deps"})
	if res.IsError {
		t.Fatalf("Search.Execute() error = %s", res.Error)
	}
	if !strings.Contains(res.Content, "node_modules/dep/index.js") {
		t.Fatalf("Search.Execute() content = %q, want dependency match", res.Content)
	}
}

func TestSearchTruncatesAtLimit(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x\nx\nx\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	res := Search{}.Execute(context.Background(), map[string]any{"path": root, "pattern": "x", "limit": float64(2)})
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
	res := Search{}.Execute(context.Background(), map[string]any{"path": root, "pattern": "needle"})
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

	res := Search{}.Execute(context.Background(), map[string]any{"path": root, "pattern": "needle"})
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

func TestSearchSupportsFilePathAndContext(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.go")
	content := "package main\n\nfunc target() {\n\tprintln(\"needle\")\n}\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	res := Search{}.Execute(context.Background(), map[string]any{"path": path, "pattern": "needle", "mode": "content", "context": float64(1)})
	if res.IsError {
		t.Fatalf("Search.Execute() error = %s", res.Error)
	}
	for _, want := range []string{"a.go", "> 4 | println", "3 | func target", "5 | }"} {
		if !strings.Contains(res.Content, want) {
			t.Fatalf("Search.Execute() content = %q, want substring %q", res.Content, want)
		}
	}
}

func TestSearchAutoReturnsPathSymbolAndContentMatches(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal"), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	path := filepath.Join(root, "internal", "target.go")
	content := "package internal\n\nfunc Target() {}\n\nfunc Other() {\n\tTarget()\n}\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	res := Search{}.Execute(context.Background(), map[string]any{"path": root, "pattern": "target", "include": []any{"**/*.go"}})
	if res.IsError {
		t.Fatalf("Search.Execute() error = %s", res.Error)
	}
	for _, want := range []string{"Path matches:", "internal/target.go", "Symbol matches:", "> 3 | func Target() {}", "Content matches:", "> 6 | Target()"} {
		if !strings.Contains(res.Content, want) {
			t.Fatalf("Search.Execute() content = %q, want substring %q", res.Content, want)
		}
	}
	if got := res.Metadata["path_matches"]; got != 1 {
		t.Fatalf("metadata path_matches = %#v, want 1", got)
	}
	if got := res.Metadata["symbol_matches"]; got != 1 {
		t.Fatalf("metadata symbol_matches = %#v, want 1", got)
	}
	if got := res.Metadata["content_matches"]; got != 1 {
		t.Fatalf("metadata content_matches = %#v, want 1", got)
	}
}

func TestSearchPathModeMatchesRelativePath(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "model"), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "model", "anthropic.go"), []byte("package model\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	res := Search{}.Execute(context.Background(), map[string]any{"path": root, "pattern": "internal/model", "mode": "path"})
	if res.IsError {
		t.Fatalf("Search.Execute() error = %s", res.Error)
	}
	if !strings.Contains(res.Content, "internal/model/anthropic.go") {
		t.Fatalf("Search.Execute() content = %q, want relative path match", res.Content)
	}
	if got := res.Metadata["files_scanned"]; got != 0 {
		t.Fatalf("metadata files_scanned = %#v, want 0 for path-only search", got)
	}
}

func TestSearchSymbolModeFindsDocumentAndConfigStructure(t *testing.T) {
	root := t.TempDir()
	content := "# Agent Guide\n\nplain text mention Agent Guide\n\n[profile]\nname = suna\n"
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	res := Search{}.Execute(context.Background(), map[string]any{"path": root, "pattern": "Agent", "mode": "symbol"})
	if res.IsError {
		t.Fatalf("Search.Execute() error = %s", res.Error)
	}
	if !strings.Contains(res.Content, "> 1 | # Agent Guide") {
		t.Fatalf("Search.Execute() content = %q, want heading match", res.Content)
	}
	if strings.Contains(res.Content, "plain text mention") {
		t.Fatalf("Search.Execute() content = %q, want symbol search to skip plain content lines", res.Content)
	}

	res = Search{}.Execute(context.Background(), map[string]any{"path": root, "pattern": "profile", "mode": "symbol"})
	if res.IsError {
		t.Fatalf("Search.Execute() error = %s", res.Error)
	}
	if !strings.Contains(res.Content, "> 5 | [profile]") {
		t.Fatalf("Search.Execute() content = %q, want config section match", res.Content)
	}
}

func TestSearchTermsMatchLiteralAlternatives(t *testing.T) {
	root := t.TempDir()
	content := "alpha\ndescribe(\nmount(\n"
	if err := os.WriteFile(filepath.Join(root, "spec.txt"), []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	res := Search{}.Execute(context.Background(), map[string]any{"path": root, "terms": []any{"vitest", "describe(", "mount("}, "mode": "content"})
	if res.IsError {
		t.Fatalf("Search.Execute() error = %s", res.Error)
	}
	for _, want := range []string{"describe(", "mount(", "[matched: describe(", "[matched: mount("} {
		if !strings.Contains(res.Content, want) {
			t.Fatalf("Search.Execute() content = %q, want substring %q", res.Content, want)
		}
	}
}

func TestSearchLiteralPatternTreatsPunctuationAsText(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "spec.txt"), []byte("describe(\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	res := Search{}.Execute(context.Background(), map[string]any{"path": root, "pattern": "describe(", "mode": "content"})
	if res.IsError {
		t.Fatalf("Search.Execute() error = %s", res.Error)
	}
	if !strings.Contains(res.Content, "> 1 | describe(") {
		t.Fatalf("Search.Execute() content = %q, want literal punctuation match", res.Content)
	}
}

func TestSearchRegexCompileErrorSuggestsLiteralOrTerms(t *testing.T) {
	root := t.TempDir()
	res := Search{}.Execute(context.Background(), map[string]any{"path": root, "pattern": "describe(", "match": "regex"})
	if !res.IsError {
		t.Fatalf("Search.Execute() IsError = false, want true")
	}
	if !strings.Contains(res.Error, "use match=literal") {
		t.Fatalf("Search.Execute() error = %q, want literal suggestion", res.Error)
	}
}
