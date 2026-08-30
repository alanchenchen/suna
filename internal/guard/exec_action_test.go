package guard

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuardExecActionReadOnlyAndWorkspace(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	g := NewGuardWithConfigModeAndWorkspace(nil, "test", ModeAsk, root, nil, nil, nil, nil)

	// 未指定 action 和显式 run 都必须继续按 command 评估只读性（写重定向非只读 → ask 确认）。
	for _, params := range []map[string]any{
		{"command": "echo hi > out.txt", "cwd": root},
		{"action": "run", "command": "echo hi > out.txt", "cwd": root},
	} {
		result := g.Check(context.Background(), "exec", params)
		if result.Decision != Confirm || result.ReadOnly {
			t.Fatalf("run decision/readOnly = %s/%v, want confirm/false", result.Decision, result.ReadOnly)
		}
	}

	// 未指定 action 和显式 run 都必须继续检查 workspace。
	for _, params := range []map[string]any{
		{"command": "cat " + filepath.Join(outside, "secret.txt"), "cwd": root},
		{"action": "run", "command": "cat " + filepath.Join(outside, "secret.txt"), "cwd": root},
	} {
		result := g.Check(context.Background(), "exec", params)
		if result.Decision != Reject || result.Audit != "workspace_reject" {
			t.Fatalf("run decision/audit = %s/%q, want reject/workspace_reject", result.Decision, result.Audit)
		}
	}

	// status/stop 只使用受管 job_id，不应因没有 command 或无关 cwd 被 workspace 拒绝。
	status := g.Check(context.Background(), "exec", map[string]any{"action": "status", "job_id": "job-42", "cwd": outside})
	if status.Decision != Approve || !status.ReadOnly || status.Audit == "workspace_reject" {
		t.Fatalf("status result = %#v, want approve/readonly without workspace rejection", status)
	}
	stop := g.Check(context.Background(), "exec", map[string]any{"action": "stop", "job_id": "job-42", "cwd": outside})
	if stop.Decision != Confirm || stop.ReadOnly || stop.Audit == "workspace_reject" {
		t.Fatalf("stop result = %#v, want confirm/non-readonly without workspace rejection", stop)
	}
}

func TestGuardExecActionTargets(t *testing.T) {
	// job_id 目标与命令目标一样先脱敏，同时保留 action，供规则与 Smart Review 准确判断。
	jobID := "token=" + "super" + "secret123"
	for _, action := range []string{"status", "stop"} {
		params := map[string]any{"action": action, "job_id": jobID}
		want := action + " ***REDACTED_CREDENTIAL***"
		if got := ruleTarget("exec", params); got != want {
			t.Fatalf("ruleTarget(%s) = %q, want %q", action, got, want)
		}
		if got := SafeTarget("exec", params); got != want {
			t.Fatalf("SafeTarget(%s) = %q, want %q", action, got, want)
		}
	}

	command := "git status"
	if got := ruleTarget("exec", map[string]any{"command": command}); got != command {
		t.Fatalf("default run rule target = %q, want %q", got, command)
	}
	if got := SafeTarget("exec", map[string]any{"action": "run", "command": command}); got != command {
		t.Fatalf("explicit run safe target = %q, want %q", got, command)
	}
}

func TestGuardExecStopUsesSmartReview(t *testing.T) {
	g := NewGuardWithMode(nil, "test", ModeSmart)
	called := false
	g.SetLLMReviewer(func(_ context.Context, req ReviewRequest) (string, error) {
		called = true
		if req.Target != "stop job-42" {
			t.Fatalf("stop review request target = %q, want %q", req.Target, "stop job-42")
		}
		return `{"decision":"approve","reason":"managed job stop is expected"}`, nil
	})

	result := g.Check(context.Background(), "exec", map[string]any{"action": "stop", "job_id": "job-42"})
	if !called || result.Decision != Approve || result.Source != "llm" {
		t.Fatalf("smart stop result = %#v, reviewer called = %v; want LLM approve", result, called)
	}
}

func TestReadonlyExecActionOnlyAllowsStatus(t *testing.T) {
	g := NewGuardWithMode(nil, "test", ModeReadonly)
	status := g.Check(context.Background(), "exec", map[string]any{"action": "status", "job_id": "job-42"})
	if status.Decision != Approve || !status.ReadOnly {
		t.Fatalf("readonly status = %s/%v, want approve/readonly", status.Decision, status.ReadOnly)
	}
	stop := g.Check(context.Background(), "exec", map[string]any{"action": "stop", "job_id": "job-42"})
	if stop.Decision != Reject || stop.ReadOnly || !strings.Contains(stop.Reason, "readonly") {
		t.Fatalf("readonly stop = %#v, want reject/non-readonly", stop)
	}
}
