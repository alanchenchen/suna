package builtin

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/alanchenchen/suna/internal/tools"
)

// execRegistry 统一管理后台任务的配额、归属、回收与关闭。
type execRegistry struct {
	mu            sync.RWMutex
	trimMu        sync.Mutex
	jobs          map[string]*execJob
	active        int
	sessionActive map[string]int
	closed        bool
	reaperStop    chan struct{}
	reaperDone    chan struct{}
	closeOnce     sync.Once
	closeErr      error
}

func newExecRegistry() *execRegistry {
	r := &execRegistry{
		jobs:          make(map[string]*execJob),
		sessionActive: make(map[string]int),
		reaperStop:    make(chan struct{}),
		reaperDone:    make(chan struct{}),
	}
	go r.reapLoop()
	return r
}

// reserve 在启动进程前集中占用 active 配额，避免并发启动越界。
func (r *execRegistry) reserve(sessionID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return "exec registry is closed"
	}
	if r.active >= execMaxGlobalActive {
		return fmt.Sprintf("global active job limit reached (%d)", execMaxGlobalActive)
	}
	if r.sessionActive[sessionID] >= execMaxSessionActive {
		return fmt.Sprintf("session active job limit reached (%d)", execMaxSessionActive)
	}
	r.active++
	r.sessionActive[sessionID]++
	return ""
}

func (r *execRegistry) releaseLocked(sessionID string) {
	if r.active > 0 {
		r.active--
	}
	if count := r.sessionActive[sessionID]; count <= 1 {
		delete(r.sessionActive, sessionID)
	} else {
		r.sessionActive[sessionID] = count - 1
	}
}

func (r *execRegistry) release(sessionID string) {
	r.mu.Lock()
	r.releaseLocked(sessionID)
	r.mu.Unlock()
}

func (r *execRegistry) add(job *execJob) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false
	}
	r.jobs[job.id] = job
	return true
}

func (r *execRegistry) remove(id string) {
	r.mu.Lock()
	delete(r.jobs, id)
	r.mu.Unlock()
}

func (r *execRegistry) jobPointers() []*execJob {
	r.mu.RLock()
	jobs := make([]*execJob, 0, len(r.jobs))
	for _, job := range r.jobs {
		jobs = append(jobs, job)
	}
	r.mu.RUnlock()
	return jobs
}

type completedJob struct {
	id        string
	sessionID string
	finished  time.Time
	job       *execJob
}

func completedLess(a, b completedJob) bool {
	if a.finished.Equal(b.finished) {
		return a.id < b.id
	}
	return a.finished.Before(b.finished)
}

// jobCompleted 在 job 锁外更新配额并串行执行终态硬上限回收，避免锁顺序反转。
func (r *execRegistry) jobCompleted(job *execJob) {
	r.mu.Lock()
	r.releaseLocked(job.owner.SessionID)
	r.mu.Unlock()
	r.trimCompleted()
}

func (r *execRegistry) trimCompleted() {
	r.trimMu.Lock()
	defer r.trimMu.Unlock()

	pointers := r.jobPointers()
	completed := make([]completedJob, 0, len(pointers))
	for _, job := range pointers {
		snapshot := job.snapshot()
		if snapshot.finished.IsZero() {
			continue
		}
		completed = append(completed, completedJob{
			id: job.id, sessionID: job.owner.SessionID, finished: snapshot.finished, job: job,
		})
	}

	remove := make(map[string]*execJob)
	bySession := make(map[string][]completedJob)
	for _, item := range completed {
		bySession[item.sessionID] = append(bySession[item.sessionID], item)
	}
	for _, items := range bySession {
		sort.Slice(items, func(i, k int) bool { return completedLess(items[i], items[k]) })
		for _, item := range items[:max(0, len(items)-execMaxSessionCompleted)] {
			remove[item.id] = item.job
		}
	}

	remaining := make([]completedJob, 0, len(completed)-len(remove))
	for _, item := range completed {
		if remove[item.id] == nil {
			remaining = append(remaining, item)
		}
	}
	if len(remaining) > execMaxGlobalCompleted {
		sort.Slice(remaining, func(i, k int) bool { return completedLess(remaining[i], remaining[k]) })
		for _, item := range remaining[:len(remaining)-execMaxGlobalCompleted] {
			remove[item.id] = item.job
		}
	}

	r.mu.Lock()
	for id, job := range remove {
		if r.jobs[id] == job {
			delete(r.jobs, id)
		}
	}
	r.mu.Unlock()
}

func (r *execRegistry) reapLoop() {
	ticker := time.NewTicker(execReaperInterval)
	defer ticker.Stop()
	defer close(r.reaperDone)
	for {
		select {
		case now := <-ticker.C:
			r.reapCompleted(now)
		case <-r.reaperStop:
			return
		}
	}
}

func (r *execRegistry) reapCompleted(now time.Time) {
	pointers := r.jobPointers()
	remove := make(map[string]*execJob)
	for _, job := range pointers {
		finished := job.snapshot().finished
		if !finished.IsZero() && now.Sub(finished) >= execCompletedRetention {
			remove[job.id] = job
		}
	}
	r.mu.Lock()
	for id, job := range remove {
		if r.jobs[id] == job {
			delete(r.jobs, id)
		}
	}
	r.mu.Unlock()
}

func (r *execRegistry) lookup(ctx context.Context, id string) (*execJob, string, string) {
	r.mu.RLock()
	job := r.jobs[id]
	r.mu.RUnlock()
	if job == nil {
		return nil, execStatusNotFound, "job not found"
	}
	owner, ok := tools.ExecutionContextFrom(ctx)
	if !ok || owner.SessionID == "" || owner.SessionID != job.owner.SessionID {
		return nil, execStatusAccessDenied, "job is not owned by this session"
	}
	if job.scope == execScopeRun && (owner.RunID != job.owner.RunID || owner.BoundaryID != job.owner.BoundaryID) {
		return nil, execStatusAccessDenied, "job is not owned by this run boundary"
	}
	if job.scope == execScopeSession && !isMainBoundary(owner.BoundaryID) {
		return nil, execStatusAccessDenied, "session job is only accessible from the main boundary"
	}
	return job, "", ""
}

