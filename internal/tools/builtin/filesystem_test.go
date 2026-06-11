package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileSystemRemoveDirectoryRequiresRecursive(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	res := FileSystem{}.Execute(context.Background(), map[string]any{"action": "remove", "path": dir})
	if !res.IsError {
		t.Fatalf("remove non-empty dir IsError = false, want true")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("directory was removed without recursive=true")
	}

	res = FileSystem{}.Execute(context.Background(), map[string]any{"action": "remove", "path": dir, "recursive": true, "expected_kind": "dir"})
	if res.IsError {
		t.Fatalf("recursive remove error = %s", res.Error)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("directory still exists after recursive remove: %v", err)
	}
	if got := res.Metadata["kind"]; got != "fs_change" {
		t.Fatalf("metadata kind = %#v, want fs_change", got)
	}
}

func TestFileSystemOverwriteRejectsKindMismatch(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src.txt")
	dst := filepath.Join(root, "dst")
	if err := os.WriteFile(src, []byte("a"), 0644); err != nil {
		t.Fatalf("WriteFile(src) error = %v", err)
	}
	if err := os.Mkdir(dst, 0755); err != nil {
		t.Fatalf("Mkdir(dst) error = %v", err)
	}

	res := FileSystem{}.Execute(context.Background(), map[string]any{"action": "copy", "path": src, "destination": dst, "overwrite": true})
	if !res.IsError || !strings.Contains(res.Error, "destination kind") {
		t.Fatalf("copy over mismatched kind result = %#v, want kind mismatch error", res)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("destination directory removed on kind mismatch: %v", err)
	}
}

func TestFileSystemCopySymlinkPreservesLink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.txt")
	link := filepath.Join(root, "link.txt")
	copyPath := filepath.Join(root, "copy.txt")
	if err := os.WriteFile(target, []byte("a"), 0644); err != nil {
		t.Fatalf("WriteFile(target) error = %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	res := FileSystem{}.Execute(context.Background(), map[string]any{"action": "copy", "path": link, "destination": copyPath, "expected_kind": "symlink"})
	if res.IsError {
		t.Fatalf("copy symlink error = %s", res.Error)
	}
	info, err := os.Lstat(copyPath)
	if err != nil {
		t.Fatalf("Lstat(copy) error = %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("copy mode = %v, want symlink", info.Mode())
	}
}
