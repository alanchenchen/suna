package builtin

import (
	"math"
	"time"

	"github.com/alanchenchen/suna/internal/tools"
)

func (Exec) Spec() tools.Spec {
	// 顶层暴露全部字段；操作之间的组合约束由入口校验统一执行。
	return builtinSpec("exec", "Run or manage a stateful shell command. Prefer dedicated file, search, and HTTP tools for supported operations. Keep cwd, path arguments, and redirects inside the configured workspace; use workspace-local temp files instead of /tmp. Use the Suna data directory only for explicit Suna-specific tasks. Omit action to run. To start a background command, use background=true, not job_id. Use the returned job_id with action=status or action=stop.", tools.Act, map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"action":     map[string]any{"type": "string", "enum": []string{"run", "status", "stop"}, "default": "run", "description": "Operation. Omit to run. Run requires command; status and stop require a returned job_id and reject run-only fields"},
			"command":    map[string]any{"type": "string", "minLength": 1, "description": "Shell command to execute. Required for run only. Prefer dedicated tools over redirection. Keep path arguments and redirects inside the workspace."},
			"cwd":        map[string]any{"type": "string", "description": "Run only. Optional working directory. Defaults to the session cwd; keep ordinary project work within the configured workspace."},
			"background": map[string]any{"type": "boolean", "default": false, "description": "Run only. Set background=true to start a background command and receive its job_id. Omit or false for foreground execution"},
			"scope":      map[string]any{"type": "string", "enum": []string{execScopeRun, execScopeSession}, "description": "Only valid with background=true. Default run: cleaned up when the owning run ends. Session: survives individual runs and is cleaned up on session deletion or daemon shutdown (not client detach); allowed only in the main boundary"},
			"timeout":    map[string]any{"type": "integer", "minimum": 1, "maximum": int64(math.MaxInt64 / int64(time.Second)), "description": "Run only. Total command lifetime in seconds, including process startup and execution. Foreground default: 60 seconds. Run-scoped background: no default timeout. Session-scoped background: one-hour default"},
			"env":        map[string]any{"type": "object", "description": "Run only. Environment variables added to the inherited environment", "additionalProperties": map[string]any{"type": "string"}},
			"shell":      map[string]any{"type": "string", "enum": []string{"auto", "bash", "powershell", "cmd"}, "description": "Run only. Shell type. Default auto"},
			"job_id":     map[string]any{"type": "string", "minLength": 1, "description": "Required for status and stop only. Use the job_id returned by background startup; never pass job_id when starting a command"},
			"cursor":     map[string]any{"type": "integer", "minimum": 0, "description": "Status only. Optional non-negative output cursor returned by an earlier response; defaults to zero"},
		},
	})
}
