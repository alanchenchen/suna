package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileReturnsFileChangeMetadataForCreate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	res := WriteFile{}.Execute(context.Background(), map[string]any{"path": path, "content": "one\ntwo\n"})
	if res.IsError {
		t.Fatalf("writefile error: %s", res.Error)
	}
	if got := res.Content; got != "file created: "+path+" (+2 -0, 8B)" {
		t.Fatalf("content = %q", got)
	}
	assertMetadata(t, res.Metadata, "created", path, 2, 0, 0)
	if _, ok := res.Metadata["size_before"]; ok {
		t.Fatalf("new file metadata unexpectedly has size_before: %#v", res.Metadata)
	}
}

func TestWriteFileReturnsFileChangeMetadataForUpdateAndUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	res := WriteFile{}.Execute(context.Background(), map[string]any{"path": path, "content": "a\nb2\nc\nd\n"})
	if res.IsError {
		t.Fatalf("writefile error: %s", res.Error)
	}
	if !strings.Contains(res.Content, "file updated: "+path+" (+2 -1") {
		t.Fatalf("unexpected content: %q", res.Content)
	}
	assertMetadata(t, res.Metadata, "updated", path, 2, 1, 0)

	res = WriteFile{}.Execute(context.Background(), map[string]any{"path": path, "content": "a\nb2\nc\nd\n"})
	if res.IsError {
		t.Fatalf("writefile unchanged error: %s", res.Error)
	}
	assertMetadata(t, res.Metadata, "unchanged", path, 0, 0, 0)
}

func TestEditFileReturnsFileChangeMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\ngamma\n"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	res := EditFile{}.Execute(context.Background(), map[string]any{"path": path, "old_string": "beta\n", "new_string": "beta2\ndelta\n"})
	if res.IsError {
		t.Fatalf("editfile error: %s", res.Error)
	}
	if !strings.Contains(res.Content, "1 replacement") {
		t.Fatalf("expected replacement count in content, got %q", res.Content)
	}
	assertMetadata(t, res.Metadata, "updated", path, 2, 1, 1)
}

func assertMetadata(t *testing.T, metadata map[string]any, operation string, path string, added, removed, replacements int) {
	t.Helper()
	if metadata["kind"] != "file_change" {
		t.Fatalf("metadata kind = %#v, want file_change", metadata["kind"])
	}
	if metadata["operation"] != operation || metadata["path"] != path {
		t.Fatalf("metadata = %#v, want operation=%s path=%s", metadata, operation, path)
	}
	if metadata["added_lines"] != added || metadata["removed_lines"] != removed {
		t.Fatalf("metadata line delta = %#v/%#v, want %d/%d", metadata["added_lines"], metadata["removed_lines"], added, removed)
	}
	if replacements > 0 && metadata["replacements"] != replacements {
		t.Fatalf("metadata replacements = %#v, want %d", metadata["replacements"], replacements)
	}
}
