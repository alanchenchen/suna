package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func BenchmarkEditFileDiagnostics(b *testing.B) {
	for _, tt := range []struct {
		name    string
		content string
		target  string
	}{
		{name: "small block", content: "function f() {\n\treturn value;\n}\n", target: "function f() {\n    return value;\n}"},
		{name: "large unique", content: strings.Repeat("// unrelated padding for the diagnostic benchmark\n", 20000) + "function uniqueTarget() {\n\treturn actual;\n}\n", target: "function uniqueTarget() {\n\treturn expected;\n}"},
		{name: "repeated blocks", content: strings.Repeat("if (enabled) {\n  unrelated();\n}\n", 10000) + "if (enabled) {\n  keep();\n  return actual;\n}\n", target: "if (enabled) {\n  keep();\n  return expected;\n}"},
		{name: "long line", content: "const value = \"" + strings.Repeat("x", 100000) + "\";\n", target: "const value = \"expected\";"},
		{name: "no candidate", content: strings.Repeat("// unrelated padding for the diagnostic benchmark\n", 20000), target: "function uniqueTarget() {\n\treturn expected;\n}"},
	} {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tt.content)))
			for b.Loop() {
				if editNotFoundError(tt.content, tt.target) == nil {
					b.Fatal("missing diagnostic")
				}
			}
		})
	}
}

func BenchmarkEditFileExactMatch(b *testing.B) {
	content := strings.Repeat("// unrelated padding for the successful edit\n", 20000) + "unique original\n"
	edit := editOperation{OldString: "unique original", NewString: "replacement", Mode: "unique"}
	b.ReportAllocs()
	b.SetBytes(int64(len(content)))
	for b.Loop() {
		if _, _, _, err := applyEditOperation(content, edit, false); err != nil {
			b.Fatal(err)
		}
	}
}

