//go:build !windows

package guard

import (
	"context"
	"testing"
)

// 动态表达式内部解释器危险调用：$(python -c 'os.system(...)') 应保守判非只读，
// 不能因 DynamicCmds 只提取命令名（无参数）而放行为只读。
func TestExecReadOnlyDynamicInterpreterDangerous(t *testing.T) {
	g := NewGuardWithMode(nil, "test", ModeAsk)
	cases := []struct {
		name    string
		command string
	}{
		{"python os.system", `echo $(python -c 'import os; os.system("rm -rf x")')`},
		{"node eval", `echo $(node -e 'eval("process.exit(1)")')`},
		{"perl exec", `echo $(perl -e 'exec "rm -rf x"')`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := g.Check(context.Background(), "exec", map[string]any{"command": tc.command})
			if res.ReadOnly {
				t.Fatalf("%q readOnly = true, want false (dynamic interpreter is not provably read-only)", tc.command)
			}
		})
	}
}
