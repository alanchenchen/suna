package guard

import (
	"context"
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
