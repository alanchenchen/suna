//go:build !windows

package builtin

import (
	"reflect"
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/tools"
)

func TestExecSpecSeparatesValidOperationShapes(t *testing.T) {
	parameters := Exec{}.Spec().Parameters
	if parameters["type"] != "object" {
		t.Fatalf("顶层 type = %#v，期望 object", parameters["type"])
	}
	branches, ok := parameters["oneOf"].([]any)
	if !ok || len(branches) != 4 {
		t.Fatalf("oneOf = %#v，期望四个分支", parameters["oneOf"])
	}

	// 两个 run 分支的执行公共字段必须完全相同，防止文档语义漂移。
	foreground := branches[0].(map[string]any)
	background := branches[1].(map[string]any)
	foregroundProperties := foreground["properties"].(map[string]any)
	backgroundProperties := background["properties"].(map[string]any)
	for _, name := range []string{"cwd", "timeout", "env", "shell"} {
		if !reflect.DeepEqual(foregroundProperties[name], backgroundProperties[name]) {
			t.Fatalf("公共字段 %s 不一致：前台=%#v 后台=%#v", name, foregroundProperties[name], backgroundProperties[name])
		}
	}
	for index, raw := range branches {
		branch := raw.(map[string]any)
		if branch["type"] != "object" || branch["additionalProperties"] != false {
			t.Fatalf("分支 %d 未封闭为 object：%#v", index, branch)
		}
	}
	if got := foregroundProperties["shell"].(map[string]any)["enum"]; !reflect.DeepEqual(got, []string{"auto", "bash", "powershell", "cmd"}) {
		t.Fatalf("shell enum = %#v", got)
	}
	envAdditional := foregroundProperties["env"].(map[string]any)["additionalProperties"]
	if !reflect.DeepEqual(envAdditional, map[string]any{"type": "string"}) {
		t.Fatalf("env additionalProperties = %#v", envAdditional)
	}
	description := foregroundProperties["timeout"].(map[string]any)["description"].(string) + " " +
		foreground["description"].(string) + " " + background["description"].(string)
	for _, fact := range []string{"Total command lifetime", "60 seconds", "no default timeout", "one-hour"} {
		if !strings.Contains(strings.ToLower(description), strings.ToLower(fact)) {
			t.Fatalf("timeout 描述缺少 %q：%q", fact, description)
		}
	}
	execDescription := Exec{}.Spec().Description + " " +
		foregroundProperties["command"].(map[string]any)["description"].(string) + " " +
		foregroundProperties["cwd"].(map[string]any)["description"].(string)
	for _, fact := range []string{"Prefer dedicated file, search, and HTTP tools", "cwd", "path arguments", "redirects", "configured workspace", "workspace-local temp files", "instead of /tmp", "session cwd"} {
		if !strings.Contains(strings.ToLower(execDescription), strings.ToLower(fact)) {
			t.Fatalf("exec 描述缺少 %q：%q", fact, execDescription)
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
			if got := execSchemaAccepts(parameters, test.params); got != test.valid {
				t.Fatalf("schema accepts = %v，期望 %v；参数=%#v", got, test.valid, test.params)
			}
		})
	}
}

// execSchemaAccepts 只覆盖本测试使用的 JSON Schema 关键字，用组合表验证 oneOf 边界。
func execSchemaAccepts(schema map[string]any, params map[string]any) bool {
	matched := 0
	for _, raw := range schema["oneOf"].([]any) {
		if execSchemaBranchAccepts(raw.(map[string]any), params) {
			matched++
		}
	}
	return matched == 1
}

func execSchemaBranchAccepts(branch map[string]any, params map[string]any) bool {
	properties := branch["properties"].(map[string]any)
	for _, required := range branch["required"].([]string) {
		if _, ok := params[required]; !ok {
			return false
		}
	}
	for name, value := range params {
		raw, ok := properties[name]
		if !ok {
			return false
		}
		property := raw.(map[string]any)
		switch property["type"] {
		case "string":
			if _, ok := value.(string); !ok {
				return false
			}
		case "boolean":
			if _, ok := value.(bool); !ok {
				return false
			}
		case "integer":
			number, ok := value.(float64)
			if !ok || number != float64(int64(number)) {
				return false
			}
			if minimum, ok := property["minimum"].(int); ok && number < float64(minimum) {
				return false
			}
		case "object":
			object, ok := value.(map[string]any)
			if !ok {
				return false
			}
			additional, _ := property["additionalProperties"].(map[string]any)
			for _, item := range object {
				if additional["type"] == "string" {
					if _, ok := item.(string); !ok {
						return false
					}
				}
			}
		}
		if enum, ok := property["enum"]; ok && !execSchemaEnumContains(enum, value) {
			return false
		}
	}
	return true
}

func execSchemaEnumContains(enum, value any) bool {
	switch values := enum.(type) {
	case []string:
		for _, candidate := range values {
			if candidate == value {
				return true
			}
		}
	case []bool:
		for _, candidate := range values {
			if candidate == value {
				return true
			}
		}
	}
	return false
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
