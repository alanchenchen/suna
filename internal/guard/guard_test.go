package guard

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/tools"
)

func TestGuardRiskLowOnlyForStrictReadOnlyExec(t *testing.T) {
	g := NewGuard(nil, "test")

	tests := []struct {
		name     string
		command  string
		shell    string
		decision Decision
		risk     RiskLevel
	}{
		{name: "simple readonly", command: platformSimpleReadOnlyCommand(), decision: Approve, risk: RiskLow},
		{name: "readonly pipeline", command: platformReadOnlyPipelineCommand(), decision: Approve, risk: RiskLow},
		{name: "bash compound write", command: "ls && rm -rf important", decision: Confirm, risk: RiskHigh},
		{name: "cmd compound write", command: "dir & del /s /q C:\\Users\\me", shell: "cmd", decision: Confirm, risk: RiskHigh},
		{name: "powershell compound write", command: "Get-ChildItem; Remove-Item -Recurse -Force C:\\Users\\me", shell: "powershell", decision: platformPowerShellWriteDecision(), risk: RiskHigh},
		{name: "redirection is not readonly", command: "echo hi > file.txt", decision: Confirm, risk: RiskMedium},
		{name: "find delete is not readonly", command: "find . -delete", decision: Confirm, risk: RiskMedium},
		{name: "nested shell is not readonly", command: "bash -c 'ls'", decision: Confirm, risk: RiskMedium},
		{name: "powershell encoded command is not readonly", command: "powershell -EncodedCommand SQBFAFgA", shell: "cmd", decision: Confirm, risk: RiskMedium},
		{name: "generic interpreter dynamic execution is not readonly", command: "node -e 'console.log(1)'", decision: Confirm, risk: RiskMedium},
		{name: "python process execution is high risk", command: "python -c 'import os; os.system(\"rm -rf x\")'", decision: Confirm, risk: RiskHigh},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			params := map[string]any{"command": tt.command}
			if tt.shell != "" {
				params["shell"] = tt.shell
			}
			result := g.Check(context.Background(), "exec", params)
			if result.Decision != tt.decision || result.Risk != tt.risk {
				t.Fatalf("Check(%q) decision/risk = %s/%s, want %s/%s", tt.command, result.Decision, RiskString(result.Risk), tt.decision, RiskString(tt.risk))
			}
		})
	}
}

func platformSimpleReadOnlyCommand() string {
	if runtime.GOOS == "windows" {
		return "dir"
	}
	return "ls -la"
}

func platformReadOnlyPipelineCommand() string {
	if runtime.GOOS == "windows" {
		return "git status | findstr modified"
	}
	return "git status | grep modified"
}

func platformPowerShellWriteDecision() Decision {
	if runtime.GOOS == "windows" {
		// Windows 内置 blocked rule 会直接拒绝破坏性 PowerShell 命令。
		return Reject
	}
	return Confirm
}

func TestGuardNewStructuredToolRisks(t *testing.T) {
	g := NewGuard(nil, "test")

	tests := []struct {
		name   string
		tool   string
		params map[string]any
		risk   RiskLevel
	}{
		{name: "filesystem stat", tool: "filesystem", params: map[string]any{"action": "stat", "path": "out.txt"}, risk: RiskLow},
		{name: "filesystem recursive remove", tool: "filesystem", params: map[string]any{"action": "remove", "path": "dist", "recursive": true}, risk: RiskHigh},
		{name: "http delete", tool: "http", params: map[string]any{"method": "DELETE", "url": "https://example.com/item"}, risk: RiskHigh},
		{name: "http localhost get", tool: "http", params: map[string]any{"url": "http://127.0.0.1:8080/status"}, risk: RiskMedium},
		{name: "broad content search", tool: "search", params: map[string]any{"path": "/", "query": "secret"}, risk: RiskMedium},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			result := g.Check(context.Background(), tt.tool, tt.params)
			if result.Risk != tt.risk {
				t.Fatalf("risk = %s, want %s; decision=%s reason=%q", RiskString(result.Risk), RiskString(tt.risk), result.Decision, result.Reason)
			}
		})
	}
}

