package tool

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxReadSize   = 100 * 1024
	defaultLimit  = 2000
	maxBase64Size = 10 * 1024 * 1024
)

type ReadFile struct{}

func (ReadFile) Name() string { return "readfile" }
func (ReadFile) Description() string {
	return "读取文件内容。支持分页读取大文件，二进制文件返回 base64 编码。"
}
func (ReadFile) Category() Category { return Perceive }
func (ReadFile) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":     map[string]any{"type": "string", "description": "文件路径"},
			"offset":   map[string]any{"type": "integer", "description": "起始行号（1-indexed）"},
			"limit":    map[string]any{"type": "integer", "description": "最大返回行数"},
			"encoding": map[string]any{"type": "string", "enum": []string{"text", "base64"}, "description": "编码方式"},
		},
		"required": []string{"path"},
	}
}

func (ReadFile) Execute(ctx context.Context, params map[string]any) Result {
	path, _ := params["path"].(string)
	if path == "" {
		return ErrorResult("path is required")
	}
	path = expandPath(path)

	encoding := "text"
	if e, ok := params["encoding"].(string); ok {
		encoding = e
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrorResult(fmt.Sprintf("file not found: %s", path))
		}
		return ErrorResult(fmt.Sprintf("stat file: %s", err))
	}

	if info.IsDir() {
		return ErrorResult(fmt.Sprintf("path is a directory: %s", path))
	}

	if encoding == "base64" {
		return readBase64(path, info.Size())
	}
	return readText(path, params)
}

func readBase64(path string, size int64) Result {
	if size > maxBase64Size {
		return ErrorResult(fmt.Sprintf("file too large for base64: %d bytes (max %d)", size, maxBase64Size))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ErrorResult(fmt.Sprintf("read file: %s", err))
	}
	return TextResult(base64.StdEncoding.EncodeToString(data))
}

func readText(path string, params map[string]any) Result {
	data, err := os.ReadFile(path)
	if err != nil {
		return ErrorResult(fmt.Sprintf("read file: %s", err))
	}

	if len(data) > maxReadSize {
		data = data[:maxReadSize]
	}

	lines := strings.Split(string(data), "\n")
	totalLines := len(lines)

	offset := 1
	if o, ok := params["offset"].(float64); ok && int(o) > 0 {
		offset = int(o)
	}
	limit := defaultLimit
	if l, ok := params["limit"].(float64); ok && int(l) > 0 {
		limit = int(l)
	}

	if offset > totalLines {
		return TextResult(fmt.Sprintf("offset %d exceeds total lines %d", offset, totalLines))
	}

	end := offset + limit - 1
	if end > totalLines {
		end = totalLines
	}

	selected := lines[offset-1 : end]
	var sb strings.Builder
	for i, line := range selected {
		sb.WriteString(fmt.Sprintf("%d: %s\n", offset+i, line))
	}

	truncated := totalLines > end
	content := sb.String()
	if truncated {
		content += fmt.Sprintf("\n... (truncated, %d lines total. Use offset=%d to read more)", totalLines, end+1)
	}

	return Result{Content: content, Truncated: truncated}
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}
