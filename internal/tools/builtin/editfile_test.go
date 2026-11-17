package builtin

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestEditFileAppliesMultipleEditsAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\ngamma\nbeta\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	res := EditFile{}.Execute(context.Background(), map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{"old_string": "alpha", "new_string": "ALPHA", "expected_replacements": float64(1)},
			map[string]any{"old_string": "beta", "new_string": "BETA", "target": "2", "expected_replacements": float64(1)},
		},
	})
	if res.IsError {
		t.Fatalf("EditFile.Execute() error = %s", res.Error)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got, want := string(data), "ALPHA\nbeta\ngamma\nBETA\n"; got != want {
		t.Fatalf("file content = %q, want %q", got, want)
	}
	if got := res.Metadata["replacements"]; got != 2 {
		t.Fatalf("metadata replacements = %#v, want 2", got)
	}
}

func TestEditFileReplacesAllMatchesWithTargetAll(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.txt")
	if err := os.WriteFile(path, []byte("beta beta"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	res := EditFile{}.Execute(context.Background(), map[string]any{
		"path":  path,
		"edits": []any{map[string]any{"old_string": "beta", "new_string": "BETA", "target": "all", "expected_replacements": float64(2)}},
	})
	if res.IsError {
		t.Fatalf("EditFile.Execute() error = %s", res.Error)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got, want := string(data), "BETA BETA"; got != want {
		t.Fatalf("file content = %q, want %q", got, want)
	}
}

func TestEditFileDoesNotWriteWhenAnyEditFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.txt")
	original := "alpha\nbeta\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	res := EditFile{}.Execute(context.Background(), map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{"old_string": "alpha", "new_string": "ALPHA"},
			map[string]any{"old_string": "missing", "new_string": "MISSING"},
		},
	})
	if !res.IsError {
		t.Fatalf("EditFile.Execute().IsError = false, want true")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != original {
		t.Fatalf("file content = %q, want unchanged %q", string(data), original)
	}
}

func TestEditFileRequiresTargetForAmbiguousEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.txt")
	if err := os.WriteFile(path, []byte("beta beta"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	res := EditFile{}.Execute(context.Background(), map[string]any{"path": path, "edits": []any{map[string]any{"old_string": "beta", "new_string": "BETA"}}})
	if !res.IsError || !strings.Contains(res.Error, "target=\"all\"") {
		t.Fatalf("EditFile.Execute() = %#v, want ambiguous match error with target hint", res)
	}
}

func TestEditFileReplacesSpecificTargetOccurrence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.txt")
	if err := os.WriteFile(path, []byte("beta beta beta"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	res := EditFile{}.Execute(context.Background(), map[string]any{
		"path":  path,
		"edits": []any{map[string]any{"old_string": "beta", "new_string": "BETA", "target": "2"}},
	})
	if res.IsError {
		t.Fatalf("EditFile.Execute() error = %s", res.Error)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got, want := string(data), "beta BETA beta"; got != want {
		t.Fatalf("file content = %q, want %q", got, want)
	}
}

func TestEditFileRejectsInvalidTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.txt")
	if err := os.WriteFile(path, []byte("beta beta"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	res := EditFile{}.Execute(context.Background(), map[string]any{
		"path":  path,
		"edits": []any{map[string]any{"old_string": "beta", "new_string": "BETA", "target": "nth"}},
	})
	if !res.IsError || !strings.Contains(res.Error, "target must be omitted") {
		t.Fatalf("EditFile.Execute() = %#v, want invalid target error", res)
	}
}

func TestEditFileAdaptsMultilineLFInputToCRLFFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "windows.txt")
	original := "alpha\r\nbeta\r\ngamma\r\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	res := EditFile{}.Execute(context.Background(), map[string]any{
		"path": path,
		"edits": []any{map[string]any{
			"old_string":            "alpha\nbeta",
			"new_string":            "ALPHA\nBETA",
			"expected_replacements": float64(1),
		}},
	})
	if res.IsError {
		t.Fatalf("EditFile.Execute() error = %s", res.Error)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got, want := string(data), "ALPHA\r\nBETA\r\ngamma\r\n"; got != want {
		t.Fatalf("file content = %q, want %q", got, want)
	}
	if got := res.Metadata["newline_adapted"]; got != "crlf" {
		t.Fatalf("newline_adapted = %#v, want crlf", got)
	}
}

func TestEditFileDoesNotAdaptMixedNewlines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mixed.txt")
	original := "alpha\r\nbeta\ngamma\r\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	res := EditFile{}.Execute(context.Background(), map[string]any{
		"path": path,
		"edits": []any{map[string]any{
			"old_string": "alpha\nbeta",
			"new_string": "ALPHA\nBETA",
		}},
	})
	if !res.IsError || !strings.Contains(res.Error, "EDIT_OLD_NOT_FOUND") {
		t.Fatalf("EditFile.Execute() = %#v, want exact-match error", res)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := string(data); got != original {
		t.Fatalf("file content = %q, want unchanged %q", got, original)
	}
}

