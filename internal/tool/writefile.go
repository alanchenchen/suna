package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type WriteFile struct{}

func (WriteFile) Name() string { return "writefile" }
func (WriteFile) Description() string {
	return "Create or overwrite a file, optionally creating parent directories."
}
func (WriteFile) Category() Category { return Act }
func (WriteFile) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":        map[string]any{"type": "string", "description": "File path"},
			"content":     map[string]any{"type": "string", "description": "File content"},
			"create_dirs": map[string]any{"type": "boolean", "description": "Whether to create parent directories automatically"},
		},
		"required": []string{"path", "content"},
	}
}

func (WriteFile) Execute(ctx context.Context, params map[string]any) Result {
	path, _ := params["path"].(string)
	content, _ := params["content"].(string)
	if path == "" {
		return ErrorResult("path is required")
	}
	path = expandPath(path)

	createDirs := false
	if c, ok := params["create_dirs"].(bool); ok {
		createDirs = c
	}

	if isSystemPath(path) {
		return ErrorResult(fmt.Sprintf("cannot write to system directory: %s", path))
	}
	oldData, err := os.ReadFile(path)
	oldExists := true
	if err != nil {
		if os.IsNotExist(err) {
			oldExists = false
		} else {
			return ErrorResult(fmt.Sprintf("read existing file: %s", err))
		}
	}

	if createDirs {
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return ErrorResult(fmt.Sprintf("create directories: %s", err))
		}
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return ErrorResult(fmt.Sprintf("write file: %s", err))
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
