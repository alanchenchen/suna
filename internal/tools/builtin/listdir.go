package builtin

import (
	"context"
	"fmt"
	"github.com/alanchenchen/suna/internal/tools"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultListLimit = 500
	maxListLimit     = 1000
	maxWalkEntries   = 5000
)

type ListDir struct{}

func (ListDir) Spec() tools.Spec {
	return builtinSpec("listdir", "List directory contents as structured entries. Supports offset/limit pagination and recursive listing up to depth 3.", tools.Perceive, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":           map[string]any{"type": "string", "description": "Directory path"},
			"recursive":      map[string]any{"type": "boolean", "description": "Whether to recursively list subdirectories"},
			"max_depth":      map[string]any{"type": "integer", "description": "Maximum recursion depth, default 3"},
			"offset":         map[string]any{"type": "integer", "description": "Starting entry index, 1-indexed, for continuing large directory listings"},
			"limit":          map[string]any{"type": "integer", "description": "Maximum entries to return, default 500, max 1000"},
			"include":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Glob patterns to include"},
			"exclude":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Glob patterns to exclude"},
			"include_hidden": map[string]any{"type": "boolean", "description": "Whether to include hidden entries"},
		},
		"required": []string{"path"},
	})
}

func (ListDir) Execute(ctx context.Context, params map[string]any) tools.Result {
	path, _ := params["path"].(string)
	if path == "" {
		return tools.ErrorResult("path is required")
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
	offset := 1
	if o, ok := params["offset"].(float64); ok && int(o) > 0 {
		offset = int(o)
	}
	limit := defaultListLimit
	if l, ok := params["limit"].(float64); ok && int(l) > 0 {
		limit = int(l)
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return tools.ErrorResult(fmt.Sprintf("directory not found: %s", path))
		}
		return tools.ErrorResult(fmt.Sprintf("stat directory: %s", err))
	}
	if !info.IsDir() {
		return tools.ErrorResult(fmt.Sprintf("path is not a directory: %s", path))
	}

	page := listPage{offset: offset, limit: limit}
	filter := listFilter{include: stringListParam(params["include"]), exclude: stringListParam(params["exclude"])}
	if h, ok := params["include_hidden"].(bool); ok {
		filter.includeHidden = h
	}
	if recursive {
		walkDirPage(path, path, 0, maxDepth, &page, filter)
	} else {
		readDirPage(path, &page, filter)
	}

	if page.seen == 0 {
		return tools.TextResult("(empty directory)")
	}
	if len(page.entries) == 0 {
		return tools.TextResult(fmt.Sprintf("offset %d exceeds scanned entries %d", offset, page.seen))
	}
	content := formatEntries(page.entries)
	truncated := page.hasMore || page.stoppedEarly
	if truncated {
		content += fmt.Sprintf("\n... (truncated, at least %d entries. Use offset=%d to list more; limit capped at %d)", page.seen, page.nextOffset(), maxListLimit)
	}
	return tools.Result{Content: content, Truncated: truncated}
}

type entry struct {
	Name     string
	RelPath  string
	Type     string
	Size     int64
	Modified time.Time
}

type listPage struct {
	offset       int
	limit        int
	seen         int
	entries      []entry
	hasMore      bool
	stoppedEarly bool
}

func (p *listPage) add(e entry) bool {
	p.seen++
	if p.seen < p.offset {
		return true
	}
	if len(p.entries) >= p.limit {
		p.hasMore = true
		return false
	}
	p.entries = append(p.entries, e)
	return true
}

func (p *listPage) nextOffset() int { return p.offset + len(p.entries) }

func readDirPage(path string, page *listPage, filter listFilter) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		typ := "file"
		if e.IsDir() {
			typ = "dir"
		}
		rel := e.Name()
		if !filter.allow(rel, e.Name()) {
			continue
		}
		if !page.add(entry{
			Name:     e.Name(),
			RelPath:  rel,
			Type:     typ,
			Size:     info.Size(),
			Modified: info.ModTime(),
		}) {
			return
		}
	}
}

func walkDirPage(root, current string, depth, maxDepth int, page *listPage, filter listFilter) {
	if depth > maxDepth {
		return
	}
	if page.hasMore || page.stoppedEarly {
		return
	}
	dirEntries, err := os.ReadDir(current)
	if err != nil {
		return
	}
	for _, e := range dirEntries {
		if page.seen >= maxWalkEntries {
			page.stoppedEarly = true
			return
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
		if !filter.allow(rel, e.Name()) {
			continue
		}
		if !page.add(entry{
			Name:     e.Name(),
			RelPath:  rel,
			Type:     typ,
			Size:     info.Size(),
			Modified: info.ModTime(),
		}) {
			return
		}
		if e.IsDir() && depth < maxDepth {
			walkDirPage(root, filepath.Join(current, e.Name()), depth+1, maxDepth, page, filter)
			if page.hasMore || page.stoppedEarly {
				return
			}
		}
	}
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

type listFilter struct {
	include       []string
	exclude       []string
	includeHidden bool
}

func (f listFilter) allow(rel string, name string) bool {
	if !f.includeHidden && strings.HasPrefix(name, ".") {
		return false
	}
	if len(f.include) > 0 && !matchAnyGlob(rel, f.include) {
		return false
	}
	if len(f.exclude) > 0 && matchAnyGlob(rel, f.exclude) {
		return false
	}
	return true
}
