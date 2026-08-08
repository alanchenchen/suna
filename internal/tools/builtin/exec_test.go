//go:build !windows

package builtin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/tools"
)

func execState(t *testing.T, result tools.Result) map[string]string {
	t.Helper()
	state := make(map[string]string, len(result.Metadata))
	for key, value := range result.Metadata {
		state[key] = fmt.Sprint(value)
	}
	for _, key := range []string{"action", "exec_status", "output_truncated"} {
		if _, ok := state[key]; !ok {
			t.Fatalf("Metadata 缺少 %s：%#v", key, result.Metadata)
		}
	}
	return state
}

func execOutput(result tools.Result) string {
	stdout := strings.Index(result.Content, "[stdout]\n")
	stderr := strings.Index(result.Content, "[stderr]\n")
	start := stdout
	if start < 0 || stderr >= 0 && stderr < start {
		start = stderr
	}
	if start < 0 {
		return ""
	}
	return result.Content[start:]
}

func execOwner(session, run, boundary, cwd string) context.Context {
	return tools.WithExecutionContext(context.Background(), tools.ExecutionContext{
		SessionID: session, RunID: run, BoundaryID: boundary, CWD: cwd,
	})
}

func TestExecUsesExecutionContextCWD(t *testing.T) {
	root := t.TempDir()
	ctx := execOwner("session", "run", "main", root)
	result := Exec{}.Execute(ctx, map[string]any{"command": "pwd", "shell": "bash"})
	if result.IsError {
		t.Fatalf("执行失败：%s", result.Error)
	}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	if got := strings.TrimSpace(strings.TrimPrefix(execOutput(result), "[stdout]\n")); got != root {
		t.Fatalf("工作目录 = %q，期望 %q", got, root)
	}
}

func TestExecResolvesRelativeCWDFromExecutionContext(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0755); err != nil {
		t.Fatal(err)
	}
	result := Exec{}.Execute(execOwner("session", "run", "main", root), map[string]any{
		"command": "pwd", "cwd": "child", "shell": "bash",
	})
	if result.IsError {
		t.Fatalf("执行失败：%s", result.Error)
	}
	if real, err := filepath.EvalSymlinks(child); err == nil {
		child = real
	}
	if got := strings.TrimSpace(strings.TrimPrefix(execOutput(result), "[stdout]\n")); got != child {
		t.Fatalf("工作目录 = %q，期望 %q", got, child)
	}
}

func TestExecLimitsLargeStdoutAndKeepsTail(t *testing.T) {
	result := Exec{}.Execute(context.Background(), map[string]any{
		"command": "printf HEAD; yes x | head -c 200000; printf TAIL", "timeout": float64(5), "shell": "bash",
	})
	if result.IsError {
		t.Fatalf("执行失败：%s", result.Error)
	}
	if !result.Truncated || len(result.Content) > maxExecOutput*2+500 {
		t.Fatalf("有界输出不符合预期：长度=%d，截断=%v", len(result.Content), result.Truncated)
	}
	if !strings.Contains(result.Content, "HEAD") || !strings.Contains(result.Content, "TAIL") || !strings.Contains(result.Content, "truncated") {
		t.Fatalf("输出未保留头尾：%q", result.Content)
	}
}

func TestExecTimeoutAndCancelKillPipeDescendants(t *testing.T) {
	for _, test := range []struct {
		name   string
		ctx    func() context.Context
		params map[string]any
		status string
	}{
		{name: "超时", ctx: func() context.Context { return context.Background() }, params: map[string]any{"timeout": float64(1)}, status: execStatusTimedOut},
		{name: "取消", ctx: func() context.Context {
			ctx, cancel := context.WithCancel(context.Background())
			go func() { time.Sleep(100 * time.Millisecond); cancel() }()
			return ctx
		}, params: map[string]any{}, status: execStatusCancelled},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.params["command"] = "(sleep 30) & wait"
			test.params["shell"] = "bash"
			started := time.Now()
			result := Exec{}.Execute(test.ctx(), test.params)
			state := execState(t, result)
			if !result.IsError || state["exec_status"] != test.status || state["cleanup_status"] != "complete" {
				t.Fatalf("结果 = %#v", result)
			}
			if elapsed := time.Since(started); elapsed > 4*time.Second {
				t.Fatalf("返回耗时过长：%s", elapsed)
			}
		})
	}
}

