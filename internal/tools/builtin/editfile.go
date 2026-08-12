package builtin

import (
	"context"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/alanchenchen/suna/internal/tools"
)

const maxEditReplacementCount = 1_000_000

type EditFile struct{}

func (EditFile) Spec() tools.Spec {
	return builtinSpec("editfile", "Modify one existing text file with one or more ordered exact replacements, applied atomically. Put related changes to the same file in one edits array; each edit sees the prior edit's result. Homogeneous LF/CRLF differences are handled automatically.", tools.Act, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "minLength": 1, "pattern": `.*\S.*`, "description": "File path"},
			"edits": map[string]any{"type": "array", "minItems": 1, "description": "Ordered exact replacements; all edits succeed or no changes are written", "items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"old_string":            map[string]any{"type": "string", "minLength": 1, "description": "Exact text to replace"},
					"new_string":            map[string]any{"type": "string", "description": "Replacement text. Use an empty string to delete the matched text."},
					"target":                map[string]any{"type": "string", "pattern": `^\s*(all|[1-9][0-9]*)\s*$`, "description": "Match target. Omit for a unique replacement, use \"all\" for every match, or a positive 1-based occurrence such as \"2\"."},
					"expected_replacements": map[string]any{"type": "integer", "minimum": 1, "maximum": maxEditReplacementCount, "description": "Fail unless exactly this many replacements would be made"},
				},
				"required": []string{"old_string", "new_string"},
			}},
		},
		"required": []string{"path", "edits"},
	})
}

func (EditFile) Execute(ctx context.Context, params map[string]any) tools.Result {
	path, ok := params["path"].(string)
	if !ok || strings.TrimSpace(path) == "" {
		return editErrorResult("EDIT_INVALID_PARAMS", "path must be a non-empty string")
	}
	path = expandPathWithContext(ctx, path)
	if isSystemPath(path) {
		return editErrorResult("EDIT_SYSTEM_PATH", fmt.Sprintf("cannot edit system file: %s", path))
	}
	edits, err := parseEditOperations(params["edits"])
	if err != nil {
		return editErrorResult("EDIT_INVALID_PARAMS", err.Error())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return editErrorResult("EDIT_FILE_NOT_FOUND", fmt.Sprintf("file not found: %s", path))
		}
		return editErrorResult("EDIT_READ_FAILED", fmt.Sprintf("read file: %s", err))
	}

	original := string(data)
	content := original
	originalUsesCRLF := editsNeedCRLFAdaptation(edits) && hasOnlyCRLFNewlines(original)
	totalReplacements := 0
	newlineAdapted := false
	//所有 replacement 先在内存中按顺序验证并应用，任一失败都不会写入文件，保证 editfile 对单文件是原子的。
	for i, edit := range edits {
		updated, replacements, adapted, applyErr := applyEditOperation(content, edit, originalUsesCRLF)
		if applyErr != nil {
			return editOperationErrorResult(i+1, applyErr)
		}
		content = updated
		totalReplacements += replacements
		newlineAdapted = newlineAdapted || adapted
	}

	operation := "updated"
	if original == content {
		operation = "unchanged"
	} else if err := writeFileAtomic(path, []byte(content), true); err != nil {
		return editErrorResult("EDIT_WRITE_FAILED", fmt.Sprintf("write file: %s", err))
	}
	result := fileChangeResult(fileChange{Path: path, Operation: operation, OldContent: original, NewContent: content, OldExists: true, Replacements: totalReplacements})
	if newlineAdapted {
		result.Metadata["newline_adapted"] = "crlf"
	}
	return result
}

type editOperation struct {
	OldString            string
	NewString            string
	Mode                 string
	Occurrence           int
	ExpectedReplacements *int
}

type editOperationError struct {
	Code    string
	Message string
}

func (e *editOperationError) Error() string { return e.Message }

func parseEditOperations(value any) ([]editOperation, error) {
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return nil, fmt.Errorf("edits must be a non-empty array")
	}
	edits := make([]editOperation, 0, len(items))
	for i, item := range items {
		position := i + 1
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("edit %d must be an object", position)
		}
		for key := range m {
			switch key {
			case "old_string", "new_string", "target", "expected_replacements":
			default:
				return nil, fmt.Errorf("edit %d contains unsupported field %q", position, key)
			}
		}
		oldValue, exists := m["old_string"]
		oldStr, ok := oldValue.(string)
		if !exists || !ok || oldStr == "" {
			return nil, fmt.Errorf("edit %d old_string must be a non-empty string", position)
		}
		newValue, exists := m["new_string"]
		newStr, ok := newValue.(string)
		if !exists || !ok {
			return nil, fmt.Errorf("edit %d new_string must be present and must be a string; use an empty string for deletion", position)
		}
		edit := editOperation{OldString: oldStr, NewString: newStr, Mode: "unique"}
		target, hasTarget := m["target"]
		if err := applyEditTarget(position, target, hasTarget, &edit); err != nil {
			return nil, err
		}
		if expected, exists := m["expected_replacements"]; exists {
			n, ok := positiveIntegerParam(expected)
			if !ok {
				return nil, fmt.Errorf("edit %d expected_replacements must be a positive integer", position)
			}
			edit.ExpectedReplacements = &n
		}
		edits = append(edits, edit)
	}
	return edits, nil
}

