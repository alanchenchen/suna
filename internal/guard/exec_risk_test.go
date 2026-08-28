package guard

import (
	"context"
	"testing"
)

// risk 迁移：AST 命令名精确，引号内文本不误判。
func TestExecRiskQuotedTextNotMisjudged(t *testing.T) {
	g := NewGuardWithMode(nil, "test", ModeAsk)
	res := g.Check(context.Background(), "exec", map[string]any{"command": `echo "rm -rf /"`})
	if res.Risk != RiskLow {
		t.Fatalf("echo quoted rm risk = %s, want low", RiskString(res.Risk))
	}
}

// risk 迁移：动态表达式分级——内部只读放行，内部高危拦截。
func TestExecRiskDynamicExpressionGrading(t *testing.T) {
	g := NewGuardWithMode(nil, "test", ModeAsk)
	cases := []struct {
		name    string
		command string
		want    RiskLevel
	}{
		{"git rev parse readonly", "echo $(git rev-parse HEAD)", RiskLow},
		{"date readonly", "echo $(date)", RiskLow},
		{"rm dangerous", "echo $(rm -rf /)", RiskHigh},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := g.Check(context.Background(), "exec", map[string]any{"command": tc.command})
			if res.Risk != tc.want {
				t.Fatalf("risk = %s, want %s", RiskString(res.Risk), RiskString(tc.want))
			}
		})
	}
}

// risk 迁移：find -delete 结构化否定（不再 fallback 旧白名单误判只读）。
func TestExecRiskFindDeleteDenied(t *testing.T) {
	g := NewGuardWithMode(nil, "test", ModeAsk)
	res := g.Check(context.Background(), "exec", map[string]any{"command": "find . -delete"})
	if res.Risk != RiskMedium {
		t.Fatalf("find -delete risk = %s, want medium", RiskString(res.Risk))
	}
}

// risk 迁移：写重定向使命令非只读。
func TestExecRiskWriteRedirectNotReadonly(t *testing.T) {
	g := NewGuardWithMode(nil, "test", ModeAsk)
	res := g.Check(context.Background(), "exec", map[string]any{"command": "echo hi > out.txt"})
	if res.Risk != RiskMedium {
		t.Fatalf("echo > file risk = %s, want medium", RiskString(res.Risk))
	}
	// /dev/null 丢弃输出仍可只读（无文件副作用）。
	res = g.Check(context.Background(), "exec", map[string]any{"command": "echo hi > /dev/null"})
	if res.Risk != RiskLow {
		t.Fatalf("echo > /dev/null risk = %s, want low", RiskString(res.Risk))
	}
}

// risk 迁移：Windows 删除命令高危识别。
func TestExecRiskWindowsDeleteCommands(t *testing.T) {
	g := NewGuardWithMode(nil, "test", ModeAsk)
	cases := []struct {
		name    string
		command string
		want    RiskLevel
	}{
		{"del sq", `del /s /q C:\Users\me`, RiskHigh},
		{"remove-item recurse force", `Remove-Item -Recurse -Force C:\Users\me`, RiskHigh},
		{"del single", `del file.txt`, RiskMedium},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := g.Check(context.Background(), "exec", map[string]any{"command": tc.command, "shell": "powershell"})
			if res.Risk != tc.want {
				t.Fatalf("risk = %s, want %s", RiskString(res.Risk), RiskString(tc.want))
			}
		})
	}
}

// git stash 语义：只有 list 是只读，无参数/其他子命令是写操作。
func TestExecRiskGitStashSemantics(t *testing.T) {
	g := NewGuardWithMode(nil, "test", ModeAsk)
	cases := []struct {
		name    string
		command string
		want    RiskLevel
	}{
		{"stash list readonly", "git stash list", RiskLow},
		{"stash push write", "git stash push", RiskMedium},
		{"stash bare write", "git stash", RiskMedium},
		{"stash pop write", "git stash pop", RiskMedium},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := g.Check(context.Background(), "exec", map[string]any{"command": tc.command})
			if res.Risk != tc.want {
				t.Fatalf("%q risk = %s, want %s", tc.command, RiskString(res.Risk), RiskString(tc.want))
			}
		})
	}
}

// Windows drive 路径参与 workspace 检查（C:\foo 越界应拦截）。
func TestExecRiskWindowsDrivePathWorkspace(t *testing.T) {
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
func TestExecRiskDynamicPathsNotMisjudged(t *testing.T) {
	root := t.TempDir()
	g := NewGuardWithConfigModeAndWorkspace(nil, "test", ModeAsk, root, nil, nil, nil, nil)
	for _, command := range []string{`cat "$(pwd)/x"`, `cat $(pwd)/x`, `cat "$dir/x"`, `cat $dir/x`} {
		res := g.Check(context.Background(), "exec", map[string]any{"command": command, "cwd": root})
		if res.Audit == "workspace_reject" {
			t.Fatalf("%q audit = workspace_reject, want normal Guard flow (dynamic path)", command)
		}
	}
}