func TestReadonlyAllowsStructuredReadOnlyCalls(t *testing.T) {
	g := NewGuardWithMode(nil, "test", ModeReadonly)

	for _, tt := range []struct {
		name   string
		tool   string
		params map[string]any
	}{
		{name: "filesystem stat", tool: "filesystem", params: map[string]any{"action": "stat", "path": "out.txt"}},
		{name: "http get", tool: "http", params: map[string]any{"method": "GET", "url": "https://example.com"}},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			result := g.Check(context.Background(), tt.tool, tt.params)
			if result.Decision != Approve {
				t.Fatalf("decision = %s, want approve; risk=%s reason=%q", result.Decision, RiskString(result.Risk), result.Reason)
			}
		})
	}
}

func TestGuardConservativeFallbacks(t *testing.T) {
	g := NewGuard(nil, "test")

	unknown := g.Check(context.Background(), "dangerous_future_tool", map[string]any{"path": "x"})
	if unknown.Decision != Confirm || unknown.Risk != RiskMedium {
		t.Fatalf("unknown Act fallback = %s/%s, want confirm/medium", unknown.Decision, RiskString(unknown.Risk))
	}

	write := g.Check(context.Background(), "writefile", map[string]any{"path": "new-file.txt", "content": "hello"})
	if write.Decision != Confirm || write.Risk != RiskMedium {
		t.Fatalf("writefile new file = %s/%s, want confirm/medium", write.Decision, RiskString(write.Risk))
	}

	hook := g.Check(context.Background(), "writefile", map[string]any{"path": ".git/hooks/pre-commit", "content": "#!/bin/sh"})
	if hook.Decision != Confirm || hook.Risk != RiskHigh {
		t.Fatalf("writefile hook = %s/%s, want confirm/high", hook.Decision, RiskString(hook.Risk))
	}
}

func TestMarshalParamsEscapesAndMasks(t *testing.T) {
	params := map[string]any{
		"command": "printf \"hello\"",
		"env": map[string]any{
			"API_KEY": "sk-123456789012345678901234",
		},
	}
	encoded, err := marshalParams(params)
	if err != nil {
		t.Fatalf("marshalParams() error = %v", err)
	}
	if !strings.Contains(encoded, `\"hello\"`) {
		t.Fatalf("marshalParams() = %q, want JSON-escaped string", encoded)
	}
	if strings.Contains(encoded, "sk-123456789012345678901234") || !strings.Contains(encoded, "REDACTED_ENV") {
		t.Fatalf("marshalParams() = %q, want masked secret", encoded)
	}

	contentEncoded, err := marshalParams(map[string]any{"content": "secret source code"})
	if err != nil {
		t.Fatalf("marshalParams(content) error = %v", err)
	}
	if strings.Contains(contentEncoded, "secret source code") || !strings.Contains(contentEncoded, "sha256=") {
		t.Fatalf("marshalParams(content) = %q, want summarized content with sha256", contentEncoded)
	}
}

func TestWorkspaceEmptyDoesNotBlock(t *testing.T) {
	g := NewGuardWithConfigModeAndWorkspace(nil, "test", ModeAuto, "", nil, nil, nil, nil)
	result := g.Check(context.Background(), "readfile", map[string]any{"path": "/definitely/outside"})
	if result.Decision != Approve || result.Risk != RiskLow {
		t.Fatalf("empty workspace readfile = %s/%s, want approve/low", result.Decision, RiskString(result.Risk))
	}
}

func TestWorkspaceBlocksFileToolsOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	g := NewGuardWithConfigModeAndWorkspace(nil, "test", ModeAuto, root, nil, nil, nil, nil)

	tests := []struct {
		tool string
		path string
	}{
		{tool: "readfile", path: filepath.Join(outside, "secret.txt")},
		{tool: "listdir", path: outside},
		{tool: "writefile", path: filepath.Join(outside, "new.txt")},
		{tool: "editfile", path: filepath.Join(outside, "old.txt")},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.tool, func(t *testing.T) {
			result := g.Check(context.Background(), tt.tool, map[string]any{"path": tt.path})
			if result.Decision != Reject || !strings.Contains(result.Reason, "outside workspace") {
				t.Fatalf("%s outside decision/reason = %s/%q, want reject with outside workspace", tt.tool, result.Decision, result.Reason)
			}
		})
	}
}

