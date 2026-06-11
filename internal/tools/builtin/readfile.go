package builtin

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"github.com/alanchenchen/suna/internal/tools"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultReadLineLimit = 2000
	maxReadLineLimit     = 5000
	maxReadResultBytes   = 100 * 1024
	maxReadLineBytes     = 32 * 1024
	maxBase64Size        = 10 * 1024 * 1024
)

type ReadFile struct{}

func (ReadFile) Spec() tools.Spec {
	return builtinSpec("readfile", "Read file contents with line ranges, tail, and base64 support.", tools.Perceive, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":       map[string]any{"type": "string", "description": "File path"},
			"start_line": map[string]any{"type": "integer", "description": "Starting line number, 1-indexed"},
			"line_count": map[string]any{"type": "integer", "description": "Maximum lines to return, default 2000, max 5000"},
			"tail_lines": map[string]any{"type": "integer", "description": "Read the last N lines"},
			"encoding":   map[string]any{"type": "string", "enum": []string{"text", "base64"}, "description": "Output encoding"},
		},
		"required": []string{"path"},
	})
}

func (ReadFile) Execute(ctx context.Context, params map[string]any) tools.Result {
	path, _ := params["path"].(string)
	if path == "" {
		return tools.ErrorResult("path is required")
	}
	path = expandPath(path)

	encoding := "text"
	if e, ok := params["encoding"].(string); ok {
		encoding = e
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return tools.ErrorResult(fmt.Sprintf("file not found: %s", path))
		}
		return tools.ErrorResult(fmt.Sprintf("stat file: %s", err))
	}

	if info.IsDir() {
		return tools.ErrorResult(fmt.Sprintf("path is a directory: %s", path))
	}

	if encoding == "base64" {
		return readBase64(path, info.Size())
	}
	return readText(path, params)
}

func readBase64(path string, size int64) tools.Result {
	if size > maxBase64Size {
		return tools.ErrorResult(fmt.Sprintf("file too large for base64: %d bytes (max %d)", size, maxBase64Size))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("read file: %s", err))
	}
	return tools.TextResult(base64.StdEncoding.EncodeToString(data))
}

func readText(path string, params map[string]any) tools.Result {
	file, err := os.Open(path)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("open file: %s", err))
	}
	defer file.Close()

	offset := 1
	if o, ok := params["start_line"].(float64); ok && int(o) > 0 {
		offset = int(o)
	}
	limit := defaultReadLineLimit
	if l, ok := params["line_count"].(float64); ok && int(l) > 0 {
		limit = int(l)
	}
	if limit > maxReadLineLimit {
		limit = maxReadLineLimit
	}
	if tail, ok := params["tail_lines"].(float64); ok && int(tail) > 0 {
		return readTailText(path, int(tail))
	}

	reader := bufio.NewReader(file)
	var sb strings.Builder
	lineNo := 0
	returned := 0
	truncated := false
	var nextOffset int
	for {
		line, readErr := readLogicalLine(reader)
		if readErr != nil && readErr != io.EOF {
			return tools.ErrorResult(fmt.Sprintf("read file: %s", readErr))
		}
		if readErr == io.EOF && line == "" {
			break
		}
		lineNo++
		if lineNo < offset {
			if readErr == io.EOF {
				break
			}
			continue
		}

		entry := fmt.Sprintf("%d: %s\n", lineNo, line)
		if returned >= limit || sb.Len()+len(entry) > maxReadResultBytes {
			truncated = true
			nextOffset = lineNo
			break
		}
		sb.WriteString(entry)
		returned++
		if readErr == io.EOF {
			break
		}
	}

	content := sb.String()
	if returned == 0 && !truncated {
		return tools.TextResult(fmt.Sprintf("offset %d exceeds total lines %d", offset, lineNo))
	}
	if truncated {
		if nextOffset == 0 {
			nextOffset = lineNo + 1
		}
		content += fmt.Sprintf("\n... (truncated. Use start_line=%d to read more; limit capped at %d lines and %d bytes per result)", nextOffset, maxReadLineLimit, maxReadResultBytes)
	}

	return tools.Result{Content: content, Truncated: truncated}
}

func readLogicalLine(r *bufio.Reader) (string, error) {
	var line []byte
	lineTruncated := false
	for {
		part, err := r.ReadSlice('\n')
		if len(part) > 0 {
			remaining := maxReadLineBytes - len(line)
			if remaining > 0 {
				if len(part) > remaining {
					line = append(line, part[:remaining]...)
					lineTruncated = true
				} else {
					line = append(line, part...)
				}
			} else {
				lineTruncated = true
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err == nil || err == io.EOF {
			text := string(line)
			text = strings.TrimSuffix(text, "\n")
			text = strings.TrimSuffix(text, "\r")
			if lineTruncated {
				text += "... (line truncated)"
			}
			return text, err
		}
		return "", err
	}
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
