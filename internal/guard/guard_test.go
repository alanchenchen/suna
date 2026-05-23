package guard

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		{name: "simple readonly", command: "ls -la", decision: Approve, risk: RiskLow},
		{name: "readonly pipeline", command: "git status | grep modified", decision: Approve, risk: RiskLow},
		{name: "bash compound write", command: "ls && rm -rf important", decision: Confirm, risk: RiskHigh},
		{name: "cmd compound write", command: "dir & del /s /q C:\\Users\\me", shell: "cmd", decision: Confirm, risk: RiskHigh},
		{name: "powershell compound write", command: "Get-ChildItem; Remove-Item -Recurse -Force C:\\Users\\me", shell: "powershell", decision: Confirm, risk: RiskHigh},
		{name: "redirection is not readonly", command: "echo hi > file.txt", decision: Confirm, risk: RiskMedium},
		{name: "find delete is not readonly", command: "find . -delete", decision: Confirm, risk: RiskMedium},
		{name: "nested shell is not readonly", command: "bash -c 'ls'", decision: Confirm, risk: RiskMedium},
		{name: "powershell encoded command is not readonly", command: "powershell -EncodedCommand SQBFAFgA", shell: "cmd", decision: Confirm, risk: RiskMedium},
		{name: "generic interpreter dynamic execution is not readonly", command: "node -e 'console.log(1)'", decision: Confirm, risk: RiskMedium},
		{name: "python process execution is high risk", command: "python -c 'import os; os.system(\"rm -rf x\")'", decision: Confirm, risk: RiskHigh},
	}

	for _, tt := range tests {
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
		t.Fatalf("marshalParams error: %v", err)
	}
	if !strings.Contains(encoded, `\"hello\"`) {
		t.Fatalf("marshalParams did not JSON-escape string: %s", encoded)
	}
	if strings.Contains(encoded, "sk-123456789012345678901234") || !strings.Contains(encoded, "REDACTED_ENV") {
		t.Fatalf("marshalParams did not mask secret: %s", encoded)
	}

	contentEncoded, err := marshalParams(map[string]any{"content": "secret source code"})
	if err != nil {
		t.Fatalf("marshalParams content error: %v", err)
	}
	if strings.Contains(contentEncoded, "secret source code") || !strings.Contains(contentEncoded, "sha256=") {
		t.Fatalf("marshalParams did not summarize content safely: %s", contentEncoded)
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
		{name: "readhttp", tool: "readhttp", params: map[string]any{"url": "http://169.254.169.254/latest/meta-data"}},
	}
	for _, tt := range tests {
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
		{name: "quoted interpreter path", params: map[string]any{"command": `python -c 'print(open("/etc/passwd").read())'`, "cwd": root}, reasonPart: "outside workspace"},
		{name: "shell expansion", params: map[string]any{"command": `cat "$HOME/.ssh/id_rsa"`, "cwd": root}, reasonPart: "cannot be safely checked"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := g.Check(context.Background(), "exec", tt.params)
			if result.Decision != Reject || !strings.Contains(result.Reason, tt.reasonPart) {
				t.Fatalf("exec %s decision/reason = %s/%q, want reject with %q", tt.name, result.Decision, result.Reason, tt.reasonPart)
			}
		})
	}
}
