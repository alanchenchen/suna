package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
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
func TestEditFileReportsClosestLineWhenNoMatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "miss.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\ngamma\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	res := EditFile{}.Execute(context.Background(), map[string]any{
		"path":  path,
		"edits": []any{map[string]any{"old_string": "betaX", "new_string": "BETA"}},
	})
	if !res.IsError || !strings.Contains(res.Error, "EDIT_OLD_NOT_FOUND") {
		t.Fatalf("EditFile.Execute() = %#v, want not-found error", res)
	}
	if !strings.Contains(res.Error, "line 2") || !strings.Contains(res.Error, "beta") {
		t.Fatalf("error does not point at the closest line: %s", res.Error)
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

func TestEditFileNotFoundHintStaysCompact(t *testing.T) {
	dir := t.TempDir()

	t.Run("CRLF line has no carriage return", func(t *testing.T) {
		path := filepath.Join(dir, "crlf.txt")
		if err := os.WriteFile(path, []byte("alpha\r\n\tbetaX\r\ngamma\r\n"), 0644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		res := EditFile{}.Execute(context.Background(), map[string]any{
			"path":  path,
			"edits": []any{map[string]any{"old_string": "betaZZZ", "new_string": "B"}},
		})
		if !res.IsError {
			t.Fatal("expected not-found error")
		}
		if strings.Contains(res.Error, "\r") {
			t.Fatalf("hint leaked carriage return: %q", res.Error)
		}
		if !strings.Contains(res.Error, "betaX") {
			t.Fatalf("hint missing real text: %q", res.Error)
		}
	})

	t.Run("long line is reported in full", func(t *testing.T) {
		path := filepath.Join(dir, "long.txt")
		line := "func veryLongName" + strings.Repeat("X", 5000)
		if err := os.WriteFile(path, []byte(line+"\n"), 0644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		res := EditFile{}.Execute(context.Background(), map[string]any{
			"path":  path,
			"edits": []any{map[string]any{"old_string": "func veryLongNameZZZ", "new_string": "B"}},
		})
		if !res.IsError {
			t.Fatal("expected not-found error")
		}
		// 回显真实行不截断：模型需要看到完整内容才能改对。
		if !strings.Contains(res.Error, strings.Repeat("X", 100)) {
			t.Fatalf("hint should include the real line: %q", res.Error)
		}
	})

	t.Run("CJK truncation stays valid UTF-8", func(t *testing.T) {
		path := filepath.Join(dir, "cjk.txt")
		line := "这是一个很长的中文函数名" + strings.Repeat("测试", 200)
		if err := os.WriteFile(path, []byte(line+"\n"), 0644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		res := EditFile{}.Execute(context.Background(), map[string]any{
			"path":  path,
			"edits": []any{map[string]any{"old_string": "这是一个很长的中文函数名ZZZ", "new_string": "B"}},
		})
		if !res.IsError {
			t.Fatal("expected not-found error")
		}
		if !utf8.ValidString(res.Error) {
			t.Fatalf("truncation broke UTF-8: %q", res.Error)
		}
	})
}

func TestEditFileHintPointsAtFirstMismatchingLine(t *testing.T) {
	cases := []struct {
		name, content, oldString string
		wantLine                 string
		wantText                 string
	}{
		{
			name:      "single line misremembered",
			content:   "alpha\n\tbeta\ngamma\n",
			oldString: "betaX",
			wantLine:  "line 2",
			wantText:  "beta",
		},
		{
			name:      "third line of a block wrong, first two correct",
			content:   "func f() {\n\tif x {\n\t\treturn nil\n\t}\n}\n",
			oldString: "func f() {\n\tif x {\n\t\treturn err\n\t}\n}",
			wantLine:  "line 3",
			wantText:  "return nil",
		},
		{
			name:      "indent difference reports the real file line",
			content:   "func f() {\n\treturn nil\n}\n",
			oldString: "func f() {\n    return nil\n}",
			wantLine:  "line 1",
			wantText:  "func f() {",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "target.txt")
			if err := os.WriteFile(path, []byte(tc.content), 0644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			res := EditFile{}.Execute(context.Background(), map[string]any{
				"path":  path,
				"edits": []any{map[string]any{"old_string": tc.oldString, "new_string": "X"}},
			})
			if !res.IsError {
				t.Fatal("expected not-found error")
			}
			if !strings.Contains(res.Error, tc.wantLine) || !strings.Contains(res.Error, tc.wantText) {
				t.Fatalf("hint should point at %s containing %q, got: %s", tc.wantLine, tc.wantText, res.Error)
			}
		})
	}
}

func TestEditFileErrorsCarryMatchLineNumbers(t *testing.T) {
	content := "alpha\nbeta\ngamma\nbeta\ndelta\n"
	cases := []struct {
		name     string
		edit     map[string]any
		wantCode string
		wantLine string
	}{
		{
			name:     "occurrence out of range reports where the matches are",
			edit:     map[string]any{"old_string": "beta", "new_string": "B", "target": "5"},
			wantCode: "EDIT_OCCURRENCE_NOT_FOUND",
			wantLine: "line 2, 4",
		},
		{
			name:     "ambiguous match lists every location",
			edit:     map[string]any{"old_string": "beta", "new_string": "B"},
			wantCode: "EDIT_AMBIGUOUS",
			wantLine: "line 2, 4",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "target.txt")
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			res := EditFile{}.Execute(context.Background(), map[string]any{"path": path, "edits": []any{tc.edit}})
			if !res.IsError {
				t.Fatal("expected an error")
			}
			if !strings.Contains(res.Error, tc.wantCode) || !strings.Contains(res.Error, tc.wantLine) {
				t.Fatalf("error should mention %s and %q, got: %s", tc.wantCode, tc.wantLine, res.Error)
			}
		})
	}
}

func TestEditFileBatchFailureReportsEarlierEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "target.txt")
	original := "a\nb\nc\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	// 前两条能匹配，第三条失败：必须告知前面已匹配但未写入，否则模型会拿原始内容重试。
	res := EditFile{}.Execute(context.Background(), map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{"old_string": "a", "new_string": "A"},
			map[string]any{"old_string": "b", "new_string": "B"},
			map[string]any{"old_string": "zzz", "new_string": "Z"},
		},
	})
	if !res.IsError {
		t.Fatal("expected an error")
	}
	if !strings.Contains(res.Error, "2 earlier edit(s)") {
		t.Fatalf("error should mention earlier matched edits, got: %s", res.Error)
	}
	data, _ := os.ReadFile(path)
	if string(data) != original {
		t.Fatalf("file changed on failure: %q", string(data))
	}
}

