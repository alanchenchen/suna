//go:build !windows

package builtin

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/tools"
)

func TestExecRegistryBoundsReaperAndClose(t *testing.T) {
	registry := newExecRegistry()
	for index := 0; index < execMaxSessionActive; index++ {
		if reason := registry.reserve("s1"); reason != "" {
			t.Fatalf("第 %d 个 session 配额申请失败：%s", index+1, reason)
		}
	}
	if reason := registry.reserve("s1"); !strings.Contains(reason, "session active job limit") {
		t.Fatalf("未执行 session 配额：%q", reason)
	}
	for index := execMaxSessionActive; index < execMaxGlobalActive; index++ {
		if reason := registry.reserve("s" + string(rune('a'+index))); reason != "" {
			t.Fatalf("第 %d 个全局配额申请失败：%s", index+1, reason)
		}
	}
	if reason := registry.reserve("overflow"); !strings.Contains(reason, "global active job limit") {
		t.Fatalf("未执行全局配额：%q", reason)
	}

	completed := &execJob{id: "completed", finished: time.Now().Add(-execCompletedRetention), done: make(chan struct{}), stop: make(chan struct{})}
	registry.mu.Lock()
	registry.jobs[completed.id] = completed
	registry.mu.Unlock()
	registry.reapCompleted(time.Now())
	registry.mu.RLock()
	_, exists := registry.jobs[completed.id]
	registry.mu.RUnlock()
	if exists {
		t.Fatal("reaper 未删除过期 completed job")
	}

	registry.close()
	registry.close()
	if reason := registry.reserve("after-close"); reason != "exec registry is closed" {
		t.Fatalf("关闭门闩未生效：%q", reason)
	}
}

func TestExecStopPartialIsError(t *testing.T) {
	registry := newExecRegistry()
	defer registry.close()
	ctx, cancel := context.WithCancel(execOwner("session", "run", "main", t.TempDir()))
	cancel()
	job := &execJob{
		id: "partial", owner: tools.ExecutionContext{SessionID: "session", RunID: "run", BoundaryID: "main"},
		scope: execScopeRun, status: execStatusRunning, started: time.Now(), done: make(chan struct{}), stop: make(chan struct{}),
	}
	registry.mu.Lock()
	registry.jobs[job.id] = job
	registry.mu.Unlock()
	result := registry.stop(ctx, map[string]any{"job_id": job.id})
	state := execState(t, result)
	if !result.IsError || state["exec_status"] != execStatusRunning || state["cleanup_status"] != "partial" || !strings.Contains(result.Error, "partial") {
		t.Fatalf("partial stop 结果 = %#v", result)
	}
	registry.remove(job.id)
}

func TestProviderCloseIsIdempotentAndClosesRegistry(t *testing.T) {
	provider := NewProvider()
	if err := provider.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := provider.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	result := provider.tools[1].Execute(execOwner("session", "run", "main", t.TempDir()), map[string]any{
		"command": "sleep 30", "shell": "bash", "background": true,
	})
	if !result.IsError || execState(t, result)["exec_status"] != execStatusStartFailed {
		t.Fatalf("关闭后仍可启动：%#v", result)
	}
}

