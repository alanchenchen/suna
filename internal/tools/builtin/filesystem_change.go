package builtin

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/alanchenchen/suna/internal/tools"
)

func fsChangeResult(action string, path string, dst string, entryKind string, recursive bool, overwritten bool, missing bool, entries int, size int64) tools.Result {
	metadata := map[string]any{"kind": "fs_change", "action": action, "path": path, "entry_kind": entryKind, "recursive": recursive, "overwritten": overwritten, "entries": entries, "size": size}
	if dst != "" {
		metadata["destination"] = dst
	}
	if missing {
		metadata["missing"] = true
	}
	content := fmt.Sprintf("filesystem %s: %s", action, path)
	if dst != "" {
		content += fmt.Sprintf(" -> %s", dst)
	}
	if action == "remove" && !missing {
		content += " permanently deleted"
	}
	return tools.Result{Content: content, Metadata: metadata}
}

func estimatePath(path string, info os.FileInfo) (int, int64) {
	if fsKind(info) != "dir" {
		return 1, info.Size()
	}
	entries := 0
	var size int64
	_ = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil || p == path {
			return nil
		}
		entries++
		if info, err := d.Info(); err == nil && !d.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return entries, size
}