func applyEditTarget(position int, value any, exists bool, edit *editOperation) error {
	if !exists {
		edit.Mode = "unique"
		return nil
	}
	target, ok := value.(string)
	if !ok {
		return fmt.Errorf("edit %d target must be omitted, \"all\", or a positive 1-based occurrence number string", position)
	}
	target = strings.TrimSpace(target)
	if target == "all" {
		edit.Mode = "all"
		return nil
	}
	n, ok := positiveIntegerString(target)
	if !ok {
		return fmt.Errorf("edit %d target must be omitted, \"all\", or a positive 1-based occurrence number string", position)
	}
	edit.Mode = "nth"
	edit.Occurrence = n
	return nil
}

func positiveIntegerString(value string) (int, bool) {
	if value == "" || value[0] < '1' || value[0] > '9' {
		return 0, false
	}
	for i := 1; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n <= 0 || n > maxEditReplacementCount {
		return 0, false
	}
	return int(n), true
}

func positiveIntegerParam(value any) (int, bool) {
	var n int64
	switch v := value.(type) {
	case int:
		if v <= 0 || v > maxEditReplacementCount {
			return 0, false
		}
		return v, true
	case int64:
		n = v
	case int32:
		n = int64(v)
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 1 || v > maxEditReplacementCount || math.Trunc(v) != v {
			return 0, false
		}
		return int(v), true
	case float32:
		return positiveIntegerParam(float64(v))
	default:
		return 0, false
	}
	if n <= 0 || n > maxEditReplacementCount {
		return 0, false
	}
	return int(n), true
}

func applyEditOperation(content string, edit editOperation, originalUsesCRLF bool) (string, int, bool, *editOperationError) {
	oldString := edit.OldString
	newString := edit.NewString
	adapted := false
	if originalUsesCRLF && strings.Contains(newString, "\n") && !strings.Contains(newString, "\r") {
		newString = strings.ReplaceAll(newString, "\n", "\r\n")
		adapted = true
	}
	count := strings.Count(content, oldString)
	if count == 0 && canAdaptOldStringToCRLF(originalUsesCRLF, edit.OldString) {
		oldString = strings.ReplaceAll(oldString, "\n", "\r\n")
		count = strings.Count(content, oldString)
		adapted = adapted || count > 0
	}
	if count == 0 {
		return content, 0, false, &editOperationError{Code: "EDIT_OLD_NOT_FOUND", Message: "old_string has no exact match in the current staged file. Read the target region and retry with exact text"}
	}
	replacements := 1
	var updated string
	switch edit.Mode {
	case "all":
		updated = strings.ReplaceAll(content, oldString, newString)
		replacements = count
	case "nth":
		if edit.Occurrence > count {
			return content, 0, false, &editOperationError{Code: "EDIT_OCCURRENCE_NOT_FOUND", Message: fmt.Sprintf("old_string occurrence %d does not exist; found %d exact matches", edit.Occurrence, count)}
		}
		updated = replaceOccurrence(content, oldString, newString, edit.Occurrence)
	case "unique":
		if count > 1 {
			return content, 0, false, &editOperationError{Code: "EDIT_AMBIGUOUS", Message: fmt.Sprintf("old_string matched %d locations. Use target=\"all\" or a specific 1-based occurrence such as target=\"2\"", count)}
		}
		updated = strings.Replace(content, oldString, newString, 1)
	}
	if edit.ExpectedReplacements != nil && replacements != *edit.ExpectedReplacements {
		return content, 0, false, &editOperationError{Code: "EDIT_EXPECTED_MISMATCH", Message: fmt.Sprintf("would make %d replacements, expected %d", replacements, *edit.ExpectedReplacements)}
	}
	return updated, replacements, adapted, nil
}

func editsNeedCRLFAdaptation(edits []editOperation) bool {
	for _, edit := range edits {
		if strings.Contains(edit.OldString, "\n") && !strings.Contains(edit.OldString, "\r") {
			return true
		}
		if strings.Contains(edit.NewString, "\n") && !strings.Contains(edit.NewString, "\r") {
			return true
		}
	}
	return false
}

func canAdaptOldStringToCRLF(crlfContent bool, oldString string) bool {
	return crlfContent && strings.Contains(oldString, "\n") && !strings.Contains(oldString, "\r")
}

func hasOnlyCRLFNewlines(content string) bool {
	found := false
	for i := 0; i < len(content); i++ {
		switch content[i] {
		case '\r':
			if i+1 >= len(content) || content[i+1] != '\n' {
				return false
			}
		case '\n':
			if i == 0 || content[i-1] != '\r' {
				return false
			}
			found = true
		}
	}
	return found
}

func editOperationErrorResult(position int, err *editOperationError) tools.Result {
	return editErrorResult(err.Code, fmt.Sprintf("edit %d: %s", position, err.Message))
}

func editErrorResult(code, message string) tools.Result {
	text := code + ": " + message + ". No changes were written"
	return tools.Result{Content: text, Error: text, IsError: true}
}
