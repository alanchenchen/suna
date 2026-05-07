package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxEntries = 500

type ListDir struct{}

func (ListDir) Name() string { return "listdir" }
func (ListDir) Description() string {
	return "列出目录内容，返回结构化文件列表。支持递归列出，最大深度 3。"
}
func (ListDir) Category() Category { return Perceive }
func (ListDir) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":      map[string]any{"type": "string", "description": "目录路径"},
			"recursive": map[string]any{"type": "boolean", "description": "是否递归列出子目录"},
			"max_depth": map[string]any{"type": "integer", "description": "递归最大深度（默认3）"},
		},
		"required": []string{"path"},
	}
}

func (ListDir) Execute(ctx context.Context, params map[string]any) Result {
	path, _ := params["path"].(string)
	if path == "" {
		return ErrorResult("path is required")
	}
	path = expandPath(path)

	recursive := false
	if r, ok := params["recursive"].(bool); ok {
		recursive = r
	}
	maxDepth := 3
	if d, ok := params["max_depth"].(float64); ok && int(d) > 0 {
		maxDepth = int(d)
		if maxDepth > 3 {
			maxDepth = 3
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrorResult(fmt.Sprintf("directory not found: %s", path))
		}
		return ErrorResult(fmt.Sprintf("stat directory: %s", err))
	}
	if !info.IsDir() {
		return ErrorResult(fmt.Sprintf("path is not a directory: %s", path))
	}

	var entries []entry
	if recursive {
		entries = walkDir(path, path, 0, maxDepth, &entries)
	} else {
		entries = readDir(path)
	}

	if len(entries) > maxEntries {
		entries = entries[:maxEntries]
		return Result{
			Content:   formatEntries(entries) + fmt.Sprintf("\n... (truncated, showing %d of total)", maxEntries),
			Truncated: true,
		}
	}

	if len(entries) == 0 {
		return TextResult("(empty directory)")
	}
	return TextResult(formatEntries(entries))
}

type entry struct {
	Name     string
	RelPath  string
	Type     string
	Size     int64
	Modified time.Time
}

func readDir(path string) []entry {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil
	}
	var result []entry
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		typ := "file"
		if e.IsDir() {
			typ = "dir"
		}
		result = append(result, entry{
			Name:     e.Name(),
			RelPath:  e.Name(),
			Type:     typ,
			Size:     info.Size(),
			Modified: info.ModTime(),
		})
	}
	return result
}

func walkDir(root, current string, depth, maxDepth int, entries *[]entry) []entry {
	if depth > maxDepth {
		return *entries
	}
	dirEntries, err := os.ReadDir(current)
	if err != nil {
		return *entries
	}
	for _, e := range dirEntries {
		if len(*entries) >= maxEntries*2 {
			return *entries
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		typ := "file"
		if e.IsDir() {
			typ = "dir"
		}
		rel, _ := filepath.Rel(root, filepath.Join(current, e.Name()))
		*entries = append(*entries, entry{
			Name:     e.Name(),
			RelPath:  rel,
			Type:     typ,
			Size:     info.Size(),
			Modified: info.ModTime(),
		})
		if e.IsDir() && depth < maxDepth {
			walkDir(root, filepath.Join(current, e.Name()), depth+1, maxDepth, entries)
		}
	}
	return *entries
}

func formatEntries(entries []entry) string {
	var sb strings.Builder
	for _, e := range entries {
		mod := e.Modified.Format("2006-01-02 15:04")
		size := ""
		if e.Type == "file" {
			size = fmt.Sprintf(" %d", e.Size)
		}
		sb.WriteString(fmt.Sprintf("%s  %s%s  %s\n", e.Type[:1], e.RelPath, size, mod))
	}
	return sb.String()
}
