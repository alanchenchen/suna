package guard

import (
	"context"
	"testing"
)

// 动态表达式内部解释器危险调用：$(python -c 'os.system(...)') 应保守拦截，
// 不能因 DynamicCmds 只提取命令名（无参数）而放行为 low。
func TestExecRiskDynamicInterpreterDangerous(t *testing.T) {
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
			if res.Risk == RiskLow {
				t.Fatalf("%q risk = low, want medium/high (dynamic interpreter is not provably read-only)", tc.command)
			}
		})
	}
}
