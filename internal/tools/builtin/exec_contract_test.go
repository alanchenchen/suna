package builtin

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/tools"
)

func TestExecSpecExposesTopLevelParameters(t *testing.T) {
	parameters := Exec{}.Spec().Parameters
	if parameters["type"] != "object" {
		t.Fatalf("顶层 type = %#v，期望 object", parameters["type"])
	}
	if _, exists := parameters["oneOf"]; exists || parameters["additionalProperties"] != false {
		t.Fatalf("schema is not a closed top-level object: %#v", parameters)
	}
	properties := parameters["properties"].(map[string]any)
	names := []string{"action", "command", "cwd", "timeout", "env", "shell", "background", "scope", "job_id", "cursor"}
	if len(properties) != len(names) {
		t.Fatalf("properties = %#v", properties)
	}
	for _, name := range names {
		if _, ok := properties[name]; !ok {
			t.Fatalf("missing property %s", name)
		}
	}
	if action := properties["action"].(map[string]any); action["default"] != "run" || !reflect.DeepEqual(action["enum"], []string{"run", "status", "stop"}) {
		t.Fatalf("action = %#v", action)
	}
	if got := properties["shell"].(map[string]any)["enum"]; !reflect.DeepEqual(got, []string{"auto", "bash", "powershell", "cmd"}) {
		t.Fatalf("shell enum = %#v", got)
	}
	if got := properties["env"].(map[string]any)["additionalProperties"]; !reflect.DeepEqual(got, map[string]any{"type": "string"}) {
		t.Fatalf("env = %#v", got)
	}
	description := Exec{}.Spec().Description
	for _, raw := range properties {
		description += " " + raw.(map[string]any)["description"].(string)
	}
	for _, fact := range []string{"background=true", "returned job_id", "never pass job_id", "owning run ends", "session deletion", "main boundary", "Total command lifetime", "60 seconds", "no default timeout", "one-hour", "Prefer dedicated file, search, and HTTP tools", "configured workspace", "workspace-local temp files", "session cwd"} {
		if !strings.Contains(description, fact) {
			t.Fatalf("description missing %q", fact)
		}
	}

	tests := []struct {
		name   string
		params map[string]any
		valid  bool
	}{
		{name: "默认前台 run", params: map[string]any{"command": "pwd"}, valid: true},
		{name: "显式前台 run", params: map[string]any{"action": "run", "command": "pwd", "background": false, "cwd": ".", "timeout": float64(1), "env": map[string]any{"A": "B"}, "shell": "bash"}, valid: true},
		{name: "默认后台 run", params: map[string]any{"command": "pwd", "background": true}, valid: true},
		{name: "session 后台 run", params: map[string]any{"action": "run", "command": "pwd", "background": true, "scope": "session"}, valid: true},
		{name: "status", params: map[string]any{"action": "status", "job_id": "job"}, valid: true},
		{name: "status cursor", params: map[string]any{"action": "status", "job_id": "job", "cursor": float64(0)}, valid: true},
		{name: "stop", params: map[string]any{"action": "stop", "job_id": "job"}, valid: true},
		{name: "run 缺 command", params: map[string]any{}, valid: false},
		{name: "前台禁止 scope", params: map[string]any{"command": "pwd", "scope": "run"}, valid: false},
		{name: "前台禁止 job_id", params: map[string]any{"command": "pwd", "job_id": "job"}, valid: false},
		{name: "前台禁止 cursor", params: map[string]any{"command": "pwd", "cursor": float64(0)}, valid: false},
		{name: "后台必须为 true", params: map[string]any{"command": "pwd", "background": false, "scope": "session"}, valid: false},
		{name: "后台非法 scope", params: map[string]any{"command": "pwd", "background": true, "scope": "other"}, valid: false},
		{name: "status 必须显式 action", params: map[string]any{"job_id": "job"}, valid: false},
		{name: "status 缺 job_id", params: map[string]any{"action": "status"}, valid: false},
		{name: "status 禁止 cwd", params: map[string]any{"action": "status", "job_id": "job", "cwd": "."}, valid: false},
		{name: "status cursor 非负", params: map[string]any{"action": "status", "job_id": "job", "cursor": float64(-1)}, valid: false},
		{name: "stop 仅允许 job_id", params: map[string]any{"action": "stop", "job_id": "job", "cursor": float64(0)}, valid: false},
		{name: "非法 shell", params: map[string]any{"command": "pwd", "shell": "zsh"}, valid: false},
		{name: "env 值必须为字符串", params: map[string]any{"command": "pwd", "env": map[string]any{"A": float64(1)}}, valid: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validateExecParams(test.params) == nil; got != test.valid {
				t.Fatalf("validation accepts = %v，期望 %v；参数=%#v", got, test.valid, test.params)
			}
			if !test.valid {
				result := (Exec{}).Execute(context.Background(), test.params)
				if !result.IsError || result.Metadata["exec_status"] != execStatusStartFailed {
					t.Fatalf("Execute accepted invalid parameters: %#v", result)
				}
			}
		})
	}
}

