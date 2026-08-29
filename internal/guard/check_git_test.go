package guard

import (
	"context"
	"runtime"
	"testing"
)

// git 等有子命令语义的命令保守非只读：静态分析无法覆盖其所有参数组合，
// 一律走 mode policy（ask 确认 / smart 审 / readonly 拒）。
func TestExecReadOnlyGitConservative(t *testing.T) {
	g := NewGuardWithMode(nil, "test", ModeAsk)
	cases := []struct {
		name    string
		command string
	}{
		{"branch list", "git branch"},
		{"branch -a", "git branch -a"},
		{"branch -d", "git branch -d old-feature"},
		{"status", "git status"},
		{"log", "git log"},
		{"stash list", "git stash list"},
		{"stash pop", "git stash pop"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := g.Check(context.Background(), "exec", map[string]any{"command": tc.command})
			if res.ReadOnly {
				t.Fatalf("%q readOnly = true, want false (git is conservative non-read-only)", tc.command)
			}
		})
	}
}

// 动态表达式内部命令：只有简单白名单（date）放行，git 等语义命令保守非只读。
// Windows 无 AST 解析走 fallback 分词器，hasDynamicShellSyntax 对 $() 保守判非只读，
// 因此 $(date) 在 Windows 上也是非只读（平台差异，期望值按平台区分）。
func TestExecReadOnlyDynamicSubcommandSensitive(t *testing.T) {
	g := NewGuardWithMode(nil, "test", ModeAsk)
	cases := []struct {
		name    string
		command string
		wantRO  bool
	}{
		{"date readonly", "echo $(date)", runtime.GOOS != "windows"},
		{"git rev parse conservative", "echo $(git rev-parse HEAD)", false},
		{"git commit conservative", "echo $(git commit -m x)", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := g.Check(context.Background(), "exec", map[string]any{"command": tc.command})
			if res.ReadOnly != tc.wantRO {
				t.Fatalf("%q readOnly = %v, want %v", tc.command, res.ReadOnly, tc.wantRO)
			}
		})
	}
}
