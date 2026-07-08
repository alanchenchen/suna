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
	return builtinSpec("writefile", "Create, append, or replace an entire file. Use create_dirs=true to auto-create parent directories. Use editfile for targeted edits.", tools.Act, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":            map[string]any{"type": "string", "description": "File path"},
			"content":         map[string]any{"type": "string", "description": "File content"},
			"mode":            map[string]any{"type": "string", "enum": []string{"overwrite", "create_new", "append"}, "description": "Write mode: create_new, append, or overwrite. Default overwrite"},
			"create_dirs":     map[string]any{"type": "boolean", "description": "Auto-create parent directories"},
			"expected_sha256": map[string]any{"type": "string", "description": "Expected existing file SHA-256 before writing"},
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
	path = expandPathWithContext(ctx, path)

	createDirs := false
	if c, ok := params["create_dirs"].(bool); ok {
		createDirs = c
	}
	mode, _ := params["mode"].(string)
	if mode == "" {
		mode = "overwrite"
	}
	if mode != "overwrite" && mode != "create_new" && mode != "append" {
		return tools.ErrorResult("mode must be overwrite, create_new, or append")
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
	if mode == "create_new" && oldExists {
		return tools.ErrorResult(fmt.Sprintf("file already exists: %s", path))
	}
	if expected, _ := params["expected_sha256"].(string); expected != "" && oldExists {
		actual, err := fileSHA256(path)
		if err != nil {
			return tools.ErrorResult(fmt.Sprintf("hash existing file: %s", err))
		}
		if !sameString(actual, expected) {
			return tools.ErrorResult("existing file sha256 does not match expected_sha256")
		}
	}

	if createDirs {
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return tools.ErrorResult(fmt.Sprintf("create directories: %s", err))
		}
	}

	newContent := content
	if mode == "append" && oldExists {
		newContent = string(oldData) + content
	}
	if err := writeFileAtomic(path, []byte(newContent), oldExists); err != nil {
		return tools.ErrorResult(fmt.Sprintf("write file: %s", err))
	}

	operation := "created"
	if oldExists {
		operation = "updated"
		if mode == "append" {
			operation = "appended"
		}
		if string(oldData) == newContent {
			operation = "unchanged"
		}
	}
	return fileChangeResult(fileChange{Path: path, Operation: operation, OldContent: string(oldData), NewContent: newContent, OldExists: oldExists})
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

func writeFileAtomic(path string, data []byte, preserveExisting bool) error {
	mode := os.FileMode(0644)
	if preserveExisting {
		if info, err := os.Stat(path); err == nil {
			mode = info.Mode().Perm()
		}
	}
	dir := filepath.Dir(path)
	// 先写同目录临时文件再 rename，避免进程中断造成目标文件半写入。
	tmp, err := os.CreateTemp(dir, ".suna-write-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