func TestEditFileRejectsInvalidOperationsWithoutWriting(t *testing.T) {
	tests := []struct {
		name    string
		edit    map[string]any
		message string
	}{
		{name: "missing new string", edit: map[string]any{"old_string": "alpha"}, message: "new_string must be present"},
		{name: "null new string", edit: map[string]any{"old_string": "alpha", "new_string": nil}, message: "new_string must be present"},
		{name: "non-string new string", edit: map[string]any{"old_string": "alpha", "new_string": 1}, message: "new_string must be present"},
		{name: "fractional expected", edit: map[string]any{"old_string": "alpha", "new_string": "ALPHA", "expected_replacements": 1.5}, message: "expected_replacements must be a positive integer"},
		{name: "string expected", edit: map[string]any{"old_string": "alpha", "new_string": "ALPHA", "expected_replacements": "1"}, message: "expected_replacements must be a positive integer"},
		{name: "zero expected", edit: map[string]any{"old_string": "alpha", "new_string": "ALPHA", "expected_replacements": float64(0)}, message: "expected_replacements must be a positive integer"},
		{name: "overflow target", edit: map[string]any{"old_string": "alpha", "new_string": "ALPHA", "target": "999999999999999999999999999999"}, message: "target must be"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.txt")
			original := "alpha\nbeta\n"
			if err := os.WriteFile(path, []byte(original), 0644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}

			res := EditFile{}.Execute(context.Background(), map[string]any{"path": path, "edits": []any{tt.edit}})
			if !res.IsError || !strings.Contains(res.Error, "EDIT_INVALID_PARAMS") || !strings.Contains(res.Error, tt.message) || !strings.Contains(res.Error, "No changes were written") {
				t.Fatalf("EditFile.Execute() = %#v, want invalid parameter error containing %q", res, tt.message)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			if got := string(data); got != original {
				t.Fatalf("file content = %q, want unchanged %q", got, original)
			}
		})
	}
}

func TestEditFileExpectedMismatchExplainsNoWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.txt")
	original := "alpha alpha"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	res := EditFile{}.Execute(context.Background(), map[string]any{
		"path": path,
		"edits": []any{map[string]any{
			"old_string":            "alpha",
			"new_string":            "ALPHA",
			"target":                "all",
			"expected_replacements": float64(1),
		}},
	})
	if !res.IsError || !strings.Contains(res.Error, "EDIT_EXPECTED_MISMATCH") || !strings.Contains(res.Error, "would make 2 replacements") || !strings.Contains(res.Error, "No changes were written") {
		t.Fatalf("EditFile.Execute() = %#v, want actionable expected mismatch", res)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := string(data); got != original {
		t.Fatalf("file content = %q, want unchanged %q", got, original)
	}
}

func TestEditFileNoOpDoesNotRewriteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.txt")
	original := "alpha\nbeta\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	oldTime := time.Unix(1_600_000_000, 0)
	if err := os.Chtimes(path, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}

	res := EditFile{}.Execute(context.Background(), map[string]any{
		"path": path,
		"edits": []any{map[string]any{
			"old_string": "alpha",
			"new_string": "alpha",
		}},
	})
	if res.IsError {
		t.Fatalf("EditFile.Execute() error = %s", res.Error)
	}
	if got := res.Metadata["operation"]; got != "unchanged" {
		t.Fatalf("operation = %#v, want unchanged", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.ModTime(); !got.Equal(oldTime) {
		t.Fatalf("mod time = %v, want unchanged %v", got, oldTime)
	}
}

func TestEditFileSchemaDescribesStrictInputs(t *testing.T) {
	spec := EditFile{}.Spec()
	for _, want := range []string{"one or more ordered exact replacements", "each edit sees the prior edit's result", "LF/CRLF differences are handled automatically"} {
		if !strings.Contains(spec.Description, want) {
			t.Fatalf("description missing %q: %q", want, spec.Description)
		}
	}
	properties := spec.Parameters["properties"].(map[string]any)
	edits := properties["edits"].(map[string]any)
	if got := edits["minItems"]; got != 1 {
		t.Fatalf("edits minItems = %#v, want 1", got)
	}
	items := edits["items"].(map[string]any)
	editProperties := items["properties"].(map[string]any)
	if got := editProperties["old_string"].(map[string]any)["minLength"]; got != 1 {
		t.Fatalf("old_string minLength = %#v, want 1", got)
	}
	if got := editProperties["expected_replacements"].(map[string]any)["minimum"]; got != 1 {
		t.Fatalf("expected_replacements minimum = %#v, want 1", got)
	}
}

func TestEditFilePreservesCRLFWhenSingleLineMatchAddsLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "windows.txt")
	if err := os.WriteFile(path, []byte("alpha\r\nbeta\r\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	res := EditFile{}.Execute(context.Background(), map[string]any{
		"path": path,
		"edits": []any{map[string]any{
			"old_string": "alpha",
			"new_string": "ALPHA\ninserted",
		}},
	})
	if res.IsError {
		t.Fatalf("EditFile.Execute() error = %s", res.Error)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got, want := string(data), "ALPHA\r\ninserted\r\nbeta\r\n"; got != want {
		t.Fatalf("file content = %q, want %q", got, want)
	}
	if got := res.Metadata["newline_adapted"]; got != "crlf" {
		t.Fatalf("newline_adapted = %#v, want crlf", got)
	}
}