func TestWorkspaceAllowsFileToolsInsideRoot(t *testing.T) {
	root := t.TempDir()
	g := NewGuardWithConfigModeAndWorkspace(nil, "test", ModeAuto, root, nil, nil, nil, nil)
	result := g.Check(context.Background(), "readfile", map[string]any{"path": "file.txt"})
	if result.Decision != Approve {
		t.Fatalf("inside readfile decision = %s, want approve; reason=%q", result.Decision, result.Reason)
	}
}

func TestGuardBlockedRulesApplyToReadToolsAndHTTP(t *testing.T) {
	g := NewGuardWithConfigModeAndWorkspace(nil, "test", ModeAuto, "", []string{`secret`, `169\.254\.169\.254`}, []string{"blocked target", "blocked metadata"}, nil, nil)

	tests := []struct {
		name   string
		tool   string
		params map[string]any
	}{
		{name: "readfile", tool: "readfile", params: map[string]any{"path": "secret.txt"}},
		{name: "listdir", tool: "listdir", params: map[string]any{"path": "secret-dir"}},
		{name: "http", tool: "http", params: map[string]any{"url": "http://169.254.169.254/latest/meta-data"}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			result := g.Check(context.Background(), tt.tool, tt.params)
			if result.Decision != Reject {
				t.Fatalf("%s blocked rule decision = %s, want reject", tt.name, result.Decision)
			}
		})
	}
}

func TestWorkspaceBlocksSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "outside-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	g := NewGuardWithConfigModeAndWorkspace(nil, "test", ModeAuto, root, nil, nil, nil, nil)
	result := g.Check(context.Background(), "writefile", map[string]any{"path": filepath.Join(link, "created.txt")})
	if result.Decision != Reject || !strings.Contains(result.Reason, "outside workspace") {
		t.Fatalf("symlink escape decision/reason = %s/%q, want reject with outside workspace", result.Decision, result.Reason)
	}
}

func TestWorkspacePrecedesAllowedAndAuto(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	g := NewGuardWithConfigModeAndWorkspace(nil, "test", ModeAuto, root, nil, nil, []string{`.*`}, []string{"readfile"})
	result := g.Check(context.Background(), "readfile", map[string]any{"path": filepath.Join(outside, "allowed.txt")})
	if result.Decision != Reject || !strings.Contains(result.Reason, "outside workspace") {
		t.Fatalf("workspace precedence decision/reason = %s/%q, want reject with outside workspace", result.Decision, result.Reason)
	}
}

func TestWorkspaceExecUsesExecutionContextCWD(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	g := NewGuardWithConfigModeAndWorkspace(nil, "test", ModeAuto, root, nil, nil, nil, nil)
	ctx := tools.WithExecutionContext(context.Background(), tools.ExecutionContext{CWD: root})

	inside := g.Check(ctx, "exec", map[string]any{"command": "pwd"})
	if inside.Decision == Reject || inside.Audit == "workspace_reject" {
		t.Fatalf("inside execution context decision/audit = %s/%q, want non-workspace rejection", inside.Decision, inside.Audit)
	}
	outsideCWD := g.Check(ctx, "exec", map[string]any{"command": "pwd", "cwd": outside})
	if outsideCWD.Decision != Reject || outsideCWD.Audit != "workspace_reject" {
		t.Fatalf("outside cwd decision/audit = %s/%q, want reject/workspace_reject", outsideCWD.Decision, outsideCWD.Audit)
	}
}

func TestWorkspaceExecDoesNotTreatOrdinarySlashArgumentAsPath(t *testing.T) {
	root := t.TempDir()
	g := NewGuardWithConfigModeAndWorkspace(nil, "test", ModeAuto, root, nil, nil, nil, nil)
	result := g.Check(context.Background(), "exec", map[string]any{"command": "printf '%s' namespace/resource", "cwd": root})
	if result.Audit == "workspace_reject" {
		t.Fatalf("ordinary slash argument audit = %q, want normal Guard flow", result.Audit)
	}
}