func TestExecRejectsInvalidParamsBeforeStart(t *testing.T) {
	// 使用跨平台重定向命令验证无效参数不会启动进程。
	marker := filepath.Join(t.TempDir(), "must-not-exist")
	command := `echo must-not-run > "` + marker + `"`
	tests := []map[string]any{
		{"unknown": true}, {"action": nil}, {"action": ""}, {"action": "other"},
		{"command": 1}, {"cwd": nil}, {"background": "true"}, {"scope": "run"},
		{"shell": ""}, {"intent": 1}, {"job_id": "invented"}, {"cursor": float64(0)},
		{"env": "A=B"}, {"env": map[string]string{"A": "B"}}, {"env": map[string]any{"A": nil}},
		{"timeout": 1}, {"timeout": nil}, {"timeout": float64(0)}, {"timeout": -1.0},
		{"timeout": 1.5}, {"timeout": math.NaN()}, {"timeout": math.Inf(1)},
		{"timeout": float64(math.MaxInt64)}, {"timeout": float64(9223372037)},
		{"action": "status", "job_id": "job", "cursor": math.NaN()},
		{"action": "status", "job_id": "job", "cursor": math.Inf(1)},
		{"action": "status", "job_id": "job", "cursor": float64(math.MaxInt64)},
		{"action": "status", "job_id": "job", "cursor": 1.5},
		{"action": "status", "job_id": "job", "cursor": 1},
		{"action": "stop", "job_id": "job", "cursor": float64(0)},
	}
	for i, invalid := range tests {
		params := map[string]any{"command": command}
		for key, value := range invalid {
			params[key] = value
		}
		if action, _ := params["action"].(string); action == "status" || action == "stop" {
			delete(params, "command")
		}
		if err := validateExecParams(params); err == nil {
			t.Fatalf("case %d accepted", i)
		}
		result := (Exec{}).Execute(context.Background(), params)
		if !result.IsError || result.Metadata["exec_status"] != execStatusStartFailed || !strings.Contains(result.Content, "Error:") {
			t.Fatalf("case %d: %#v", i, result)
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("invalid call created marker: %v", err)
	}
}

func TestExecParameterNumericBoundariesAndIntent(t *testing.T) {
	for _, params := range []map[string]any{
		{"command": "echo ok", "timeout": float64(9223372036), "intent": "test"},
		{"action": "status", "job_id": "job", "cursor": math.Nextafter(0x1p63, 0), "intent": "test"},
		{"action": "stop", "job_id": "job", "intent": "test"},
	} {
		if err := validateExecParams(params); err != nil {
			t.Fatalf("valid params: %v", err)
		}
	}
	result := (Exec{}).Execute(context.Background(), map[string]any{"command": "echo exec-contract-ok", "intent": "test"})
	if result.IsError || !strings.Contains(result.Content, "exec-contract-ok") {
		t.Fatalf("default run: %#v", result)
	}
}

func TestMakeExecResultStatesErrorsAsFailures(t *testing.T) {
	tests := []struct {
		name      string
		result    tools.Result
		contains  []string
		forbidden []string
	}{
		{
			name:      "missing job id is invalid",
			result:    makeExecResult("status", "", "", execStatusNotFound, nil, true, "job_id is required", false, nil),
			contains:  []string{"status request is invalid", "no job ID was provided", "Error: job_id is required"},
			forbidden: []string{"finished", "stopped", "running"},
		},
		{
			name:      "job not found",
			result:    makeExecResult("stop", "", "missing", execStatusNotFound, nil, true, "job not found", false, nil),
			contains:  []string{"stop request could not find job missing", "Error: job not found"},
			forbidden: []string{"finished", "stopped"},
		},
		{
			name:      "access denied",
			result:    makeExecResult("status", "", "private", execStatusAccessDenied, nil, true, "job belongs to another session", false, nil),
			contains:  []string{"status request was denied access to job private"},
			forbidden: []string{"finished", "stopped", "running"},
		},
		{
			name:      "invalid cursor does not claim running",
			result:    makeExecResult("status", "run", "job-1", execStatusRunning, nil, true, "cursor must be a non-negative integer", false, nil),
			contains:  []string{"status request for job job-1 failed", "Error: cursor must be a non-negative integer"},
			forbidden: []string{"is running", "finished", "stopped"},
		},
		{
			name:      "terminal stop reports current status",
			result:    makeExecResult("stop", "run", "job-1", execStatusExited, nil, false, "", false, nil),
			contains:  []string{"stop request for job job-1 completed", "Current status: exited"},
			forbidden: []string{"stopped job", "just stopped"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, text := range test.contains {
				if !strings.Contains(test.result.Content, text) {
					t.Fatalf("Content 缺少 %q：%q", text, test.result.Content)
				}
			}
			for _, text := range test.forbidden {
				if strings.Contains(test.result.Content, text) {
					t.Fatalf("Content 不应包含 %q：%q", text, test.result.Content)
				}
			}
		})
	}
}

func TestMakeExecResultUsesStableReadableContent(t *testing.T) {
	extra := map[string]any{
		"started_at":      "kept-only-in-metadata",
		"cleanup_status":  "complete",
		"next_cursor":     int64(23),
		"timeout_seconds": int64(60),
		"duration_ms":     int64(12),
		"exit_code":       7,
	}
	result := makeExecResult("status", "session", "job-1", execStatusExited, []byte("original output"), true, "bad\nthing", true, extra)
	want := "Exec status request for job job-1 failed.\nScope: session. Exit code: 7. Elapsed: 12 ms. Timeout: 60 seconds. Next output cursor: 23. Cleanup: complete. Output truncated: true.\nError: bad thing\noriginal output"
	if result.Content != want {
		t.Fatalf("Content = %q，期望 %q", result.Content, want)
	}
	if result.Metadata["started_at"] != "kept-only-in-metadata" || result.Metadata["exit_code"] != 7 {
		t.Fatalf("Metadata 未保持结构化字段：%#v", result.Metadata)
	}
	if strings.Contains(result.Content, "started_at") || strings.Contains(result.Content, "action=status") {
		t.Fatalf("Content 包含机器协议字段：%q", result.Content)
	}
}