func TestEditFileRejectsUnknownEditField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.txt")
	original := "alpha\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	res := EditFile{}.Execute(context.Background(), map[string]any{
		"path": path,
		"edits": []any{map[string]any{
			"old_string":           "alpha",
			"new_string":           "ALPHA",
			"expected_replacments": float64(1),
		}},
	})
	if !res.IsError || !strings.Contains(res.Error, "unsupported field") || !strings.Contains(res.Error, "No changes were written") {
		t.Fatalf("EditFile.Execute() = %#v, want unsupported field error", res)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := string(data); got != original {
		t.Fatalf("file content = %q, want unchanged %q", got, original)
	}
}

func TestEditFileKeepsOriginalCRLFStyleAcrossOrderedEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "windows.txt")
	if err := os.WriteFile(path, []byte("alpha\r\nbeta"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	res := EditFile{}.Execute(context.Background(), map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{"old_string": "alpha\r\n", "new_string": ""},
			map[string]any{"old_string": "beta", "new_string": "BETA\ninserted"},
		},
	})
	if res.IsError {
		t.Fatalf("EditFile.Execute() error = %s", res.Error)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got, want := string(data), "BETA\r\ninserted"; got != want {
		t.Fatalf("file content = %q, want %q", got, want)
	}
}

func TestEditFileAcceptsNumericTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "numeric.txt")
	if err := os.WriteFile(path, []byte("dup\ndup\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	// target 传数字而非字符串，应被接受而不是报参数错误。
	res := EditFile{}.Execute(context.Background(), map[string]any{
		"path":  path,
		"edits": []any{map[string]any{"old_string": "dup", "new_string": "DUP", "target": float64(2)}},
	})
	if res.IsError {
		t.Fatalf("EditFile.Execute() error = %v", res.Error)
	}
	data, _ := os.ReadFile(path)
	if got := string(data); got != "dup\nDUP\n" {
		t.Fatalf("file content = %q", got)
	}
}

func TestEditFileBehaviorMatrix(t *testing.T) {
	cases := []struct {
		name, original, oldString, newString string
		target                               any
		wantErr                              bool
		want                                 string
	}{
		{"LF unique", "a\nb\nc\n", "b", "B", nil, false, "a\nB\nc\n"},
		{"CRLF unique", "a\r\nb\r\nc\r\n", "b", "B", nil, false, "a\r\nB\r\nc\r\n"},
		{"LF all", "x\nx\n", "x", "Y", "all", false, "Y\nY\n"},
		{"CRLF all", "x\r\nx\r\n", "x", "Y", "all", false, "Y\r\nY\r\n"},
		{"LF nth", "x\nx\n", "x", "Y", "2", false, "x\nY\n"},
		{"CRLF nth", "x\r\nx\r\n", "x", "Y", "2", false, "x\r\nY\r\n"},
		{"numeric target", "x\nx\n", "x", "Y", float64(2), false, "x\nY\n"},
		{"CRLF adapts LF input", "a\r\nb\r\nc\r\n", "a\nb", "A\nB", nil, false, "A\r\nB\r\nc\r\n"},
		{"CRLF single line keeps CRLF", "a\r\nb\r\n", "b", "B", nil, false, "a\r\nB\r\n"},
		{"delete with trailing newline", "a\nb\nc\n", "b\n", "", nil, false, "a\nc\n"},
		{"delete without trailing newline", "a\nb\nc\n", "b", "", nil, false, "a\n\nc\n"},
		{"grow lines", "a\nb\nc\n", "a\nb", "A1\nA2\nA3", nil, false, "A1\nA2\nA3\nc\n"},
		{"shrink lines", "a\nb\nc\n", "a\nb\nc", "Z", nil, false, "Z\n"},
		{"no trailing newline", "a\nb", "b", "B", nil, false, "a\nB"},
		{"blank lines preserved", "a\n\n\nb\n", "a\n\n\nb", "X", nil, false, "X\n"},
		{"single line file", "only\n", "only", "NEW", nil, false, "NEW\n"},
		{"mixed newlines rejected", "a\r\nb\nc\r\n", "a\nb", "X", nil, true, ""},
		{"ambiguous unique", "dup\ndup\n", "dup", "DUP", nil, true, ""},
		{"nth out of range", "x\nx\n", "x", "Y", "5", true, ""},
		{"missing old_string", "a\nb\n", "zzz", "X", nil, true, ""},
		{"empty file", "", "x", "Y", nil, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "target.txt")
			if err := os.WriteFile(path, []byte(tc.original), 0644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			edit := map[string]any{"old_string": tc.oldString, "new_string": tc.newString}
			if tc.target != nil {
				edit["target"] = tc.target
			}
			res := EditFile{}.Execute(context.Background(), map[string]any{"path": path, "edits": []any{edit}})
			data, _ := os.ReadFile(path)
			if res.IsError != tc.wantErr {
				t.Fatalf("IsError = %v, want %v (err=%s)", res.IsError, tc.wantErr, res.Error)
			}
			if !tc.wantErr && string(data) != tc.want {
				t.Fatalf("file content = %q, want %q", string(data), tc.want)
			}
			if tc.wantErr && string(data) != tc.original {
				t.Fatalf("file changed on failure: %q", string(data))
			}
		})
	}
}

