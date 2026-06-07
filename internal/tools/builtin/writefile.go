package builtin

import (
	"context"
	"fmt"
	"github.com/alanchenchen/suna/internal/tools"
	"os"
	"path/filepath"
	"strings"
)

type WriteFile struct{}

func (WriteFile) Spec() tools.Spec {
	return builtinSpec("writefile", "Create or overwrite a file, optionally creating parent directories.", tools.Act, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":        map[string]any{"type": "string", "description": "File path"},
			"content":     map[string]any{"type": "string", "description": "File content"},
			"create_dirs": map[string]any{"type": "boolean", "description": "Whether to create parent directories automatically"},
		},
		"required": []string{"path", "content"},
	})
}

func (WriteFile) Execute(ctx context.Context, params map[string]any) tools.Result {
	path, _ := params["path"].(string)
	content, _ := params["content"].(string)
	if path == "" {
		return tools.ErrorResult("path is required")
	}
	path = expandPath(path)

	createDirs := false
	if c, ok := params["create_dirs"].(bool); ok {
		createDirs = c
	}

	if isSystemPath(path) {
		return tools.ErrorResult(fmt.Sprintf("cannot write to system directory: %s", path))
	}
	oldData, err := os.ReadFile(path)
	oldExists := true
	if err != nil {
		if os.IsNotExist(err) {
			oldExists = false
		} else {
			return tools.ErrorResult(fmt.Sprintf("read existing file: %s", err))
		}
	}

	if createDirs {
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return tools.ErrorResult(fmt.Sprintf("create directories: %s", err))
		}
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return tools.ErrorResult(fmt.Sprintf("write file: %s", err))
	}

	operation := "created"
	if oldExists {
		operation = "updated"
		if string(oldData) == content {
			operation = "unchanged"
		}
	}
	return fileChangeResult(fileChange{Path: path, Operation: operation, OldContent: string(oldData), NewContent: content, OldExists: oldExists})
}

func isSystemPath(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	abs = strings.ToLower(abs)
	systemDirs := []string{"/etc/", "/usr/", "/system/", "c:\\windows", "c:\\program files"}
	for _, d := range systemDirs {
		if strings.HasPrefix(abs, d) {
			return true
		}
	}
	return false
}
