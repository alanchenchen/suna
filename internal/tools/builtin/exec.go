package builtin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/alanchenchen/suna/internal/tools"
	"github.com/google/uuid"
)

const (
	execTimeout               = 60 * time.Second
	execSessionTimeout        = time.Hour
	execTerminateGrace        = 200 * time.Millisecond
	execWaitLimit             = 2 * time.Second
	execOutputDrainLimit      = 500 * time.Millisecond
	maxExecOutput             = 50 * 1024
	execDefaultShell          = "bash"
	execScopeRun              = "run"
	execScopeSession          = "session"
	execStatusRunning         = "running"
	execStatusExited          = "exited"
	execStatusStopped         = "stopped"
	execStatusTimedOut        = "timed_out"
	execStatusCancelled       = "cancelled"
	execStatusStartFailed     = "start_failed"
	execStatusNotFound        = "not_found"
	execStatusAccessDenied    = "access_denied"
	execMaxSessionActive      = 8
	execMaxGlobalActive       = 32
	execMaxSessionCompleted   = 32
	execMaxGlobalCompleted    = 128
	execCompletedRetention    = 15 * time.Minute
	execReaperInterval        = 30 * time.Second
	execJobStopLimit          = execWaitLimit + execTerminateGrace + execOutputDrainLimit
	execLifecycleCleanupLimit = execJobStopLimit + time.Second
)

// Exec 保留工具名不变，并通过注册表管理后台进程。
type Exec struct {
	registry *execRegistry
}

var defaultExecRegistry struct {
	once     sync.Once
	registry *execRegistry
}

func (e Exec) registryOrDefault() *execRegistry {
	if e.registry != nil {
		return e.registry
	}
	// 零值 Exec 的前台调用不需要后台注册表；仅在首次状态化操作时启动回收协程。
	defaultExecRegistry.once.Do(func() {
		defaultExecRegistry.registry = newExecRegistry()
	})
	return defaultExecRegistry.registry
}

func (e Exec) Execute(ctx context.Context, params map[string]any) tools.Result {
	action, _ := params["action"].(string)
	if action == "" {
		action = "run"
	}
	switch action {
	case "run":
		return e.executeRun(ctx, params)
	case "status":
		return e.registryOrDefault().status(ctx, params)
	case "stop":
		return e.registryOrDefault().stop(ctx, params)
	default:
		return makeExecResult("run", execScopeRun, "", execStatusStartFailed, nil, true, "action must be run, status, or stop", false, nil)
	}
}

