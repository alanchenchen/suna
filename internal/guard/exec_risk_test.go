//go:build !windows

package guard

import (
	"context"
	"testing"
)

// 只读迁移：AST 命令名精确，引号内文本不误判。
func TestExecReadOnlyQuotedTextNotMisjudged(t *testing.T) {
	g := NewGuardWithMode(nil, "test", ModeAsk)
	res := g.Check(context.Background(), "exec", map[string]any{"command": `echo "rm -rf /"`})
	if !res.ReadOnly {
		t.Fatalf("echo quoted rm readOnly = false, want true")
	}
}

// 只读迁移：动态表达式内部只读放行，内部高危由结构性高危层拦截。
// git 等子命令语义敏感的命令保守非只读（AST 只能看到命令名，无法证明子命令只读）。
func TestExecReadOnlyDynamicExpressionGrading(t *testing.T) {
	g := NewGuardWithMode(nil, "test", ModeAsk)
	cases := []struct {
		name    string
		command string
		wantRO  bool
		wantRej bool
	}{
		{"git rev parse conservative", "echo $(git rev-parse HEAD)", false, false},
		{"date readonly", "echo $(date)", true, false},
		{"rm dangerous", "echo $(rm -rf /)", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := g.Check(context.Background(), "exec", map[string]any{"command": tc.command})
			if res.ReadOnly != tc.wantRO {
				t.Fatalf("readOnly = %v, want %v", res.ReadOnly, tc.wantRO)
			}
			if (res.Decision == Reject) != tc.wantRej {
				t.Fatalf("rejected = %v, want %v (decision=%s)", res.Decision == Reject, tc.wantRej, res.Decision)
			}
		})
	}
}

// 只读迁移：find -delete 结构化否定（不再 fallback 旧白名单误判只读）。
func TestExecReadOnlyFindDeleteDenied(t *testing.T) {
	g := NewGuardWithMode(nil, "test", ModeAsk)
	res := g.Check(context.Background(), "exec", map[string]any{"command": "find . -delete"})
	if res.ReadOnly {
		t.Fatalf("find -delete readOnly = true, want false")
	}
}

// 只读迁移：写重定向使命令非只读。
func TestExecReadOnlyWriteRedirectNotReadonly(t *testing.T) {
	g := NewGuardWithMode(nil, "test", ModeAsk)
	res := g.Check(context.Background(), "exec", map[string]any{"command": "echo hi > out.txt"})
	if res.ReadOnly {
		t.Fatalf("echo > file readOnly = true, want false")
	}
	// /dev/null 丢弃输出仍可只读（无文件副作用）。
	res = g.Check(context.Background(), "exec", map[string]any{"command": "echo hi > /dev/null"})
	if !res.ReadOnly {
		t.Fatalf("echo > /dev/null readOnly = false, want true")
	}
}

// 只读迁移：Windows 删除命令一律非只读（不在只读白名单）。
func TestExecReadOnlyWindowsDeleteCommands(t *testing.T) {
	g := NewGuardWithMode(nil, "test", ModeAsk)
	cases := []struct {
		name    string
		command string
	}{
		{"del sq", `del /s /q C:\Users\me`},
		{"remove-item recurse force", `Remove-Item -Recurse -Force C:\Users\me`},
		{"del single", `del file.txt`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := g.Check(context.Background(), "exec", map[string]any{"command": tc.command, "shell": "powershell"})
			if res.ReadOnly {
				t.Fatalf("%q readOnly = true, want false", tc.command)
			}
		})
	}
}

// Windows drive 路径参与 workspace 检查（C:\foo 越界应拦截）。
func TestExecReadOnlyWindowsDrivePathWorkspace(t *testing.T) {
	root := t.TempDir()
	g := NewGuardWithConfigModeAndWorkspace(nil, "test", ModeAsk, root, nil, nil, nil, nil)
	res := g.Check(context.Background(), "exec", map[string]any{"command": `type C:\Windows\system32\drivers\etc\hosts`, "shell": "cmd"})
	if res.Decision != Reject {
		t.Fatalf("windows drive path decision = %s, want reject", res.Decision)
	}
	if res.Audit != "workspace_reject" {
		t.Fatalf("audit = %q, want workspace_reject", res.Audit)
	}
}

// 变量/命令替换路径不误判：$(pwd)/x、$dir/x 值无法静态确定，走正常 Guard flow（与旧语义一致）。
func TestExecReadOnlyDynamicPathsNotMisjudged(t *testing.T) {
	root := t.TempDir()
	g := NewGuardWithConfigModeAndWorkspace(nil, "test", ModeAsk, root, nil, nil, nil, nil)
	for _, command := range []string{`cat "$(pwd)/x"`, `cat $(pwd)/x`, `cat "$dir/x"`, `cat $dir/x`} {
		res := g.Check(context.Background(), "exec", map[string]any{"command": command, "cwd": root})
		if res.Audit == "workspace_reject" {
			t.Fatalf("%q audit = workspace_reject, want normal Guard flow (dynamic path)", command)
		}
	}
}