func TestExecBackgroundRunStatusStop(t *testing.T) {
	registry := newExecRegistry()
	defer registry.close()
	execTool := Exec{registry: registry}
	ctx := execOwner("session", "run", "worker", t.TempDir())
	run := execTool.Execute(ctx, map[string]any{
		"command": "echo begin; sleep 30", "shell": "bash", "background": true,
	})
	state := execState(t, run)
	jobID := state["job_id"]
	if run.IsError || state["exec_status"] != execStatusRunning || jobID == "" || !strings.Contains(run.Content, "started background job") {
		t.Fatalf("后台启动结果 = %#v", run)
	}

	deadline := time.Now().Add(3 * time.Second)
	var status tools.Result
	for {
		status = execTool.Execute(ctx, map[string]any{"action": "status", "job_id": jobID, "cursor": float64(0)})
		statusState := execState(t, status)
		if status.IsError || statusState["job_id"] != jobID {
			t.Fatalf("状态结果 = %#v", status)
		}
		if strings.Contains(status.Content, "begin") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待后台输出超时：%#v", status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	statusState := execState(t, status)
	cursor, err := strconv.ParseInt(statusState["next_cursor"], 10, 64)
	if err != nil {
		t.Fatalf("next_cursor 无效：%q", statusState["next_cursor"])
	}
	increment := execTool.Execute(ctx, map[string]any{"action": "status", "job_id": jobID, "cursor": float64(cursor)})
	if increment.IsError || execOutput(increment) != "" {
		t.Fatalf("增量状态结果 = %#v", increment)
	}

	stopped := execTool.Execute(ctx, map[string]any{"action": "stop", "job_id": jobID})
	if stopped.IsError || execState(t, stopped)["exec_status"] != execStatusStopped {
		t.Fatalf("停止结果 = %#v", stopped)
	}
	again := execTool.Execute(ctx, map[string]any{"action": "stop", "job_id": jobID})
	if again.IsError || execState(t, again)["exec_status"] != execStatusStopped {
		t.Fatalf("幂等停止结果 = %#v", again)
	}
}

func TestExecRunCleanupAndSessionOwnership(t *testing.T) {
	provider := NewProvider()
	defer provider.Close(context.Background())
	execTool := provider.tools[1]
	worker := execOwner("s1", "r1", "worker", t.TempDir())
	run := execTool.Execute(worker, map[string]any{"command": "sleep 30", "shell": "bash", "background": true})
	jobID := execState(t, run)["job_id"]
	if jobID == "" {
		t.Fatalf("未创建 run job：%#v", run)
	}
	otherBoundary := execOwner("s1", "r1", "other", t.TempDir())
	denied := execTool.Execute(otherBoundary, map[string]any{"action": "status", "job_id": jobID})
	if !denied.IsError || execState(t, denied)["exec_status"] != execStatusAccessDenied {
		t.Fatalf("边界越权未拒绝：%#v", denied)
	}
	if err := provider.CleanupRun(context.Background(), tools.ExecutionContext{SessionID: "s1", RunID: "r1"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := provider.execRegistry.jobs[jobID]; ok {
		t.Fatal("CleanupRun 未删除任务")
	}

	main := execOwner("s1", "r2", "main", t.TempDir())
	sessionRun := execTool.Execute(main, map[string]any{
		"command": "sleep 30", "shell": "bash", "background": true, "scope": "session",
	})
	sessionID := execState(t, sessionRun)["job_id"]
	if sessionID == "" {
		t.Fatalf("未创建 session job：%#v", sessionRun)
	}
	wrongSession := execOwner("s2", "r2", "main", t.TempDir())
	denied = execTool.Execute(wrongSession, map[string]any{"action": "stop", "job_id": sessionID})
	if !denied.IsError {
		t.Fatalf("session 越权未拒绝：%#v", denied)
	}
	workerSession := execTool.Execute(worker, map[string]any{
		"command": "sleep 1", "shell": "bash", "background": true, "scope": "session",
	})
	if !workerSession.IsError {
		t.Fatalf("子边界创建 session job 未拒绝：%#v", workerSession)
	}
	if err := provider.CleanupSession(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
}

func TestExecFastBackgroundExitKeepsManagedJob(t *testing.T) {
	registry := newExecRegistry()
	defer registry.close()
	execTool := Exec{registry: registry}
	ctx := execOwner("session", "run", "main", t.TempDir())
	run := execTool.Execute(ctx, map[string]any{
		"command": "printf quick", "shell": "bash", "background": true,
	})
	state := execState(t, run)
	jobID := state["job_id"]
	if run.IsError || state["exec_status"] != execStatusRunning || jobID == "" {
		t.Fatalf("快速退出启动结果 = %#v", run)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		status := execTool.Execute(ctx, map[string]any{"action": "status", "job_id": jobID})
		statusState := execState(t, status)
		if statusState["exec_status"] == execStatusExited {
			if status.IsError || !strings.Contains(status.Content, "quick") {
				t.Fatalf("快速退出终态结果 = %#v", status)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待快速退出终态超时：%#v", status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestExecJobSnapshotStateIsInternallyConsistent(t *testing.T) {
	job := &execJob{status: execStatusRunning, started: time.Now()}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for index := 1; index <= 10000; index++ {
			job.mu.Lock()
			job.status = execStatusRunning
			job.exitCode = 0
			job.finished = time.Time{}
			job.cleanupStatus = ""
			job.mu.Unlock()

			job.mu.Lock()
			job.status = execStatusExited
			job.exitCode = index
			job.finished = time.Now()
			job.waitComplete = true
			job.outputComplete = true
			job.cleanupStatus = "complete"
			job.mu.Unlock()
		}
	}()
	for {
		snapshot := job.snapshot()
		if snapshot.status == execStatusRunning && (snapshot.exitCode != 0 || !snapshot.finished.IsZero() || snapshot.cleanupStatus != "") {
			t.Fatalf("观察到混合 running 快照：%+v", snapshot)
		}
		if snapshot.status == execStatusExited && (snapshot.exitCode == 0 || snapshot.finished.IsZero() || snapshot.cleanupStatus != "complete") {
			t.Fatalf("观察到混合 exited 快照：%+v", snapshot)
		}
		select {
		case <-done:
			return
		default:
		}
	}
}

func TestExecRegistryCompletedHardLimitsKeepRunning(t *testing.T) {
	registry := newExecRegistry()
	defer registry.close()
	started := time.Now().Add(-time.Hour)
	for index := 0; index < 160; index++ {
		done := make(chan struct{})
		close(done)
		job := &execJob{
			id: fmt.Sprintf("completed-%03d", index),
			owner: tools.ExecutionContext{
				SessionID: fmt.Sprintf("session-%d", index%5),
			},
			status: execStatusExited, started: started, finished: started.Add(time.Duration(index) * time.Second),
			waitComplete: true, outputComplete: true, cleanupStatus: "complete", done: done, stop: make(chan struct{}),
		}
		registry.jobs[job.id] = job
	}
	for index := 0; index < 3; index++ {
		job := &execJob{
			id: fmt.Sprintf("running-%d", index), owner: tools.ExecutionContext{SessionID: "session-0"},
			status: execStatusRunning, started: time.Now(), done: make(chan struct{}), stop: make(chan struct{}),
		}
		registry.jobs[job.id] = job
	}

	registry.trimCompleted()
	pointers := registry.jobPointers()
	completed := 0
	bySession := make(map[string]int)
	running := 0
	for _, job := range pointers {
		snapshot := job.snapshot()
		if snapshot.status == execStatusRunning {
			running++
			continue
		}
		completed++
		bySession[job.owner.SessionID]++
	}
	if running != 3 {
		t.Fatalf("running 任务被驱逐：剩余 %d", running)
	}
	if completed != execMaxGlobalCompleted {
		t.Fatalf("全局 completed 数量 = %d，期望 %d", completed, execMaxGlobalCompleted)
	}
	for sessionID, count := range bySession {
		if count > execMaxSessionCompleted {
			t.Fatalf("%s retained completed = %d，超过 %d", sessionID, count, execMaxSessionCompleted)
		}
	}
	// 人工 running 任务没有 monitor，断言后转成可清理终态。
	for _, job := range pointers {
		job.mu.Lock()
		if job.status == execStatusRunning {
			job.status = execStatusStopped
			job.finished = time.Now()
			job.waitComplete = true
			job.outputComplete = true
			job.cleanupStatus = "complete"
			close(job.done)
		}
		job.mu.Unlock()
	}
}

func TestExecTerminalPartialStopAndStatus(t *testing.T) {
	registry := newExecRegistry()
	defer registry.close()
	ctx := execOwner("session", "run", "main", t.TempDir())
	job := &execJob{
		id: "terminal-partial", owner: tools.ExecutionContext{SessionID: "session", RunID: "run", BoundaryID: "main"},
		scope: execScopeRun, status: execStatusStopped, exitCode: -1, started: time.Now().Add(-time.Second), finished: time.Now(),
		waitComplete: false, outputComplete: true, cleanupStatus: "partial", output: newCursorRing(maxExecOutput),
		done: make(chan struct{}), stop: make(chan struct{}),
	}
	close(job.done)
	registry.jobs[job.id] = job

	stopped := registry.stop(ctx, map[string]any{"job_id": job.id})
	if state := execState(t, stopped); !stopped.IsError || state["cleanup_status"] != "partial" || !strings.Contains(stopped.Error, "partial") {
		t.Fatalf("终态 partial stop = %#v", stopped)
	}
	status := registry.status(ctx, map[string]any{"job_id": job.id})
	if state := execState(t, status); status.IsError || state["cleanup_status"] != "partial" || !strings.Contains(status.Content, "Cleanup: partial") {
		t.Fatalf("终态 partial status = %#v", status)
	}
}

func TestExecLifecycleCleanupWaitsThenRemovesAndReturnsPartial(t *testing.T) {
	registry := newExecRegistry()
	defer registry.close()
	job := &execJob{
		id: "cleanup-partial", owner: tools.ExecutionContext{SessionID: "session", RunID: "run"}, scope: execScopeRun,
		status: execStatusRunning, started: time.Now(), done: make(chan struct{}), stop: make(chan struct{}),
	}
	registry.jobs[job.id] = job

	result := make(chan error, 1)
	go func() {
		result <- registry.cleanup(context.Background(), "run", func(candidate *execJob) bool { return candidate == job })
	}()
	select {
	case <-job.stop:
	case <-time.After(time.Second):
		t.Fatal("cleanup 未请求停止")
	}
	registry.mu.RLock()
	_, retainedWhileWaiting := registry.jobs[job.id]
	registry.mu.RUnlock()
	if !retainedWhileWaiting {
		t.Fatal("cleanup 在等待终态前删除了任务")
	}
	job.mu.Lock()
	job.status = execStatusStopped
	job.exitCode = -1
	job.finished = time.Now()
	job.waitComplete = false
	job.outputComplete = true
	job.cleanupStatus = "partial"
	job.mu.Unlock()
	close(job.done)

	if err := <-result; err == nil || !strings.Contains(err.Error(), "partial") {
		t.Fatalf("cleanup partial error = %v", err)
	}
	registry.mu.RLock()
	_, exists := registry.jobs[job.id]
	registry.mu.RUnlock()
	if exists {
		t.Fatal("生命周期 cleanup 后 partial 任务仍可访问")
	}
}