func (e Exec) executeRun(ctx context.Context, params map[string]any) tools.Result {
	command, _ := params["command"].(string)
	if command == "" {
		return makeExecResult("run", execScopeRun, "", execStatusStartFailed, nil, true, "command is required", false, nil)
	}
	background, _ := params["background"].(bool)
	scope, _ := params["scope"].(string)
	if scope == "" {
		scope = execScopeRun
	}
	if scope != execScopeRun && scope != execScopeSession {
		return makeExecResult("run", scope, "", execStatusStartFailed, nil, true, "scope must be run or session", false, nil)
	}
	if !background && scope != execScopeRun {
		return makeExecResult("run", scope, "", execStatusStartFailed, nil, true, "scope is only valid for background runs", false, nil)
	}

	owner, _ := tools.ExecutionContextFrom(ctx)
	if background {
		if owner.SessionID == "" {
			return makeExecResult("run", scope, "", execStatusAccessDenied, nil, true, "background exec requires SessionID", false, nil)
		}
		if scope == execScopeRun && owner.RunID == "" {
			return makeExecResult("run", scope, "", execStatusAccessDenied, nil, true, "run-scoped exec requires RunID", false, nil)
		}
		if scope == execScopeSession && !isMainBoundary(owner.BoundaryID) {
			return makeExecResult("run", scope, "", execStatusAccessDenied, nil, true, "session-scoped exec is only allowed in the main boundary", false, nil)
		}
	}

	cwdParam, _ := params["cwd"].(string)
	cwd, err := tools.EffectiveCWD(ctx, cwdParam)
	if err != nil {
		return makeExecResult("run", scope, "", execStatusStartFailed, nil, true, fmt.Sprintf("resolve working directory: %s", err), false, nil)
	}
	shell := "auto"
	if value, ok := params["shell"].(string); ok {
		shell = value
	}
	shellCmd, shellUsed := resolveShell(shell)
	if shellCmd == "" {
		return makeExecResult("run", scope, "", execStatusStartFailed, nil, true, "cannot determine shell, please specify shell parameter", false, nil)
	}
	env := os.Environ()
	if values, ok := params["env"].(map[string]any); ok {
		for key, value := range values {
			if text, ok := value.(string); ok {
				env = append(env, fmt.Sprintf("%s=%s", key, text))
			}
		}
	}

	timeout, hasTimeout, err := parseExecTimeout(params)
	if err != nil {
		return makeExecResult("run", scope, "", execStatusStartFailed, nil, true, err.Error(), false, nil)
	}
	if !hasTimeout {
		if !background {
			timeout = execTimeout
		} else if scope == execScopeSession {
			timeout = execSessionTimeout
		}
	}
	cmd := shellCommand(shellCmd, shellUsed, command)
	cmd.Dir, cmd.Env = cwd, env
	if background {
		return e.startBackground(ctx, cmd, owner, scope, timeout)
	}
	return runForeground(ctx, cmd, timeout)
}

func parseExecTimeout(params map[string]any) (time.Duration, bool, error) {
	value, exists := params["timeout"]
	if !exists {
		return 0, false, nil
	}
	seconds, ok := value.(float64)
	if !ok || seconds <= 0 || seconds != float64(int64(seconds)) {
		return 0, true, fmt.Errorf("timeout must be a positive integer")
	}
	return time.Duration(int64(seconds)) * time.Second, true, nil
}

func shellCommand(shellCmd, shellUsed, command string) *exec.Cmd {
	if shellUsed == "cmd" {
		return exec.Command(shellCmd, "/c", command)
	}
	return exec.Command(shellCmd, "-c", command)
}

func runForeground(ctx context.Context, cmd *exec.Cmd, timeout time.Duration) tools.Result {
	started := time.Now()
	if err := ctx.Err(); err != nil {
		extra := map[string]any{"cleanup_status": "complete"}
		return makeExecResult("run", execScopeRun, "", execStatusCancelled, nil, true, "command canceled", false, extra)
	}
	stdout := newHeadTailBuffer(maxExecOutput)
	stderr := newHeadTailBuffer(maxExecOutput)
	run, err := startManagedProcess(cmd, stdout, stderr)
	if err != nil {
		return makeExecResult("run", execScopeRun, "", execStatusStartFailed, nil, true, fmt.Sprintf("exec start: %s", err), false, nil)
	}
	status := execStatusExited
	waitComplete := true
	var waitErr error
	timeoutTimer := time.NewTimer(remainingExecTimeout(started, timeout))
	defer timeoutTimer.Stop()
	select {
	case waitErr = <-run.wait:
		// 根进程正常退出后仍需清理继承管道或留在进程组中的后代。
		run.tree.terminate(execTerminateGrace)
	case <-ctx.Done():
		status = execStatusCancelled
		run.tree.terminate(execTerminateGrace)
		waitErr, waitComplete = waitProcessWithin(run.wait, execWaitLimit)
	case <-timeoutTimer.C:
		status = execStatusTimedOut
		run.tree.terminate(execTerminateGrace)
		waitErr, waitComplete = waitProcessWithin(run.wait, execWaitLimit)
	}
	outputComplete := run.finishOutput(execOutputDrainLimit)
	run.tree.close()
	exitCode := processExitCode(waitErr)
	stdoutText, stdoutTruncated := stdout.String()
	stderrText, stderrTruncated := stderr.String()
	text, truncated := formatStreams(stdoutText, stdoutTruncated, stderrText, stderrTruncated)
	isError := status != execStatusExited || exitCode != 0
	errorText := ""
	if status == execStatusTimedOut {
		errorText = fmt.Sprintf("command timed out after %s", timeout)
	} else if status == execStatusCancelled {
		errorText = "command canceled"
	} else if exitCode != 0 {
		errorText = fmt.Sprintf("command exited with code %d", exitCode)
	} else if waitErr != nil {
		isError, errorText = true, fmt.Sprintf("exec wait: %s", waitErr)
	}
	extra := map[string]any{
		"exit_code":       exitCode,
		"duration_ms":     time.Since(started).Milliseconds(),
		"timeout_seconds": int64(timeout / time.Second),
	}
	if status == execStatusTimedOut || status == execStatusCancelled {
		extra["cleanup_status"] = foregroundCleanupStatus(waitComplete, outputComplete)
	}
	return makeExecResult("run", execScopeRun, "", status, text, isError, errorText, truncated, extra)
}