func TestWorkspaceExecAllowsStandaloneQuotedSlashProse(t *testing.T) {
	root := t.TempDir()
	g := NewGuardWithConfigModeAndWorkspace(nil, "test", ModeAuto, root, nil, nil, nil, nil)
	commands := []string{
		`git tag -m "GLOBAL / PROJECT" v1.0.0`,
		`printf '%s' "before / after"`,
	}
	for _, command := range commands {
		result := g.Check(context.Background(), "exec", map[string]any{"command": command, "cwd": root})
		if result.Audit == "workspace_reject" {
			t.Fatalf("standalone slash prose %q audit = %q, want normal Guard flow", command, result.Audit)
		}
	}
}

func TestWorkspaceExecKeepsAmbiguousQuotedPathsBlocked(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	g := NewGuardWithConfigModeAndWorkspace(nil, "test", ModeAuto, root, nil, nil, nil, nil)
	commands := []string{
		`cat "` + filepath.Join(outside, "secret.txt") + `"`,
	}
	if runtime.GOOS != "windows" {
		// 这些命令依赖 POSIX 根路径和 shell 语义；Windows 使用自身的路径与 shell 规则。
		commands = append(commands,
			`printf '%s' "mentions /tmp here"`,
			`printf 'ls / ' | sh`,
			`eval "ls / "`,
			`python3 -Bc "import os; os.system('ls / ')"`,
			`bash -xc "ls / "`,
			`sh -c "cat /"`,
			`sh -c "cat `+filepath.Join(outside, "secret.txt")+`"`,
			`python -c "open('`+filepath.Join(outside, "secret.txt")+`').read()"`,
			`cat "unterminated`,
		)
	}
	for _, command := range commands {
		result := g.Check(context.Background(), "exec", map[string]any{"command": command, "cwd": root})
		if result.Decision != Reject || result.Audit != "workspace_reject" {
			t.Fatalf("ambiguous path command %q decision/audit = %s/%q, want reject/workspace_reject", command, result.Decision, result.Audit)
		}
	}
}

func TestWorkspaceBlocksExecCWDAndCommandPaths(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	g := NewGuardWithConfigModeAndWorkspace(nil, "test", ModeAuto, root, nil, nil, nil, nil)

	tests := []struct {
		name       string
		params     map[string]any
		reasonPart string
	}{
		{name: "outside cwd", params: map[string]any{"command": "ls", "cwd": outside}, reasonPart: "outside workspace"},
		{name: "absolute path", params: map[string]any{"command": "cat " + filepath.Join(outside, "secret.txt"), "cwd": root}, reasonPart: "outside workspace"},
		{name: "relative escape", params: map[string]any{"command": "cat ../outside.txt", "cwd": root}, reasonPart: "outside workspace"},
		{name: "cd outside", params: map[string]any{"command": "cd " + outside, "cwd": root}, reasonPart: "outside workspace"},
		{name: "cd parent", params: map[string]any{"command": "cd ..", "cwd": root}, reasonPart: "outside workspace"},
		{name: "quoted interpreter path", params: map[string]any{"command": `python -c 'print(open("` + filepath.Join(outside, "secret.txt") + `").read())'`, "cwd": root}, reasonPart: "outside workspace"},
		{name: "dynamic shell expression", params: map[string]any{"command": `cat "$HOME/.ssh/id_rsa"`, "cwd": root}, reasonPart: ""},
		{name: "dynamic positional expression", params: map[string]any{"command": `printf '%s' "$1"`, "cwd": root}, reasonPart: ""},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			result := g.Check(context.Background(), "exec", tt.params)
			if tt.reasonPart == "" {
				if result.Decision == Reject || result.Audit == "workspace_reject" {
					t.Fatalf("exec %s decision/audit = %s/%q, want normal Guard flow", tt.name, result.Decision, result.Audit)
				}
				return
			}
			if result.Decision != Reject || !strings.Contains(result.Reason, tt.reasonPart) {
				t.Fatalf("exec %s decision/reason = %s/%q, want reject with %q", tt.name, result.Decision, result.Reason, tt.reasonPart)
			}
		})
	}
}

