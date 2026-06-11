package builtin

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/alanchenchen/suna/internal/tools"
)

func fsCopy(src string, dst string, overwrite bool, parents bool, recursive bool, expectedKind string) tools.Result {
	info, err := os.Lstat(src)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("stat source: %s", err))
	}
	kind := fsKind(info)
	if err := checkExpectedKind(kind, expectedKind); err != nil {
		return tools.ErrorResult(err.Error())
	}
	if kind == "dir" && !recursive {
		return tools.ErrorResult("copying a directory requires recursive=true")
	}
	overwritten, err := prepareDestination(dst, overwrite, parents, kind, recursive)
	if err != nil {
		return tools.ErrorResult(err.Error())
	}
	entries, size, err := copyPath(src, dst, info)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("copy path: %s", err))
	}
	return fsChangeResult("copy", src, dst, kind, recursive, overwritten, false, entries, size)
}

func copyPath(src string, dst string, info os.FileInfo) (int, int64, error) {
	kind := fsKind(info)
	if kind == "symlink" {
		target, err := os.Readlink(src)
		if err != nil {
			return 0, 0, err
		}
		return 1, 0, os.Symlink(target, dst)
	}
	if kind != "dir" {
		size, err := copyFile(src, dst, info.Mode())
		return 1, size, err
	}
	entries := 0
	var size int64
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if rel == "." {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		entryInfo, err := d.Info()
		if err != nil {
			return err
		}
		entries++
		if d.IsDir() {
			return os.MkdirAll(target, entryInfo.Mode().Perm())
		}
		if entryInfo.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		written, err := copyFile(path, target, entryInfo.Mode())
		size += written
		return err
	})
	return entries, size, err
}

func copyFile(src string, dst string, mode os.FileMode) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
	if err != nil {
		return 0, err
	}
	written, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return written, copyErr
	}
	return written, closeErr
}
