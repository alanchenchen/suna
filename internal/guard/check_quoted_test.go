package guard

import (
	"context"
	"testing"
)

// 引号内裸文件名（sh -c "cat .env"）也必须拦截：解释器会执行引号内容。
func TestSensitiveQuotedBareFileName(t *testing.T) {
	g := NewGuardWithMode(nil, "test", ModeAuto)
	res := g.Check(context.Background(), "exec", map[string]any{"command": `sh -c "cat .env"`})
	if res.Decision != Reject {
		t.Fatalf("sh -c cat .env decision = %s, want reject", res.Decision)
	}
	if res.Audit != "sensitive_reject" {
		t.Fatalf("audit = %q, want sensitive_reject", res.Audit)
	}
}

// 文本包含敏感文件名子串不应误拦：echo 只是打印文本，不访问文件。
func TestSensitiveTextMentionNotBlocked(t *testing.T) {
	g := NewGuardWithMode(nil, "test", ModeAuto)
	cases := []string{
		`echo "not .env file"`,
		`echo "see .npmrc docs"`,
		`printf '%s' "mentions .pem here"`,
	}
	for _, command := range cases {
		res := g.Check(context.Background(), "exec", map[string]any{"command": command})
		if res.Decision == Reject && res.Audit == "sensitive_reject" {
			t.Fatalf("%q blocked as sensitive, want normal flow (text mention only)", command)
		}
	}
}

// 引号内容分词只在解释器场景启用：echo "cat .env" 的引号内是文本（echo 不执行），
// 不应误拦；sh -c "cat .env" 的引号内会被解释器执行，应拦截。
func TestSensitiveQuotedContentOnlyForInterpreter(t *testing.T) {
	g := NewGuardWithMode(nil, "test", ModeAuto)
	for _, command := range []string{`echo "cat .env"`, `printf '%s' "cat .env"`} {
		res := g.Check(context.Background(), "exec", map[string]any{"command": command})
		if res.Decision == Reject && res.Audit == "sensitive_reject" {
			t.Fatalf("%q blocked as sensitive, want normal flow (echo does not execute quoted content)", command)
		}
	}
	res := g.Check(context.Background(), "exec", map[string]any{"command": `sh -c "cat .env"`})
	if res.Decision != Reject || res.Audit != "sensitive_reject" {
		t.Fatalf("sh -c cat .env decision/audit = %s/%q, want reject/sensitive_reject", res.Decision, res.Audit)
	}
}

// Windows fallback 分支（AST 失败走正则）同样避免文本误报：
// echo "not .env file" 只是打印文本，不应被敏感检查拦截。
func TestSensitiveTextMentionNotBlockedWindowsFallback(t *testing.T) {
	g := NewGuardWithMode(nil, "test", ModeAuto)
	for _, command := range []string{`echo "not .env file"`, `echo "cat .env"`} {
		res := g.Check(context.Background(), "exec", map[string]any{"command": command, "shell": "cmd"})
		if res.Decision == Reject && res.Audit == "sensitive_reject" {
			t.Fatalf("%q blocked as sensitive, want normal flow (text mention only)", command)
		}
	}
}
