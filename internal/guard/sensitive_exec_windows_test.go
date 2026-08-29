//go:build windows

package guard

import (
	"context"
	"testing"
)

// exec 敏感检查的 Windows 路径：保守解析（AST 失败走正则 fallback）
// 也必须拦截敏感文件，与 POSIX AST 路径行为一致。
func TestSensitiveFileBlockedViaExecWindowsFallback(t *testing.T) {
	g := NewGuardWithMode(nil, "test", ModeAuto)
	cases := []struct {
		name    string
		command string
	}{
		{"type env", `type .env`},
		{"type ssh key", `type C:\Users\me\.ssh\id_rsa`},
		{"type aws creds", `type C:\Users\me\.aws\credentials`},
		{"cmd c quoted env", `cmd /c "type .env"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := g.Check(context.Background(), "exec", map[string]any{"command": tc.command, "shell": "cmd"})
			if res.Decision != Reject {
				t.Fatalf("%q decision = %s, want reject", tc.command, res.Decision)
			}
			if res.Audit != "sensitive_reject" {
				t.Fatalf("%q audit = %q, want sensitive_reject", tc.command, res.Audit)
			}
		})
	}
}
