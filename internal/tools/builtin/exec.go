package builtin

import (
	"context"
	"fmt"
	"github.com/alanchenchen/suna/internal/tools"
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

func (Exec) Spec() tools.Spec {
	return builtinSpec("exec", "Run a shell command. Use for diagnostics, tests, builds, and other system operations.", tools.Act, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{"type": "string", "description": "Command to execute"},
			"cwd":     map[string]any{"type": "string", "description": "Working directory"},
			"timeout": map[string]any{"type": "integer", "description": "Timeout seconds. Default 60"},
			"env":     map[string]any{"type": "object", "description": "Environment variables"},
			"shell":   map[string]any{"type": "string", "description": "Shell type: auto|bash|powershell|cmd. Default auto"},
		},
		"required": []string{"command"},
	})
}

func (Exec) Execute(ctx context.Context, params map[string]any) tools.Result {
	command, _ := params["command"].(string)
	if command == "" {
		return tools.ErrorResult("command is required")
	}

	timeout := execTimeout
	if t, ok := params["timeout"].(float64); ok && int(t) > 0 {
		timeout = time.Duration(int(t)) * time.Second
	}

	cwd, _ := params["cwd"].(string)
	if cwd == "" {
		if execCtx, ok := tools.ExecutionContextFrom(ctx); ok && execCtx.CWD != "" {
			cwd = execCtx.CWD
		} else {
			cwd, _ = os.Getwd()
		}
	} else {
		cwd = expandPathWithContext(ctx, cwd)
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
		return tools.ErrorResult("cannot determine shell, please specify shell parameter")
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

	stdout := &limitedBuffer{limit: maxExecOutput}
	stderr := &limitedBuffer{limit: maxExecOutput}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()

	outStr := stdout.String()
	errStr := stderr.String()
	truncated := stdout.truncated || stderr.truncated

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return tools.ErrorResult(fmt.Sprintf("exec error: %s", err))
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
		return tools.Result{Content: sb.String(), Error: fmt.Sprintf("command exited with code %d", exitCode), IsError: true, Truncated: truncated}
	}

	return tools.Result{Content: sb.String(), Truncated: truncated}
}

type limitedBuffer struct {
	limit     int
	buf       strings.Builder
	truncated bool
}

func (w *limitedBuffer) Write(p []byte) (int, error) {
	if w.limit <= 0 {
		return len(p), nil
	}
	remaining := w.limit - w.buf.Len()
	if remaining <= 0 {
		w.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = w.buf.Write(p[:remaining])
		w.truncated = true
		return len(p), nil
	}
	_, _ = w.buf.Write(p)
	return len(p), nil
}

func (w *limitedBuffer) String() string {
	out := w.buf.String()
	if w.truncated {
		out += "\n... (truncated)"
	}
	return out
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
