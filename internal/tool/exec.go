package tool

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	execTimeout      = 60 * time.Second
	maxExecOutput    = 50 * 1024
	execDefaultShell = "bash"
)

type Exec struct{}

func (Exec) Name() string { return "exec" }
func (Exec) Description() string {
	return "执行系统命令。万能工具，可执行任何 shell 命令。自动检测 shell 类型。"
}
func (Exec) Category() Category { return Act }
func (Exec) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{"type": "string", "description": "要执行的命令"},
			"cwd":     map[string]any{"type": "string", "description": "工作目录"},
			"timeout": map[string]any{"type": "integer", "description": "超时秒数（默认60）"},
			"env":     map[string]any{"type": "object", "description": "环境变量"},
			"shell":   map[string]any{"type": "string", "description": "shell类型: auto|bash|powershell|cmd"},
		},
		"required": []string{"command"},
	}
}

func (Exec) Execute(ctx context.Context, params map[string]any) Result {
	command, _ := params["command"].(string)
	if command == "" {
		return ErrorResult("command is required")
	}

	timeout := execTimeout
	if t, ok := params["timeout"].(float64); ok && int(t) > 0 {
		timeout = time.Duration(int(t)) * time.Second
	}

	cwd, _ := params["cwd"].(string)
	if cwd == "" {
		cwd, _ = os.Getwd()
	} else {
		cwd = expandPath(cwd)
	}

	shell := "auto"
	if s, ok := params["shell"].(string); ok {
		shell = s
	}

	env := os.Environ()
	if e, ok := params["env"].(map[string]any); ok {
		for k, v := range e {
			if s, ok := v.(string); ok {
				env = append(env, fmt.Sprintf("%s=%s", k, s))
			}
		}
	}

	shellCmd, shellUsed := resolveShell(shell)
	if shellCmd == "" {
		return ErrorResult("cannot determine shell, please specify shell parameter")
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	if shellUsed == "cmd" {
		cmd = exec.CommandContext(ctx, shellCmd, "/c", command)
	} else {
		cmd = exec.CommandContext(ctx, shellCmd, "-c", command)
	}
	cmd.Dir = cwd
	cmd.Env = env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	outStr := stdout.String()
	errStr := stderr.String()
	truncated := false

	if len(outStr) > maxExecOutput {
		outStr = outStr[:maxExecOutput] + "\n... (truncated)"
		truncated = true
	}
	if len(errStr) > maxExecOutput {
		errStr = errStr[:maxExecOutput] + "\n... (truncated)"
		truncated = true
	}

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return ErrorResult(fmt.Sprintf("exec error: %s", err))
		}
	}

	var sb strings.Builder
	if outStr != "" {
		sb.WriteString(outStr)
	}
	if errStr != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("[stderr]\n")
		sb.WriteString(errStr)
	}
	if exitCode != 0 {
		sb.WriteString(fmt.Sprintf("\n[exit code: %d]", exitCode))
		return Result{Content: sb.String(), Error: fmt.Sprintf("command exited with code %d", exitCode), IsError: true, Truncated: truncated}
	}

	return Result{Content: sb.String(), Truncated: truncated}
}

func resolveShell(shell string) (cmd string, name string) {
	if shell != "auto" {
		return findShell(shell)
	}
	return autoShell()
}

func findShell(name string) (string, string) {
	switch strings.ToLower(name) {
	case "bash":
		if p, err := exec.LookPath("bash"); err == nil {
			return p, "bash"
		}
		if p, err := exec.LookPath("sh"); err == nil {
			return p, "sh"
		}
		return "", ""
	case "powershell":
		if p, err := exec.LookPath("powershell"); err == nil {
			return p, "powershell"
		}
		if p, err := exec.LookPath("pwsh"); err == nil {
			return p, "powershell"
		}
		return "", ""
	case "cmd":
		p := filepath.Join(os.Getenv("SystemRoot"), "system32", "cmd.exe")
		if _, err := os.Stat(p); err == nil {
			return p, "cmd"
		}
		if p, err := exec.LookPath("cmd"); err == nil {
			return p, "cmd"
		}
		return "", ""
	default:
		return "", ""
	}
}
