//go:build windows && integration

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWindowsDaemonLifecycleSmoke(t *testing.T) {
	home := t.TempDir()
	exe := filepath.Join(t.TempDir(), "suna.exe")
	build := exec.Command("go", "build", "-o", exe, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build error = %v, output = %s", err, output)
	}

	env := windowsSmokeEnv(os.Environ(), home)
	run := func(args ...string) (string, string, error) {
		cmd := exec.Command(exe, args...)
		cmd.Env = env
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		return stdout.String(), stderr.String(), err
	}

	var daemonPID int
	// 即使断言提前失败，也尽量停止并回收隔离 daemon，避免 Windows CI 留下后台进程。
	t.Cleanup(func() {
		_, _, _ = run("stop")
		if daemonPID > 0 {
			if proc, err := os.FindProcess(daemonPID); err == nil {
				_ = proc.Kill()
			}
		}
	})

	type serveCall struct {
		stdout string
		stderr string
		err    error
		result serveResult
	}
	calls := make([]serveCall, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range calls {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			calls[i].stdout, calls[i].stderr, calls[i].err = run("serve", "--json")
			if calls[i].err == nil {
				calls[i].err = json.Unmarshal([]byte(strings.TrimSpace(calls[i].stdout)), &calls[i].result)
			}
		}()
	}
	close(start)
	wg.Wait()

	for i, call := range calls {
		if call.err != nil {
			t.Fatalf("serve call %d error = %v, stdout = %q, stderr = %q", i, call.err, call.stdout, call.stderr)
		}
		if call.result.Status != "ready" || call.result.PID <= 0 || call.result.TCPEndpoint == "" {
			t.Fatalf("serve call %d result = %#v, want ready daemon", i, call.result)
		}
	}
	daemonPID = calls[0].result.PID
	if calls[1].result.PID != daemonPID {
		t.Fatalf("serve PIDs = %d and %d, want one daemon", daemonPID, calls[1].result.PID)
	}

	stdout, stderr, err := run("status")
	if err != nil {
		t.Fatalf("status error = %v, stdout = %q, stderr = %q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "sunad is running") {
		t.Fatalf("status output = %q, want running", stdout)
	}

	stdout, stderr, err = run("stop")
	if err != nil {
		t.Fatalf("stop error = %v, stdout = %q, stderr = %q", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "sunad stopped") {
		t.Fatalf("stop output = %q, want stopped", stdout)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		stdout, _, _ = run("status")
		if strings.Contains(stdout, "sunad is not running") {
			daemonPID = 0
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("daemon remained reachable after stop, last status = %q", stdout)
}

func windowsSmokeEnv(base []string, home string) []string {
	overrides := map[string]bool{
		"HOME":              true,
		"USERPROFILE":       true,
		daemonEnvName:       true,
		tcpListenEnv:        true,
		tcpDefaultListenEnv: true,
	}
	env := make([]string, 0, len(base)+2)
	for _, item := range base {
		key, _, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		remove := false
		for name := range overrides {
			if strings.EqualFold(key, name) {
				remove = true
				break
			}
		}
		if !remove {
			env = append(env, item)
		}
	}
	return append(env, "HOME="+home, "USERPROFILE="+home)
}
