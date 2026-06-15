package builtin

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/alanchenchen/suna/internal/tools"
)

type EditFile struct{}

func (EditFile) Spec() tools.Spec {
	return builtinSpec("editfile", "Modify an existing text file with one or more exact replacements, applied atomically.", tools.Act, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "File path"},
			"edits": map[string]any{"type": "array", "description": "Ordered exact replacements within this file", "items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"old_string":            map[string]any{"type": "string", "description": "Exact text to replace"},
					"new_string":            map[string]any{"type": "string", "description": "Replacement text"},
					"target":                map[string]any{"type": "string", "description": "Match target. Omit for default unique replacement. Use \"all\" to replace every match. Use a positive 1-based occurrence number as a string, for example \"2\", to replace one specific match."},
					"expected_replacements": map[string]any{"type": "integer", "description": "Fail unless this many replacements are made"},
				},
				"required": []string{"old_string", "new_string"},
			}},
		},
		"required": []string{"path", "edits"},
	})
}

func (EditFile) Execute(ctx context.Context, params map[string]any) tools.Result {
	path, _ := params["path"].(string)
	if path == "" {
		return tools.ErrorResult("path is required")
	}
	path = expandPath(path)
	if isSystemPath(path) {
		return tools.ErrorResult(fmt.Sprintf("cannot edit system file: %s", path))
	}
	edits, err := parseEditOperations(params["edits"])
	if err != nil {
		return tools.ErrorResult(err.Error())
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
	totalReplacements := 0
	// 所有 replacement 先在内存中按顺序验证并应用，任一失败都不会写入文件，保证 editfile 对单文件是原子的。
	for i, edit := range edits {
		updated, replacements, err := applyEditOperation(content, edit)
		if err != nil {
			return tools.ErrorResult(fmt.Sprintf("edit %d: %s", i+1, err))
		}
		content = updated
		totalReplacements += replacements
	}

	if err := writeFileAtomic(path, []byte(content), true); err != nil {
		return tools.ErrorResult(fmt.Sprintf("write file: %s", err))
	}

	operation := "updated"
	if original == content {
		operation = "unchanged"
	}
	return fileChangeResult(fileChange{Path: path, Operation: operation, OldContent: original, NewContent: content, OldExists: true, Replacements: totalReplacements})
}

type editOperation struct {
	OldString            string
	NewString            string
	Mode                 string
	Occurrence           int
	ExpectedReplacements *int
}

func parseEditOperations(value any) ([]editOperation, error) {
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return nil, fmt.Errorf("edits must be a non-empty array")
	}
	edits := make([]editOperation, 0, len(items))
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("edits[%d] must be an object", i)
		}
		oldStr, _ := m["old_string"].(string)
		newStr, _ := m["new_string"].(string)
		if oldStr == "" {
			return nil, fmt.Errorf("edits[%d].old_string is required", i)
		}
		edit := editOperation{OldString: oldStr, NewString: newStr, Mode: "unique"}
		if err := applyEditTarget(i, m["target"], &edit); err != nil {
			return nil, err
		}
		if expected, ok := m["expected_replacements"].(float64); ok && int(expected) >= 0 {
			n := int(expected)
			edit.ExpectedReplacements = &n
		}
		edits = append(edits, edit)
	}
	return edits, nil
}

func applyEditTarget(index int, value any, edit *editOperation) error {
	if value != nil {
		if _, ok := value.(string); !ok {
			return fmt.Errorf("edits[%d].target must be omitted, \"all\", or a positive 1-based occurrence number string", index)
		}
	}
	target, _ := value.(string)
	target = strings.TrimSpace(target)
	if target == "" || target == "unique" {
		edit.Mode = "unique"
		return nil
	}
	if target == "all" {
		edit.Mode = "all"
		return nil
	}
	n, ok := positiveIntegerString(target)
	if !ok {
		return fmt.Errorf("edits[%d].target must be omitted, \"all\", or a positive 1-based occurrence number string", index)
	}
	edit.Mode = "nth"
	edit.Occurrence = n
	return nil
}

func positiveIntegerString(value string) (int, bool) {
	if value == "" {
		return 0, false
	}
	n := 0
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, n > 0
}

func applyEditOperation(content string, edit editOperation) (string, int, error) {
	count := strings.Count(content, edit.OldString)
	if count == 0 {
		return content, 0, fmt.Errorf("old_string not found in file")
	}
	replacements := 1
	var updated string
	switch edit.Mode {
	case "all":
		updated = strings.ReplaceAll(content, edit.OldString, edit.NewString)
		replacements = count
	case "nth":
		if edit.Occurrence > count {
			return content, 0, fmt.Errorf("old_string occurrence %d not found; found %d matches", edit.Occurrence, count)
		}
		updated = replaceOccurrence(content, edit.OldString, edit.NewString, edit.Occurrence)
	case "unique":
		if count > 1 {
			return content, 0, fmt.Errorf("old_string found %d times in file. Use target=\"all\" or target=\"2\" for a specific 1-based occurrence", count)
		}
		updated = strings.Replace(content, edit.OldString, edit.NewString, 1)
	}
	if edit.ExpectedReplacements != nil && replacements != *edit.ExpectedReplacements {
		return content, 0, fmt.Errorf("made %d replacements, expected %d", replacements, *edit.ExpectedReplacements)
	}
	return updated, replacements, nil
}