func (r *execRegistry) status(ctx context.Context, params map[string]any) tools.Result {
	id, _ := params["job_id"].(string)
	if id == "" {
		return makeExecResult("status", "", "", execStatusNotFound, nil, true, "job_id is required", false, nil)
	}
	job, failureStatus, reason := r.lookup(ctx, id)
	if job == nil {
		return makeExecResult("status", "", id, failureStatus, nil, true, reason, false, nil)
	}
	cursor := int64(0)
	if value, ok := params["cursor"]; ok {
		number, valid := value.(float64)
		if !valid || number < 0 || number != float64(int64(number)) {
			snapshot := job.snapshot()
			return makeExecResult("status", job.scope, id, snapshot.status, nil, true, "cursor must be a non-negative integer", false, snapshot.fields())
		}
		cursor = int64(number)
	}
	// 输出使用独立锁快照；所有状态字段来自同一个 job 快照。
	data, next, truncated := job.output.snapshot(cursor)
	snapshot := job.snapshot()
	extra := snapshot.fields()
	extra["next_cursor"] = next
	// status 仅观察后台结果，即使进程非零退出或清理不完整也不是工具调用错误。
	return makeExecResult("status", job.scope, id, snapshot.status, data, false, "", truncated, extra)
}

func stopResult(job *execJob, id string, snapshot execJobSnapshot) tools.Result {
	extra := snapshot.fields()
	if snapshot.cleanupStatus == "partial" {
		return makeExecResult("stop", job.scope, id, snapshot.status, nil, true, "partial: job reached a terminal state but Wait or output drain did not complete", false, extra)
	}
	return makeExecResult("stop", job.scope, id, snapshot.status, nil, false, "", false, extra)
}

func (r *execRegistry) stop(ctx context.Context, params map[string]any) tools.Result {
	id, _ := params["job_id"].(string)
	if id == "" {
		return makeExecResult("stop", "", "", execStatusNotFound, nil, true, "job_id is required", false, nil)
	}
	job, failureStatus, reason := r.lookup(ctx, id)
	if job == nil {
		return makeExecResult("stop", "", id, failureStatus, nil, true, reason, false, nil)
	}
	if snapshot := job.snapshot(); snapshot.status != execStatusRunning {
		return stopResult(job, id, snapshot)
	}
	job.requestStop()
	// 优先检查终态，使已完成任务不因同时取消的调用上下文被误报 partial。
	select {
	case <-job.done:
		return stopResult(job, id, job.snapshot())
	default:
	}
	timer := time.NewTimer(execJobStopLimit)
	defer timer.Stop()
	select {
	case <-job.done:
		return stopResult(job, id, job.snapshot())
	case <-ctx.Done():
		snapshot := job.snapshot()
		if snapshot.status != execStatusRunning {
			return stopResult(job, id, snapshot)
		}
		extra := snapshot.fields()
		extra["cleanup_status"] = "partial"
		return makeExecResult("stop", job.scope, id, snapshot.status, nil, true, "partial: stop interrupted before the job reached a terminal state", false, extra)
	case <-timer.C:
		snapshot := job.snapshot()
		if snapshot.status != execStatusRunning {
			return stopResult(job, id, snapshot)
		}
		extra := snapshot.fields()
		extra["cleanup_status"] = "partial"
		return makeExecResult("stop", job.scope, id, snapshot.status, nil, true, "partial: job did not reach a terminal state before the stop deadline", false, extra)
	}
}

// cleanup 使用一个共享截止时间等待全部任务，等待结束后才从注册表删除生命周期匹配项。
func (r *execRegistry) cleanup(ctx context.Context, reason string, match func(*execJob) bool) error {
	cleanupCtx, cancel := context.WithTimeout(ctx, execLifecycleCleanupLimit)
	defer cancel()

	var jobs []*execJob
	for _, job := range r.jobPointers() {
		if match(job) {
			jobs = append(jobs, job)
		}
	}
	if len(jobs) == 0 {
		return nil
	}
	for _, job := range jobs {
		job.requestTerminate(execStatusStopped, reason)
	}

	var cleanupErrors []error
	for _, job := range jobs {
		select {
		case <-job.done:
			snapshot := job.snapshot()
			if snapshot.cleanupStatus == "partial" {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("exec job %s cleanup partial: Wait or output drain incomplete", job.id))
			}
		case <-cleanupCtx.Done():
			cleanupErrors = append(cleanupErrors, fmt.Errorf("exec job %s cleanup partial: %w", job.id, cleanupCtx.Err()))
		}
	}

	// 生命周期边界结束后 partial 任务也必须失去外部访问。
	r.mu.Lock()
	for _, job := range jobs {
		if r.jobs[job.id] == job {
			delete(r.jobs, job.id)
		}
	}
	r.mu.Unlock()
	return errors.Join(cleanupErrors...)
}

func (r *execRegistry) close(contexts ...context.Context) error {
	ctx := context.Background()
	if len(contexts) > 0 && contexts[0] != nil {
		ctx = contexts[0]
	}
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closed = true
		r.mu.Unlock()
		close(r.reaperStop)
		<-r.reaperDone
		r.closeErr = r.cleanup(ctx, "provider_close", func(*execJob) bool { return true })
	})
	return r.closeErr
}

func isMainBoundary(boundary string) bool { return boundary == "" || boundary == "main" }