func TestReviewParamsKeepNonSensitiveOperationSemantics(t *testing.T) {
	params, truncated := marshalReviewParams(map[string]any{
		"path":        "internal/agent/tools.go",
		"recursive":   true,
		"overwrite":   false,
		"content":     "very-secret-file-content",
		"edits":       []any{map[string]any{"old_string": "very-secret-old-content", "new_string": "very-secret-new-content", "target": "all"}},
		"headers":     map[string]any{"Authorization": "Bearer test-api-key", "X-Scope": "workspace"},
		"query_scope": "all",
	})
	if truncated {
		t.Fatal("ordinary sanitized params unexpectedly truncated")
	}
	for _, secret := range []string{"very-secret-file-content", "very-secret-old-content", "very-secret-new-content", "Bearer test-api-key"} {
		if strings.Contains(params, secret) {
			t.Fatalf("review params = %q, must not contain %q", params, secret)
		}
	}
	for _, fact := range []string{`"path":"internal/agent/tools.go"`, `"recursive":true`, `"overwrite":false`, `"target":"all"`, `"X-Scope":"workspace"`, `"query_scope":"all"`, `"Authorization":"***REDACTED_AUTHORIZATION`} {
		if !strings.Contains(params, fact) {
			t.Fatalf("review params = %q, want %q", params, fact)
		}
	}
}

func TestReviewParamsOversizedInputKeepsRiskCriticalFields(t *testing.T) {
	params, truncated := marshalReviewParams(map[string]any{
		"action":    "remove",
		"path":      "build",
		"recursive": true,
		"overwrite": true,
		"extra":     strings.Repeat("x", reviewParamsLimit),
	})
	if !truncated || !strings.Contains(params, `"summary_truncated":true`) {
		t.Fatalf("review params = %q, truncated = %v; want explicit fallback", params, truncated)
	}
	for _, fact := range []string{`"action":"remove"`, `"path":"build"`, `"recursive":true`, `"overwrite":true`, `"omitted_keys":["extra"]`} {
		if !strings.Contains(params, fact) {
			t.Fatalf("review params = %q, want %q", params, fact)
		}
	}
}

func TestSafeTargetPreservesOperationScope(t *testing.T) {
	target := SafeTarget("http", map[string]any{"method": "POST", "url": "https://api.example.com/upload?all=true&token=supersecrettoken"})
	if !strings.Contains(target, "all=true") || !strings.Contains(target, "REDACTED") {
		t.Fatalf("HTTP target = %q, want visible scope and masked token", target)
	}
}

func TestSmartReviewReceivesIntentContext(t *testing.T) {
	g := NewGuardWithMode(nil, "test", ModeSmart)
	var got ReviewRequest
	g.SetLLMReviewer(func(ctx context.Context, req ReviewRequest) (string, error) {
		got = req
		return `{"decision":"approve","reason":"aligned","suggestion":""}`, nil
	})
	ctx := ReviewContext{Evidence: "Latest direct user message:\n- prepare a report\n\nRecent resolved risk decisions:\n- Approved: tool=writefile"}
	result := g.Check(context.Background(), "writefile", map[string]any{"path": "report.md", "content": "hello"}, ctx)
	if result.Decision != Approve || result.Source != "llm" {
		t.Fatalf("smart review decision/source = %s/%s, want approve/llm", result.Decision, result.Source)
	}
	if got.Context.Evidence != ctx.Evidence {
		t.Fatalf("review request context = %#v, want %#v", got.Context, ctx)
	}
	if got.Risk != "medium" || got.ToolName != "writefile" || got.Target != "report.md" {
		t.Fatalf("review request metadata = %#v", got)
	}
}

func TestSmartReviewModifyIsDecision(t *testing.T) {
	g := NewGuardWithMode(nil, "test", ModeSmart)
	g.SetLLMReviewer(func(ctx context.Context, req ReviewRequest) (string, error) {
		return `{"decision":"modify","reason":"too broad","suggestion":"use a narrower operation"}`, nil
	})
	result := g.Check(context.Background(), "writefile", map[string]any{"path": "out.txt", "content": "hello"}, ReviewContext{Evidence: "Latest direct user message:\n- create output"})
	if result.Decision != Modify || result.Suggestion != "use a narrower operation" {
		t.Fatalf("Check() result = %#v, want modify with suggestion", result)
	}
}
