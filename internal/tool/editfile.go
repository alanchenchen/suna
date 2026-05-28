package tool

import (
	"context"
	"fmt"
	"os"
	"strings"
)

type EditFile struct{}

func (EditFile) Name() string { return "editfile" }
func (EditFile) Description() string {
	return "Edit a file by replacing an exact old_string match with new_string."
}
func (EditFile) Category() Category { return Act }
func (EditFile) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":        map[string]any{"type": "string", "description": "File path"},
			"old_string":  map[string]any{"type": "string", "description": "Exact text to replace"},
			"new_string":  map[string]any{"type": "string", "description": "Replacement text"},
			"replace_all": map[string]any{"type": "boolean", "description": "Whether to replace all matches"},
		},
		"required": []string{"path", "old_string", "new_string"},
	}
}

func (EditFile) Execute(ctx context.Context, params map[string]any) Result {
	path, _ := params["path"].(string)
	oldStr, _ := params["old_string"].(string)
	newStr, _ := params["new_string"].(string)

	if path == "" {
		return ErrorResult("path is required")
	}
	if oldStr == "" {
		return ErrorResult("old_string is required")
	}
	path = expandPath(path)

	if isSystemPath(path) {
		return ErrorResult(fmt.Sprintf("cannot edit system file: %s", path))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrorResult(fmt.Sprintf("file not found: %s", path))
		}
		return ErrorResult(fmt.Sprintf("read file: %s", err))
	}

	original := string(data)
	content := original
	count := strings.Count(content, oldStr)
	if count == 0 {
		return ErrorResult("old_string not found in file")
	}

	replaceAll := false
	if r, ok := params["replace_all"].(bool); ok {
		replaceAll = r
	}

	if count > 1 && !replaceAll {
		return ErrorResult(fmt.Sprintf("old_string found %d times in file. Set replace_all=true to replace all occurrences.", count))
	}

	if replaceAll {
		content = strings.ReplaceAll(content, oldStr, newStr)
	} else {
		content = strings.Replace(content, oldStr, newStr, 1)
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return ErrorResult(fmt.Sprintf("write file: %s", err))
	}

	replacements := count
	if !replaceAll {
		replacements = 1
	}
	operation := "updated"
	if original == content {
		operation = "unchanged"
	}
	return fileChangeResult(fileChange{Path: path, Operation: operation, OldContent: original, NewContent: content, OldExists: true, Replacements: replacements})
}
