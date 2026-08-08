package builtin

import (
	"sync"
	"time"

	"github.com/alanchenchen/suna/internal/logging"
	"github.com/alanchenchen/suna/internal/tools"
)

// execJob 的状态只在锁内修改，输出则由独立并发安全环形缓冲管理。
type execJob struct {
	mu             sync.Mutex
	id             string
	owner          tools.ExecutionContext
	scope          string
	run            *managedProcess
	output         *cursorRing
	registry       *execRegistry
	status         string
	exitCode       int
	started        time.Time
	finished       time.Time
	timeout        time.Duration
	waitComplete   bool
	outputComplete bool
	cleanupStatus  string
	done           chan struct{}
	stop           chan struct{}
	stopStatus     string
	stopReason     string
	stopOnce       sync.Once
}

type execJobSnapshot struct {
	status         string
	exitCode       int
	started        time.Time
	finished       time.Time
	timeout        time.Duration
	waitComplete   bool
	outputComplete bool
	cleanupStatus  string
}

// snapshot 一次持锁复制全部状态字段，避免观察到 running 与终态 exit_code 的混合快照。
func (j *execJob) snapshot() execJobSnapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	return execJobSnapshot{
		status:         j.status,
		exitCode:       j.exitCode,
		started:        j.started,
		finished:       j.finished,
		timeout:        j.timeout,
		waitComplete:   j.waitComplete,
		outputComplete: j.outputComplete,
		cleanupStatus:  j.cleanupStatus,
	}
}

func (s execJobSnapshot) fields() map[string]any {
	now := time.Now()
	if !s.finished.IsZero() {
		now = s.finished
	}
	fields := map[string]any{
		"started_at":  s.started.UTC().Format(time.RFC3339Nano),
		"duration_ms": now.Sub(s.started).Milliseconds(),
	}
	if s.timeout > 0 {
		fields["timeout_seconds"] = int64(s.timeout / time.Second)
	}
	if s.status != execStatusRunning {
		fields["exit_code"] = s.exitCode
		fields["wait_complete"] = s.waitComplete
		fields["output_complete"] = s.outputComplete
	}
	if !s.finished.IsZero() {
		fields["finished_at"] = s.finished.UTC().Format(time.RFC3339Nano)
	}
	if s.cleanupStatus != "" {
		fields["cleanup_status"] = s.cleanupStatus
	}
	return fields
}

func (j *execJob) monitor() {
	status := execStatusExited
	terminationReason := "exit"
	waitComplete := true
	var waitErr error
	var timer <-chan time.Time
	var timeoutTimer *time.Timer
	if j.timeout > 0 {
		timeoutTimer = time.NewTimer(remainingExecTimeout(j.started, j.timeout))
		timer = timeoutTimer.C
		defer timeoutTimer.Stop()
	}
	select {
	case waitErr = <-j.run.wait:
		j.run.tree.terminate(execTerminateGrace)
	case <-j.stop:
		j.mu.Lock()
		status = j.stopStatus
		terminationReason = j.stopReason
		j.mu.Unlock()
		j.run.tree.terminate(execTerminateGrace)
		waitErr, waitComplete = waitProcessWithin(j.run.wait, execWaitLimit)
	case <-timer:
		status = execStatusTimedOut
		terminationReason = "timeout"
		j.run.tree.terminate(execTerminateGrace)
		waitErr, waitComplete = waitProcessWithin(j.run.wait, execWaitLimit)
	}
	outputComplete := j.run.finishOutput(execOutputDrainLimit)
	j.run.tree.close()
	finished := time.Now()
	cleanupStatus := execCleanupStatus(waitComplete, outputComplete)

	// 终态相关字段必须在同一次临界区发布；cleanupStatus 仅表示 Wait 与输出排空完成度。
	j.mu.Lock()
	j.status = status
	j.exitCode = processExitCode(waitErr)
	j.finished = finished
	j.waitComplete = waitComplete
	j.outputComplete = outputComplete
	j.cleanupStatus = cleanupStatus
	j.mu.Unlock()
	capturedBytes, outputTruncated := j.output.stats()
	if terminationReason == "" {
		if status == execStatusCancelled {
			terminationReason = "cancel"
		} else if status == execStatusStopped {
			terminationReason = "stop"
		} else {
			terminationReason = "exit"
		}
	}
	logging.Info("agent", "exec_job_terminal", logging.Event{
		"session_id": j.owner.SessionID, "run_id": j.owner.RunID, "boundary_id": j.owner.BoundaryID,
		"job_id": j.id, "scope": j.scope, "status": status, "exit_code": processExitCode(waitErr),
		"duration_ms": finished.Sub(j.started).Milliseconds(), "cleanup_status": cleanupStatus,
		"wait_complete": waitComplete, "output_complete": outputComplete, "output_truncated": outputTruncated,
		"captured_bytes": capturedBytes, "reason": terminationReason,
	})
	if j.registry != nil {
		j.registry.jobCompleted(j)
	}
	close(j.done)
}

// requestTerminate 对终态任务幂等，不再发送无意义的停止请求。
func (j *execJob) requestTerminate(status, reason string) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.status != execStatusRunning {
		return false
	}
	requested := false
	j.stopOnce.Do(func() {
		j.stopStatus = status
		j.stopReason = reason
		close(j.stop)
		requested = true
	})
	return requested
}

func (j *execJob) requestStop() bool { return j.requestTerminate(execStatusStopped, "stop") }
