package guard

import (
	"context"
	"testing"
)

// 简化后只读判定行为矩阵：简单白名单只读，语义命令保守非只读。
func TestExecReadOnlySimplifiedMatrix(t *testing.T) {
	g := NewGuardWithMode(nil, "test", ModeAsk)
	cases := []struct {
		command string
		wantRO  bool
	}{
		// 简单白名单：只读
		{"ls -la", true},
		{"cat file.txt", true},
		{"echo hi", true},
		{"grep foo file", true},
		{"pwd", true},
		{"date", true},
		{"env", true},
		{"which go", true},
		{"dir", true},
		{"findstr foo", true},
		// 语义命令：保守非只读
		{"git status", false},
		{"git log", false},
		{"git diff", false},
		{"git branch", false},
		{"find . -name '*.go'", false},
		{"command -v go", false},
		// 写操作：非只读
		{"echo hi > out.txt", false},
		{"touch x", false},
		{"rm file.txt", false},
		// 解释器：非只读
		{"python script.py", false},
		{"sh -c 'ls'", false},
	}
	for _, tc := range cases {
		res := g.Check(context.Background(), "exec", map[string]any{"command": tc.command})
		if res.ReadOnly != tc.wantRO {
			t.Fatalf("%q readOnly = %v, want %v", tc.command, res.ReadOnly, tc.wantRO)
		}
	}
}
