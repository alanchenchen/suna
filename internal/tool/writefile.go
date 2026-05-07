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
	return "创建或覆盖文件。支持自动创建父目录。"
}
func (WriteFile) Category() Category { return Act }
func (WriteFile) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":        map[string]any{"type": "string", "description": "文件路径"},
			"content":     map[string]any{"type": "string", "description": "文件内容"},
			"create_dirs": map[string]any{"type": "boolean", "description": "是否自动创建父目录"},
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

	if createDirs {
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return ErrorResult(fmt.Sprintf("create directories: %s", err))
		}
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return ErrorResult(fmt.Sprintf("write file: %s", err))
	}

	return TextResult(fmt.Sprintf("wrote %d bytes to %s", len(content), path))
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
