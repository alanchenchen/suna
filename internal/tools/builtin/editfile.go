package builtin

import (
	"context"
	"fmt"
	"github.com/alanchenchen/suna/internal/tools"
	"os"
	"strings"
)

type EditFile struct{}

func (EditFile) Spec() tools.Spec {
	return builtinSpec("editfile", "Edit a file by replacing an exact old_string match with new_string.", tools.Act, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":        map[string]any{"type": "string", "description": "File path"},
			"old_string":  map[string]any{"type": "string", "description": "Exact text to replace"},
			"new_string":  map[string]any{"type": "string", "description": "Replacement text"},
			"replace_all": map[string]any{"type": "boolean", "description": "Whether to replace all matches"},
		},
		"required": []string{"path", "old_string", "new_string"},
	})
}

func (EditFile) Execute(ctx context.Context, params map[string]any) tools.Result {
	path, _ := params["path"].(string)
	oldStr, _ := params["old_string"].(string)
	newStr, _ := params["new_string"].(string)

	if path == "" {
		return tools.ErrorResult("path is required")
	}
	if oldStr == "" {
		return tools.ErrorResult("old_string is required")
	}
	path = expandPath(path)

	if isSystemPath(path) {
		return tools.ErrorResult(fmt.Sprintf("cannot edit system file: %s", path))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return tools.ErrorResult(fmt.Sprintf("file not found: %s", path))
		}
		return tools.ErrorResult(fmt.Sprintf("read file: %s", err))
	}

	original := string(data)
	content := original
	count := strings.Count(content, oldStr)
	if count == 0 {
		return tools.ErrorResult("old_string not found in file")
	}

	replaceAll := false
	if r, ok := params["replace_all"].(bool); ok {
		replaceAll = r
	}

	if count > 1 && !replaceAll {
		return tools.ErrorResult(fmt.Sprintf("old_string found %d times in file. Set replace_all=true to replace all occurrences.", count))
	}

	if replaceAll {
		content = strings.ReplaceAll(content, oldStr, newStr)
	} else {
		content = strings.Replace(content, oldStr, newStr, 1)
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return tools.ErrorResult(fmt.Sprintf("write file: %s", err))
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
