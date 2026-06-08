package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
)

type stdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr *bufio.Scanner
	mu     sync.Mutex
}

func startStdio(command string, args []string, cwd string, env map[string]string) (*stdioTransport, error) {
	if strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("mcp stdio command is required")
	}
	cmd := exec.Command(command, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Env = buildEnv(env)
	setProcessGroup(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	t := &stdioTransport{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdoutPipe), stderr: bufio.NewScanner(stderrPipe)}
	go t.drainStderr()
	return t, nil
}

func buildEnv(extra map[string]string) []string {
	allow := map[string]bool{"PATH": true, "HOME": true, "LANG": true, "LC_ALL": true, "TMPDIR": true, "TEMP": true, "TMP": true}
	base := make([]string, 0)
	for _, kv := range os.Environ() {
		key, _, ok := strings.Cut(kv, "=")
		if ok && (allow[key] || strings.HasPrefix(key, "LC_")) {
			base = append(base, kv)
		}
	}
	for k, v := range extra {
		base = append(base, k+"="+v)
	}
	return base
}

func (t *stdioTransport) writeJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	_, err = t.stdin.Write(append(b, '\n'))
	return err
}

func (t *stdioTransport) readLine() ([]byte, error) {
	line, err := t.stdout.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	return line, nil
}

func (t *stdioTransport) close() error {
	if t.stdin != nil {
		_ = t.stdin.Close()
	}
	if t.cmd != nil && t.cmd.Process != nil {
		killProcessTree(t.cmd.Process)
		_, _ = t.cmd.Process.Wait()
	}
	return nil
}

func (t *stdioTransport) drainStderr() {
	for t.stderr != nil && t.stderr.Scan() {
		// 保持 stderr 被消费，避免 MCP server 因日志阻塞；后续可接入日志系统展示。
	}
}

func setProcessGroup(cmd *exec.Cmd) {
	if runtime.GOOS != "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
}

func killProcessTree(p *os.Process) {
	if p == nil {
		return
	}
	if runtime.GOOS != "windows" {
		_ = syscall.Kill(-p.Pid, syscall.SIGKILL)
		return
	}
	_ = p.Kill()
}
