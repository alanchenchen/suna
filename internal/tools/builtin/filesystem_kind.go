package builtin

import (
	"fmt"
	"os"
)

func fsKind(info os.FileInfo) string {
	if info.Mode()&os.ModeSymlink != 0 {
		return "symlink"
	}
	if info.IsDir() {
		return "dir"
	}
	return "file"
}

func checkExpectedKind(kind string, expected string) error {
	if expected == "" || expected == "any" || expected == kind {
		return nil
	}
	return fmt.Errorf("path kind is %s, expected %s", kind, expected)
}