func TestEditFileEveryErrorPathReturnsCode(t *testing.T) {
	dir := t.TempDir()

	var many strings.Builder
	for i := 0; i < 5000; i++ {
		many.WriteString("dup\n")
	}
	manyPath := filepath.Join(dir, "many.txt")
	if err := os.WriteFile(manyPath, []byte(many.String()), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cases := []struct {
		name   string
		params map[string]any
		want   string
	}{
		{"invalid path", map[string]any{"path": 123, "edits": []any{}}, "EDIT_INVALID_PARAMS"},
		{"invalid edits", map[string]any{"path": filepath.Join(dir, "x.txt"), "edits": "notarray"}, "EDIT_INVALID_PARAMS"},
		{"missing file", map[string]any{"path": filepath.Join(dir, "nope.txt"), "edits": []any{map[string]any{"old_string": "a", "new_string": "b"}}}, "EDIT_FILE_NOT_FOUND"},
		{"ambiguous", map[string]any{"path": manyPath, "edits": []any{map[string]any{"old_string": "dup", "new_string": "b"}}}, "EDIT_AMBIGUOUS"},
		{"occurrence out of range", map[string]any{"path": manyPath, "edits": []any{map[string]any{"old_string": "dup", "new_string": "b", "target": "99999"}}}, "EDIT_OCCURRENCE_NOT_FOUND"},
		{"expected mismatch", map[string]any{"path": manyPath, "edits": []any{map[string]any{"old_string": "dup", "new_string": "b", "target": "all", "expected_replacements": float64(3)}}}, "EDIT_EXPECTED_MISMATCH"},
		{"unsupported field", map[string]any{"path": manyPath, "edits": []any{map[string]any{"old_string": "a", "new_string": "b", "bogus": "z"}}}, "EDIT_INVALID_PARAMS"},
		{"not found", map[string]any{"path": manyPath, "edits": []any{map[string]any{"old_string": "zzz", "new_string": "b"}}}, "EDIT_OLD_NOT_FOUND"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := EditFile{}.Execute(context.Background(), tc.params)
			if !res.IsError {
				t.Fatalf("expected an error, got %#v", res)
			}
			if !strings.Contains(res.Error, tc.want) {
				t.Fatalf("error should carry %s, got: %s", tc.want, res.Error)
			}
		})
	}
}

func TestEditFileRejectsSystemPath(t *testing.T) {
	// 系统目录前缀随平台不同，硬编码 /etc 在 Windows 上会被解析成当前盘符下的相对路径。
	systemPath := "/etc/hosts"
	if runtime.GOOS == "windows" {
		systemPath = `C:\Windows\System32\drivers\etc\hosts`
	}
	res := EditFile{}.Execute(context.Background(), map[string]any{
		"path":  systemPath,
		"edits": []any{map[string]any{"old_string": "a", "new_string": "b"}},
	})
	if !res.IsError || !strings.Contains(res.Error, "EDIT_SYSTEM_PATH") {
		t.Fatalf("system path should be rejected, got: %s", res.Error)
	}
}