func TestRemainingExecTimeoutIncludesStartupElapsed(t *testing.T) {
	started := time.Now().Add(-750 * time.Millisecond)
	remaining := remainingExecTimeout(started, time.Second)
	if remaining <= 0 || remaining > 300*time.Millisecond {
		t.Fatalf("剩余 timeout = %s，期望约 250ms", remaining)
	}
	if got := remainingExecTimeout(time.Now().Add(-2*time.Second), time.Second); got != time.Nanosecond {
		t.Fatalf("已耗尽 timeout = %s，期望最小正时长", got)
	}
}

func TestForegroundCleanupStatusCompleteAndPartial(t *testing.T) {
	exited := Exec{}.Execute(context.Background(), map[string]any{"command": "printf ok", "shell": "bash"})
	if state := execState(t, exited); exited.IsError || state["exec_status"] != execStatusExited {
		t.Fatalf("正常退出结果错误：%#v", exited)
	} else if _, ok := state["cleanup_status"]; ok {
		t.Fatalf("正常退出不应包含 cleanup_status：%#v", exited)
	}

	if got := foregroundCleanupStatus(true, true); got != "complete" {
		t.Fatalf("Wait 与输出均完成时 cleanup_status = %q", got)
	}
	for _, completed := range [][2]bool{{false, true}, {true, false}, {false, false}} {
		if got := foregroundCleanupStatus(completed[0], completed[1]); got != "partial" {
			t.Fatalf("完成状态 %v 的 cleanup_status = %q", completed, got)
		}
	}

	// 未关闭的 drainDone 可稳定覆盖输出排空超限路径，无需平台进程 hook。
	run := &managedProcess{drainDone: make(chan struct{})}
	if run.finishOutput(time.Millisecond) {
		t.Fatal("输出未排空却报告 complete")
	}
}

func TestExecResultContractAndFastBackgroundFailure(t *testing.T) {
	registry := newExecRegistry()
	defer registry.close()
	execTool := Exec{registry: registry}
	ctx := execOwner("session", "run", "main", t.TempDir())
	run := execTool.Execute(ctx, map[string]any{
		"command": "printf failed >&2; exit 7", "shell": "bash", "background": true,
	})
	runState := execState(t, run)
	jobID := runState["job_id"]
	if run.IsError || runState["exec_status"] != execStatusRunning || jobID == "" {
		t.Fatalf("快速后台启动结果 = %#v", run)
	}
	if !strings.Contains(run.Content, "started background job") || !strings.Contains(run.Content, jobID) || !strings.Contains(run.Content, "action status") {
		t.Fatalf("后台启动 Content 缺少自然语义：%q", run.Content)
	}

	deadline := time.Now().Add(3 * time.Second)
	var status tools.Result
	for {
		status = execTool.Execute(ctx, map[string]any{"action": "status", "job_id": jobID})
		state := execState(t, status)
		if state["exec_status"] != execStatusRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待快速后台失败终态超时：%#v", status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	state := execState(t, status)
	if status.IsError || state["exec_status"] != execStatusExited || state["exit_code"] != "7" {
		t.Fatalf("快速后台失败状态结果 = %#v", status)
	}
	for _, key := range []string{"action", "exec_status", "output_truncated", "duration_ms", "exit_code", "scope", "next_cursor", "job_id"} {
		if _, ok := state[key]; !ok {
			t.Fatalf("Metadata 缺少 %s：%v", key, state)
		}
	}
	if !strings.Contains(status.Content, "finished with status exited") || !strings.Contains(status.Content, "Exit code: 7") || !strings.Contains(status.Content, "failed") {
		t.Fatalf("终态 Content 缺少自然语义或输出：%q", status.Content)
	}
}

func TestExecCancelledContextDoesNotPublishBackgroundJob(t *testing.T) {
	registry := newExecRegistry()
	defer registry.close()
	execTool := Exec{registry: registry}
	ctx, cancel := context.WithCancel(execOwner("session", "run", "main", t.TempDir()))
	cancel()
	result := execTool.Execute(ctx, map[string]any{
		"command": "sleep 30", "shell": "bash", "background": true,
	})
	state := execState(t, result)
	if !result.IsError || state["exec_status"] != execStatusCancelled || state["cleanup_status"] != "complete" {
		t.Fatalf("已取消启动结果 = %#v", result)
	}
	if _, ok := result.Metadata["job_id"]; ok {
		t.Fatalf("已取消启动发布了 job_id：%#v", result.Metadata)
	}
	registry.mu.RLock()
	jobs, active := len(registry.jobs), registry.active
	registry.mu.RUnlock()
	if jobs != 0 || active != 0 {
		t.Fatalf("取消后仍有资源：jobs=%d active=%d", jobs, active)
	}
}
