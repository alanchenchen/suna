package builtin

import (
	"context"
	"fmt"
	"os"

	"github.com/alanchenchen/suna/internal/tools"
)

type FileSystem struct{}

func (FileSystem) Spec() tools.Spec {
	return builtinSpec("filesystem", "Manage filesystem paths: stat, mkdir, move, copy, or permanently remove files and directories. Prefer writefile with create_dirs=true to create new files.", tools.Act, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action":        map[string]any{"type": "string", "enum": []string{"stat", "mkdir", "move", "copy", "remove"}, "description": "Filesystem action"},
			"path":          map[string]any{"type": "string", "description": "Source or target path"},
			"destination":   map[string]any{"type": "string", "description": "Destination path for move or copy"},
			"parents":       map[string]any{"type": "boolean", "description": "Create parent directories when needed"},
			"overwrite":     map[string]any{"type": "boolean", "description": "Allow overwriting destination"},
			"recursive":     map[string]any{"type": "boolean", "description": "Allow recursive directory copy or permanent removal"},
			"allow_missing": map[string]any{"type": "boolean", "description": "Treat missing path as success"},
			"expected_kind": map[string]any{"type": "string", "enum": []string{"any", "file", "dir", "symlink"}, "description": "Expected path kind"},
		},
		"required": []string{"action", "path"},
	})
}

func (FileSystem) Execute(ctx context.Context, params map[string]any) tools.Result {
	action, _ := params["action"].(string)
	path, _ := params["path"].(string)
	if action == "" {
		return tools.ErrorResult("action is required")
	}
	if path == "" {
		return tools.ErrorResult("path is required")
	}
	path = expandPathWithContext(ctx, path)
	if isSystemPath(path) && action != "stat" {
		return tools.ErrorResult(fmt.Sprintf("cannot modify system path: %s", path))
	}

	allowMissing, _ := params["allow_missing"].(bool)
	expectedKind, _ := params["expected_kind"].(string)
	if expectedKind == "" {
		expectedKind = "any"
	}

	switch action {
	case "stat":
		return fsStat(path, allowMissing, expectedKind)
	case "mkdir":
		parents, _ := params["parents"].(bool)
		return fsMkdir(path, parents)
	case "move", "copy":
		dst, _ := params["destination"].(string)
		if dst == "" {
			return tools.ErrorResult("destination is required")
		}
		dst = expandPathWithContext(ctx, dst)
		if isSystemPath(dst) {
			return tools.ErrorResult(fmt.Sprintf("cannot write to system path: %s", dst))
		}
		overwrite, _ := params["overwrite"].(bool)
		parents, _ := params["parents"].(bool)
		recursive, _ := params["recursive"].(bool)
		if action == "move" {
			return fsMove(path, dst, overwrite, parents, recursive, expectedKind)
		}
		return fsCopy(path, dst, overwrite, parents, recursive, expectedKind)
	case "remove":
		recursive, _ := params["recursive"].(bool)
		return fsRemove(path, recursive, allowMissing, expectedKind)
	default:
		return tools.ErrorResult(fmt.Sprintf("unsupported filesystem action: %s", action))
	}
}

func fsStat(path string, allowMissing bool, expectedKind string) tools.Result {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) && allowMissing {
			return tools.TextResult(fmt.Sprintf("path missing: %s", path))
		}
		return tools.ErrorResult(fmt.Sprintf("stat path: %s", err))
	}
	kind := fsKind(info)
	if err := checkExpectedKind(kind, expectedKind); err != nil {
		return tools.ErrorResult(err.Error())
	}
	return tools.Result{Content: fmt.Sprintf("%s %s %dB", kind, path, info.Size()), Metadata: map[string]any{"kind": "fs_stat", "path": path, "entry_kind": kind, "size": info.Size()}}
}

func fsMkdir(path string, parents bool) tools.Result {
	var err error
	if parents {
		err = os.MkdirAll(path, 0755)
	} else {
		err = os.Mkdir(path, 0755)
	}
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("create directory: %s", err))
	}
	return fsChangeResult("mkdir", path, "", "dir", false, false, false, 1, 0)
}

func fsMove(src string, dst string, overwrite bool, parents bool, recursive bool, expectedKind string) tools.Result {
	info, err := os.Lstat(src)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("stat source: %s", err))
	}
	kind := fsKind(info)
	if err := checkExpectedKind(kind, expectedKind); err != nil {
		return tools.ErrorResult(err.Error())
	}
	overwritten, err := prepareDestination(dst, overwrite, parents, kind, recursive)
	if err != nil {
		return tools.ErrorResult(err.Error())
	}
	if err := os.Rename(src, dst); err != nil {
		return tools.ErrorResult(fmt.Sprintf("move path: %s", err))
	}
	return fsChangeResult("move", src, dst, kind, false, overwritten, false, 1, info.Size())
}