func TestEditFileAmbiguousHintStaysBounded(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("package main\n")
	for i := 0; i < 2000; i++ {
		sb.WriteString("\tif value == xxxxxxxxxxxxxxxxxxxx {\n\t\treturn nil\n\t}\n")
	}
	path := filepath.Join(t.TempDir(), "many.go")
	if err := os.WriteFile(path, []byte(sb.String()), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	res := EditFile{}.Execute(context.Background(), map[string]any{
		"path":  path,
		"edits": []any{map[string]any{"old_string": "if value == xxxxxxxxxxxxxxxxxxxx {", "new_string": "X"}},
	})
	if !res.IsError {
		t.Fatal("expected ambiguous error")
	}
	// 上千处匹配时只列前几处并标注总数，否则提示会撑爆模型上下文。
	if len(res.Error) > 400 {
		t.Fatalf("hint too long: %d bytes", len(res.Error))
	}
	if !strings.Contains(res.Error, "2000 locations") || !strings.Contains(res.Error, "more") {
		t.Fatalf("hint should state the total and elide the rest: %s", res.Error)
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
		{"system path", map[string]any{"path": "/etc/hosts", "edits": []any{map[string]any{"old_string": "a", "new_string": "b"}}}, "EDIT_SYSTEM_PATH"},
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