func TestEditFileDiagnosticShowsRealWhitespaceAndContext(t *testing.T) {
	for _, tt := range []struct {
		name, content, target string
		want                  []string
	}{
		{name: "indent", content: "function f() {\n\treturn value;\n}\n", target: "function f() {\n    return value;\n}\n", want: []string{"line 2, column 1", `"\treturn value;\n"`, `"    return value;\n"`, "U+0009", "U+0020"}},
		{name: "trailing space", content: "function f() {\n  return value; \n}\n", target: "function f() {\n  return value;\n}\n", want: []string{"line 2, column 16", `"  return value; \n"`, "U+0020", "U+000A"}},
		{name: "zero width", content: "const token = value;\u200b\n", target: "const token = value;\n", want: []string{"line 1, column 21", `\u200b`, "U+200B"}},
		{name: "nonbreaking space", content: "const token = value;\u00a0\n", target: "const token = value;\n", want: []string{`\u00a0`, "U+00A0"}},
		{name: "repeated first line", content: "if (enabled) {\n  unrelated();\n}\n\nif (enabled) {\n  keep();\n  return nextValue;\n}\n", target: "if (enabled) {\n  keep();\n  return oldValue;\n}\n", want: []string{"line 7, column 10", "nextValue", "oldValue"}},
		{name: "changed long line must not anchor to an unrelated block", content: "function f() {\n  observed();\n  keep();\n}\n\nfunction other() {\n  completelyDifferentLongOperation();\n  unrelated();\n}\n", target: "function f() {\n  completelyDifferentLongOperation();\n  keep();\n}\n", want: []string{"line 2, column 3", "observed"}},
		{name: "long changed line still uses matching block context", content: "function f() {\n  observed();\n}\n", target: "function f() {\n  completelyDifferentLongOperation();\n}\n", want: []string{"line 2, column 3", "observed", "completelyDifferentLongOperation"}},
		{name: "first character differs", content: "const token = value;\n", target: "Xonst token = value;\n", want: []string{"line 1, column 1", "const token"}},
		{name: "unicode character column", content: "中文🙂actual\n", target: "中文🙂expected\n", want: []string{"line 1, column 4", "中文🙂actual", "中文🙂expected"}},
		{name: "leading blank line", content: "header\n\nfunction f() {\n  actual();\n}\n", target: "\nfunction f() {\n  expected();\n}\n", want: []string{"line 4, column 3", "actual()", "expected()"}},
		{name: "file ends before requested block", content: "function f() {\n  value();", target: "function f() {\n  value();\n}\n", want: []string{"line 2, column 11", "end of file"}},
		{name: "inline fragment does not invent an indentation mismatch", content: "\tconst token = actual;\n", target: "const token = expected;\n", want: []string{"line 1, column 16", "actual", "expected"}},
		{name: "late difference after probe window", content: "function f() {\n" + strings.Repeat("  keep();\n", 10) + "  actual();\n}\n", target: "function f() {\n" + strings.Repeat("  keep();\n", 10) + "  expected();\n}\n", want: []string{"line 12, column 3", "actual", "expected"}},
		{name: "identical candidate contexts", content: "function f() {\n  actual();\n}\nfunction f() {\n  actual();\n}\n", target: "function f() {\n  expected();\n}\n", want: []string{"2 equally ranked candidates", "readfile"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			message := editNotFoundError(tt.content, tt.target).Message
			for _, want := range tt.want {
				if !strings.Contains(message, want) {
					t.Fatalf("diagnostic missing %q: %s", want, message)
				}
			}
		})
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
			wantLine:  "line 2",
			wantText:  `\treturn nil`,
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

func TestEditFileDiagnosticBytesAndLineEndings(t *testing.T) {
	for _, tt := range []struct {
		name, content, target string
		want                  []string
	}{
		{name: "CRLF differs from LF", content: "function f() {\r\n  value();\r\n}\r\n", target: "function f() {\n  value();\n}\n", want: []string{"line 1, column 15", `\r\n`, "U+000D", "U+000A"}},
		{name: "mixed newline", content: "header\r\nfunction f() {\n  actual();\r\n}\n", target: "function f() {\n  expected();\n}\n", want: []string{"line 3, column 3", `\r\n`, "actual"}},
		{name: "missing final newline", content: "return actual;", target: "return actual;\n", want: []string{"line 1, column 15", "end of file", "U+000A"}},
		{name: "short target with no reliable anchor", content: "alpha\nbeta\n", target: "xy", want: []string{"readfile", "without line numbers"}},
		{name: "empty file", target: "return value;", want: []string{"readfile"}},
		{name: "only whitespace", content: " \t\n", target: "\t \n", want: []string{"readfile"}},
		{name: "UTF8 shared first byte", content: "const 值 = 中;\n", target: "const 值 = 丰;\n", want: []string{"line 1, column 11", "U+4E2D", "U+4E30"}},
		{name: "invalid UTF8 bytes", content: "const value = \xff;\n", target: "const value = \xfe;\n", want: []string{"line 1, column 15", `\xff`, `\xfe`, "byte 0xFF", "byte 0xFE"}},
		{name: "invalid UTF8 continuation is its own byte", content: "const value = a\x80;\n", target: "const value = a\x81;\n", want: []string{"line 1, column 16", "byte 0x80", "byte 0x81"}},
		{name: "truncated UTF8 prefix is not a full character", content: "const value = \xe4\xb8", target: "const value = \xe4\xb9", want: []string{"line 1, column 16", "byte 0xB8", "byte 0xB9"}},
		{name: "escaped controls", content: "const value = \x1b[31m;\n", target: "const value = plain;\n", want: []string{`\x1b`, "U+001B"}},
		{name: "literal escapes differ from invisible character", content: "const value = literal\\t;\n", target: "const value = literal\t;\n", want: []string{"U+005C", "U+0009", `\\t`}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			message := editNotFoundError(tt.content, tt.target).Message
			for _, want := range tt.want {
				if !strings.Contains(message, want) {
					t.Fatalf("diagnostic missing %q: %s", want, message)
				}
			}
			if !utf8.ValidString(message) || strings.ContainsAny(message, "\r\x00\x1b") {
				t.Fatalf("diagnostic contains unescaped control or invalid UTF8: %q", message)
			}
		})
	}
}

func TestEditFileDiagnosticDoesNotReportAdaptedCRLFAsMismatch(t *testing.T) {
	_, _, _, err := applyEditOperation("function f() {\r\n\treturn actual;\r\n}\r\n", editOperation{OldString: "function f() {\n\treturn expected;\n}\n", NewString: "replacement", Mode: "unique"}, true)
	if err == nil || !strings.Contains(err.Message, "line 2, column 9") || !strings.Contains(err.Message, `\r\n`) {
		t.Fatalf("error=%v, want actual second-line difference after newline adaptation", err)
	}
}

func TestEditFileDiagnosticBatchRefersToStagedContentWithoutWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "target.txt")
	original := "function f() {\n\treturn value;\n}\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	result := EditFile{}.Execute(context.Background(), map[string]any{"path": path, "edits": []any{
		map[string]any{"old_string": "function f() {", "new_string": "// staged header\nfunction f() {"},
		map[string]any{"old_string": "function f() {\n    return value;\n}\n", "new_string": "replacement"},
	}})
	for _, want := range []string{"EDIT_OLD_NOT_FOUND", "edit 2", "line 3, column 1", "1 earlier edit(s)", "rolled back", "staged content", "whole edits array", "No changes were written"} {
		if !result.IsError || !strings.Contains(result.Content, want) {
			t.Fatalf("missing %q in result=%+v", want, result)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != original {
		t.Fatalf("got=%q err=%v, want original untouched file", data, err)
	}
}

func FuzzEditFileDiagnosticsNeverMutateOrMisquote(f *testing.F) {
	f.Add("function f() {\n\treturn actual;\n}\n", "function f() {\n    return expected;\n}\n")
	f.Add("const value = \xff;\n", "const value = \xfe;\n")
	f.Add("中文🙂value\u200b\n", "中文🙂expected\n")
	f.Add("", "missing")
	f.Fuzz(func(t *testing.T, content, target string) {
		if len(content) > 64*1024 || len(target) > 8192 || target == "" || strings.Contains(content, target) {
			return
		}
		updated, replacements, _, err := applyEditOperation(content, editOperation{OldString: target, NewString: "replacement", Mode: "unique"}, false)
		if err == nil || replacements != 0 || updated != content || err.Code != "EDIT_OLD_NOT_FOUND" {
			t.Fatalf("failed exact edit changed content or result: replacements=%d error=%v", replacements, err)
		}
		if !utf8.ValidString(err.Message) || strings.ContainsAny(err.Message, "\r\x00\x1b") {
			t.Fatalf("unsafe diagnostic=%q", err.Message)
		}
		for _, line := range strings.Split(err.Message, "\n") {
			if quoted, ok := strings.CutPrefix(line, "File:      "); ok {
				decoded, err := strconv.Unquote(quoted)
				if err != nil || !strings.Contains(content, decoded) {
					t.Fatalf("file line does not reproduce original bytes: %q err=%v", quoted, err)
				}
			}
			if quoted, ok := strings.CutPrefix(line, "Requested: "); ok {
				decoded, err := strconv.Unquote(quoted)
				if err != nil || !strings.Contains(target, decoded) {
					t.Fatalf("requested line does not reproduce original bytes: %q err=%v", quoted, err)
				}
			}
		}
	})
}

func TestEditFileQuotedHintCanRepairExactReplacement(t *testing.T) {
	for _, tt := range []struct {
		name, original, target string
	}{
		{name: "indent and trailing space", original: "function f() {\n\treturn value; \n}\n", target: "function f() {\n    return value;\n}\n"},
		{name: "invisible suffix", original: "const value = actual;\u200b\n", target: "const value = actual;\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "target.txt")
			if err := os.WriteFile(path, []byte(tt.original), 0600); err != nil {
				t.Fatal(err)
			}
			params := func(old string) map[string]any {
				return map[string]any{"path": path, "edits": []any{map[string]any{"old_string": old, "new_string": "replacement\n"}}}
			}
			result := EditFile{}.Execute(context.Background(), params(tt.target))
			if !result.IsError {
				t.Fatal("got successful edit, want exact-match rejection")
			}
			var actual, requested string
			for _, line := range strings.Split(result.Content, "\n") {
				if quoted, ok := strings.CutPrefix(line, "File:      "); ok {
					var err error
					actual, err = strconv.Unquote(quoted)
					if err != nil {
						t.Fatal(err)
					}
				}
				if quoted, ok := strings.CutPrefix(line, "Requested: "); ok {
					var err error
					requested, err = strconv.Unquote(quoted)
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			if actual == "" || requested == "" {
				t.Fatalf("diagnostic missing exact repair text: %s", result.Content)
			}
			corrected := strings.Replace(tt.target, requested, actual, 1)
			if result := (EditFile{}).Execute(context.Background(), params(corrected)); result.IsError {
				t.Fatalf("hint-based exact retry failed: %s", result.Error)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "replacement\n" {
				t.Fatalf("got=%q err=%v, want corrected exact replacement", data, err)
			}
		})
	}
}
