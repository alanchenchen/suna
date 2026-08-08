package builtin

import "github.com/alanchenchen/suna/internal/tools"

func (Exec) Spec() tools.Spec {
	// 每个分支只暴露该操作可用的字段，避免模型拼出语义冲突的参数组合。
	foregroundRun := execRunProperties()
	foregroundRun["background"] = map[string]any{"type": "boolean", "enum": []bool{false}, "description": "Run in the foreground. May be omitted or false"}
	backgroundRun := execRunProperties()
	backgroundRun["background"] = map[string]any{"type": "boolean", "enum": []bool{true}, "description": "Run in the background. Must be true"}
	backgroundRun["scope"] = map[string]any{"type": "string", "enum": []string{execScopeRun, execScopeSession}, "description": "Background lifetime scope. Default run"}

	return builtinSpec("exec", "Run or manage a stateful shell command. Omit action to run; use action=status or action=stop with a background job_id.", tools.Act, map[string]any{
		"type": "object",
		"oneOf": []any{
			map[string]any{
				"title":                "Foreground run",
				"description":          "Run a command in the foreground. The total command lifetime defaults to 60 seconds.",
				"type":                 "object",
				"properties":           foregroundRun,
				"required":             []string{"command"},
				"additionalProperties": false,
			},
			map[string]any{
				"title":                "Background run",
				"description":          "Start a background command. Run-scoped jobs have no default timeout; session-scoped jobs default to a one-hour total command lifetime.",
				"type":                 "object",
				"properties":           backgroundRun,
				"required":             []string{"command", "background"},
				"additionalProperties": false,
			},
			map[string]any{
				"title":       "Background job status",
				"description": "Read the current status and incremental output of a background job.",
				"type":        "object",
				"properties": map[string]any{
					"action": map[string]any{"type": "string", "enum": []string{"status"}, "description": "Read background job status and output"},
					"job_id": map[string]any{"type": "string", "description": "Background job identifier"},
					"cursor": map[string]any{"type": "integer", "minimum": 0, "description": "Optional output cursor returned by an earlier status call"},
				},
				"required":             []string{"action", "job_id"},
				"additionalProperties": false,
			},
			map[string]any{
				"title":       "Stop background job",
				"description": "Request that a background job stop and report its current status.",
				"type":        "object",
				"properties": map[string]any{
					"action": map[string]any{"type": "string", "enum": []string{"stop"}, "description": "Stop a background job"},
					"job_id": map[string]any{"type": "string", "description": "Background job identifier"},
				},
				"required":             []string{"action", "job_id"},
				"additionalProperties": false,
			},
		},
	})
}

// execRunProperties 为两个 run 分支生成独立但完全一致的公共字段定义。
func execRunProperties() map[string]any {
	return map[string]any{
		"action":  map[string]any{"type": "string", "enum": []string{"run"}, "description": "Run a command. May be omitted; run is the default action"},
		"command": map[string]any{"type": "string", "description": "Shell command to execute"},
		"cwd":     map[string]any{"type": "string", "description": "Working directory"},
		"timeout": map[string]any{"type": "integer", "minimum": 1, "description": "Total command lifetime in seconds, including process startup and execution. Foreground default: 60 seconds. Run-scoped background default: no timeout. Session-scoped background default: 1 hour"},
		"env": map[string]any{
			"type":                 "object",
			"description":          "Environment variables added to the inherited environment",
			"additionalProperties": map[string]any{"type": "string"},
		},
		"shell": map[string]any{"type": "string", "enum": []string{"auto", "bash", "powershell", "cmd"}, "description": "Shell type. Default auto"},
	}
}
