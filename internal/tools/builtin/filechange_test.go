package builtin

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
		t.Fatalf("WriteFile.Execute() error = %s", res.Error)
	}
	wantContent := "file created: " + path + " (+2 -0, 8B)"
	if got := res.Content; got != wantContent {
		t.Fatalf("WriteFile.Execute().Content = %q, want %q", got, wantContent)
	}
	assertMetadata(t, res.Metadata, "created", path, 2, 0, 0)
	if _, ok := res.Metadata["size_before"]; ok {
		t.Fatalf("metadata[size_before] exists in %#v, want absent for new file", res.Metadata)
	}
}

func TestWriteFileReturnsFileChangeMetadataForUpdateAndUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.txt")
	writeFileForToolTest(t, path, "a\nb\nc\n")

	res := WriteFile{}.Execute(context.Background(), map[string]any{"path": path, "content": "a\nb2\nc\nd\n"})
	if res.IsError {
		t.Fatalf("WriteFile.Execute() update error = %s", res.Error)
	}
	wantContent := "file updated: " + path + " (+2 -1"
	if !strings.Contains(res.Content, wantContent) {
		t.Fatalf("WriteFile.Execute() update content = %q, want substring %q", res.Content, wantContent)
	}
	assertMetadata(t, res.Metadata, "updated", path, 2, 1, 0)

	res = WriteFile{}.Execute(context.Background(), map[string]any{"path": path, "content": "a\nb2\nc\nd\n"})
	if res.IsError {
		t.Fatalf("WriteFile.Execute() unchanged error = %s", res.Error)
	}
	assertMetadata(t, res.Metadata, "unchanged", path, 0, 0, 0)
}

func TestEditFileReturnsFileChangeMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.txt")
	writeFileForToolTest(t, path, "alpha\nbeta\ngamma\n")

	res := EditFile{}.Execute(context.Background(), map[string]any{"path": path, "old_string": "beta\n", "new_string": "beta2\ndelta\n"})
	if res.IsError {
		t.Fatalf("EditFile.Execute() error = %s", res.Error)
	}
	if !strings.Contains(res.Content, "1 replacement") {
		t.Fatalf("EditFile.Execute().Content = %q, want replacement count", res.Content)
	}
	assertMetadata(t, res.Metadata, "updated", path, 2, 1, 1)
}

func assertMetadata(t *testing.T, metadata map[string]any, operation string, path string, added, removed, replacements int) {
	t.Helper()
	if got := metadata["kind"]; got != "file_change" {
		t.Fatalf("metadata[kind] = %#v, want %q", got, "file_change")
	}
	if got := metadata["operation"]; got != operation {
		t.Fatalf("metadata[operation] = %#v, want %q", got, operation)
	}
	if got := metadata["path"]; got != path {
		t.Fatalf("metadata[path] = %#v, want %q", got, path)
	}
	if got := metadata["added_lines"]; got != added {
		t.Fatalf("metadata[added_lines] = %#v, want %d", got, added)
	}
	if got := metadata["removed_lines"]; got != removed {
		t.Fatalf("metadata[removed_lines] = %#v, want %d", got, removed)
	}
	if replacements > 0 {
		if got := metadata["replacements"]; got != replacements {
			t.Fatalf("metadata[replacements] = %#v, want %d", got, replacements)
		}
	}
}

func writeFileForToolTest(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}
