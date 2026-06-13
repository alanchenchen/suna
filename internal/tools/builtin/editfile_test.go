package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditFileAppliesMultipleEditsAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\ngamma\nbeta\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	res := EditFile{}.Execute(context.Background(), map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{"old_string": "alpha", "new_string": "ALPHA", "expected_replacements": float64(1)},
			map[string]any{"old_string": "beta", "new_string": "BETA", "mode": "nth", "occurrence": float64(2), "expected_replacements": float64(1)},
		},
	})
	if res.IsError {
		t.Fatalf("EditFile.Execute() error = %s", res.Error)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got, want := string(data), "ALPHA\nbeta\ngamma\nBETA\n"; got != want {
		t.Fatalf("file content = %q, want %q", got, want)
	}
	if got := res.Metadata["replacements"]; got != 2 {
		t.Fatalf("metadata replacements = %#v, want 2", got)
	}
}

func TestEditFileReplacesAllMatchesWithModeAll(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.txt")
	if err := os.WriteFile(path, []byte("beta beta"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	res := EditFile{}.Execute(context.Background(), map[string]any{
		"path":  path,
		"edits": []any{map[string]any{"old_string": "beta", "new_string": "BETA", "mode": "all", "expected_replacements": float64(2)}},
	})
	if res.IsError {
		t.Fatalf("EditFile.Execute() error = %s", res.Error)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got, want := string(data), "BETA BETA"; got != want {
		t.Fatalf("file content = %q, want %q", got, want)
	}
}

func TestEditFileDoesNotWriteWhenAnyEditFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.txt")
	original := "alpha\nbeta\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	res := EditFile{}.Execute(context.Background(), map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{"old_string": "alpha", "new_string": "ALPHA"},
			map[string]any{"old_string": "missing", "new_string": "MISSING"},
		},
	})
	if !res.IsError {
		t.Fatalf("EditFile.Execute().IsError = false, want true")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != original {
		t.Fatalf("file content = %q, want unchanged %q", string(data), original)
	}
}

func TestEditFileRequiresModeForAmbiguousEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.txt")
	if err := os.WriteFile(path, []byte("beta beta"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	res := EditFile{}.Execute(context.Background(), map[string]any{"path": path, "edits": []any{map[string]any{"old_string": "beta", "new_string": "BETA"}}})
	if !res.IsError || !strings.Contains(res.Error, "mode=\"nth\"") {
		t.Fatalf("EditFile.Execute() = %#v, want ambiguous match error with mode hint", res)
	}
}