func (e Exec) startBackground(ctx context.Context, cmd *exec.Cmd, owner tools.ExecutionContext, scope string, timeout time.Duration) tools.Result {
	started := time.Now()
	registry := e.registryOrDefault()
	if reason := registry.reserve(owner.SessionID); reason != "" {
		return makeExecResult("run", scope, "", execStatusStartFailed, nil, true, reason, false, nil)
	}
	output := newCursorRing(maxExecOutput)
	stdout := &streamWriter{label: "[stdout]\n", output: output}
	stderr := &streamWriter{label: "[stderr]\n", output: output}
	run, err := startManagedProcess(cmd, stdout, stderr)
	if err != nil {
		registry.release(owner.SessionID)
		return makeExecResult("run", scope, "", execStatusStartFailed, nil, true, fmt.Sprintf("exec start: %s", err), false, nil)
	}
	job := &execJob{
		id: uuid.NewString(), owner: owner, scope: scope, run: run, output: output, registry: registry,
		status: execStatusRunning, started: started, done: make(chan struct{}), stop: make(chan struct{}), timeout: timeout,
	}
	if !registry.add(job) {
		go job.monitor()
		job.requestTerminate(execStatusStopped, "provider_close")
		<-job.done
		snapshot := job.snapshot()
		return makeExecResult("run", scope, "", execStatusStartFailed, nil, true, "exec registry is closed", false, snapshot.fields())
	}
	go job.monitor()
	// 注册成功后不再探测快速退出；终态和输出始终由后续 status 读取。
	// 在发布 job_id 前检查调用上下文，使已发生的取消稳定走清理路径。
	if ctx.Err() != nil {
		return cancelBackgroundStart(job, registry, scope, output)
	}
	extra := map[string]any{
		"started_at":  started.UTC().Format(time.RFC3339Nano),
		"duration_ms": time.Since(started).Milliseconds(),
		"next_cursor": int64(0),
	}
	if timeout > 0 {
		extra["timeout_seconds"] = int64(timeout / time.Second)
	}
	return makeExecResult("run", scope, job.id, execStatusRunning, nil, false, "", false, extra)
}

func cancelBackgroundStart(job *execJob, registry *execRegistry, scope string, output *cursorRing) tools.Result {
	job.requestTerminate(execStatusCancelled, "cancel")
	_ = waitSignalWithin(job.done, execJobStopLimit)
	registry.remove(job.id)
	data, next, truncated := output.snapshot(0)
	snapshot := job.snapshot()
	extra := snapshot.fields()
	extra["next_cursor"] = next
	cleanupStatus := snapshot.cleanupStatus
	if snapshot.status == execStatusRunning || cleanupStatus == "" {
		cleanupStatus = "partial"
	}
	extra["cleanup_status"] = cleanupStatus
	return makeExecResult("run", scope, "", execStatusCancelled, data, true, "background start cancelled", truncated, extra)
}
