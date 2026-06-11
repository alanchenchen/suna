package builtin

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/alanchenchen/suna/internal/tools"
)

func fsRemove(path string, recursive bool, allowMissing bool, expectedKind string) tools.Result {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) && allowMissing {
			return fsChangeResult("remove", path, "", "missing", recursive, false, true, 0, 0)
		}
		return tools.ErrorResult(fmt.Sprintf("stat path: %s", err))
	}
	kind := fsKind(info)
	if err := checkExpectedKind(kind, expectedKind); err != nil {
		return tools.ErrorResult(err.Error())
	}
	entries, size := estimatePath(path, info)
	// 删除目录默认只允许空目录；递归删除必须由模型显式传参，便于 Guard 和 TUI 明确展示高风险。
	if kind == "dir" && recursive {
		err = os.RemoveAll(path)
	} else {
		err = os.Remove(path)
	}
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("remove path: %s", err))
	}
	return fsChangeResult("remove", path, "", kind, recursive, false, false, entries, size)
}

func prepareDestination(dst string, overwrite bool, parents bool, sourceKind string, recursive bool) (bool, error) {
	if parents {
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return false, fmt.Errorf("create destination parents: %s", err)
		}
	}
	if info, err := os.Lstat(dst); err == nil {
		if !overwrite {
			return false, fmt.Errorf("destination exists: %s", dst)
		}
		dstKind := fsKind(info)
		if dstKind != sourceKind {
			return false, fmt.Errorf("destination kind is %s, source kind is %s", dstKind, sourceKind)
		}
		// 覆盖目录会删除目录内容，必须同时显式 overwrite 和 recursive，避免 move/copy 误删大目录。
		if dstKind == "dir" && !recursive {
			return false, fmt.Errorf("overwriting a directory requires recursive=true")
		}
		if err := removeDestination(dst, dstKind); err != nil {
			return false, err
		}
		return true, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("stat destination: %s", err)
	}
	return false, nil
}

func removeDestination(path string, kind string) error {
	if kind == "dir" {
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove existing destination directory: %s", err)
		}
		return nil
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove existing destination: %s", err)
	}
	return nil
}
