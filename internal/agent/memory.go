package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/runner"
)

func (a *Agent) enqueueMemoryEvent(ctx context.Context, role model.Role, content string, hadToolCall, toolFailed, guardBlocked, userCorrection bool) {
	if a.extractQueue == nil || content == "" || role != model.RoleUser || hadToolCall || toolFailed || guardBlocked {
		return
	}
	candidate, ok := memory.ExtractCandidate(content, userCorrection)
	if !ok {
		return
	}
	a.extractQueue.Push(ctx, memory.DefaultUserID, a.modelRef, candidate)
}

func (a *Agent) newContextMemory(summary string) *runner.ContextMemory {
	state := runner.NewContextMemory(summary)
	state.Notify = func(event runner.CompactState) {
		a.stateMu.Lock()
		notify := a.compactNotify
		a.stateMu.Unlock()
		if notify != nil {
			notify(event)
		}
	}
	state.Ready = a.tryIdleMemory

	return state
}
func (a *Agent) tryIdleMemory() {
	a.stateMu.Lock()
	if !a.runMu.TryLock() {
		a.stateMu.Unlock()
		return
	}
	a.idleMemoryDone = make(chan struct{})
	a.stateMu.Unlock()
	defer func() {
		a.stateMu.Lock()
		a.runMu.Unlock()
		close(a.idleMemoryDone)
		a.idleMemoryDone = nil
		a.stateMu.Unlock()
	}()
	state := a.CompactState()
	a.stateMu.Lock()
	failed := a.saveErr != nil
	retired := a.memoryRetired
	a.stateMu.Unlock()
	if state.State != "ready" || failed || retired {
		return
	}
	if a.contextMemory != nil {
		binding, err := a.BindModel(a.modelRef)
		if err != nil {
			a.contextMemory.Cancel()
			return
		}
		a.contextMemory.Bind(binding)
		a.contextMemory.ApplyReady(context.Background(), a.compressor)
	}
}

func (a *Agent) SetMemoryNotifications(compact func(runner.CompactState)) {
	a.stateMu.Lock()
	a.compactNotify = compact
	a.stateMu.Unlock()
}
func (a *Agent) CompactState() runner.CompactState {
	a.stateMu.Lock()
	state := a.contextMemory
	a.stateMu.Unlock()
	if state == nil {
		return runner.CompactState{}
	}
	return state.Snapshot()
}
func (a *Agent) CancelMemory() {
	a.stateMu.Lock()
	state := a.contextMemory
	a.stateMu.Unlock()
	if state != nil {
		state.Cancel()
	}
}
func (a *Agent) prepareMemory(ctx context.Context) error {
	a.stateMu.Lock()
	retired := a.memoryRetired
	dirty := a.saveErr != nil
	a.stateMu.Unlock()
	if retired {
		return fmt.Errorf("session memory has been disposed")
	}
	if a.working != nil {
		a.working.SetMessages(memory.RecoverMessages(a.working.Messages()))
	}
	if dirty {
		if err := a.saveConversationState(ctx); err != nil {
			return fmt.Errorf("session cannot continue until known results are saved: %w", err)
		}
	}
	if a.contextMemory == nil {
		a.stateMu.Lock()
		a.contextMemory = a.newContextMemory(a.summary)
		a.stateMu.Unlock()
	}
	return nil
}
func (a *Agent) finishMemory() {
	if a.working == nil {
		return
	}
	// 当前 run 结束后才替换 RAM 图片；DB 检查点早已使用同一文字投影。
	a.working.SetMessages(memory.ProjectMessages(a.working.Messages()))
}

func (a *Agent) saveConversationState(ctx context.Context) error {
	summary := a.summary
	if a.contextMemory != nil {
		summary = a.contextMemory.Summary()
	}
	err := a.persistMemory(ctx, summary, a.working.Messages())
	a.stateMu.Lock()
	a.saveErr = err
	a.stateMu.Unlock()
	return err
}
func (a *Agent) persistMemory(ctx context.Context, summary string, messages []memory.Message) error {
	if a.stateStore == nil || a.sessionID == "" {
		return nil
	}
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err := a.stateStore.Save(saveCtx, a.sessionID, summary, messages); err != nil {
		return err
	}
	if a.sessionStore != nil {
		a.sessionStore.SetMessageCount(saveCtx, a.sessionID, len(visibleMessagesForCount(messages)))
	}
	return nil
}

func (a *Agent) commitMemory(ctx context.Context, summary string, messages []memory.Message) error {
	if err := a.persistMemory(ctx, summary, messages); err != nil {
		return err
	}
	a.summary = summary
	return nil
}
func visibleMessagesForCount(messages []memory.Message) []memory.Message {
	out := make([]memory.Message, 0, len(messages))
	for _, m := range messages {
		if (m.Role == model.RoleUser || m.Role == model.RoleAssistant) && (m.Text() != "" || m.Observation != nil) {
			out = append(out, m)
		}
	}
	return out
}
func (a *Agent) maintainMemory(ctx context.Context, manual bool) error {
	if err := a.prepareMemory(ctx); err != nil {
		return err
	}
	binding, err := a.router.Bind(a.modelRef)
	if err != nil {
		return err
	}
	system, err := a.buildSystemPrompt(ctx)
	if err != nil {
		return err
	}
	project := a.requestProjection(ctx)
	req := model.CompletionRequest{System: system, Tools: a.buildToolDefs(), MaxTokens: binding.MaxOutputTokens()}
	return a.contextMemory.Maintain(ctx, a.compressor, runner.CompactRequest{Binding: binding, Invocation: model.Invocation{SessionScope: model.SessionScope(a.sessionID)}, Working: a.working, Request: req, Project: project, Coefficient: a.calibrator.Coefficient(binding.Ref()), Calibrated: a.calibrator.Calibrated(binding.Ref()), Commit: a.commitMemory, Usage: func(u *model.Usage) { a.RecordUsage(context.WithoutCancel(ctx), binding.ModelID(), u) }}, manual)
}

func (a *Agent) ReserveCompact(ctx context.Context) (string, func() error, error) {
	if !a.tryRunLock(ctx) {
		return "", nil, fmt.Errorf("session is already running")
	}
	a.syncRuntime()
	if err := a.prepareMemory(ctx); err != nil {
		a.runMu.Unlock()
		return "", nil, err
	}
	// 预留ID之前先失效旧模型候选，避免后续Bind取消掉已回给客户端的ID。
	binding, err := a.router.Bind(a.modelRef)
	if err != nil {
		a.runMu.Unlock()
		return "", nil, err
	}
	a.contextMemory.Bind(binding)
	id := a.contextMemory.ReserveManual()
	runCtx, cancel := context.WithCancel(ctx)
	a.cancelMu.Lock()
	a.cancelFn = cancel
	a.cancelMu.Unlock()
	run := func() error {
		defer a.runMu.Unlock()
		defer cancel()
		defer func() { a.cancelMu.Lock(); a.cancelFn = nil; a.cancelMu.Unlock() }()
		return a.maintainMemory(runCtx, true)
	}
	return id, run, nil
}
func (a *Agent) Compact(ctx context.Context) error {
	_, run, err := a.ReserveCompact(ctx)
	if err != nil {
		return err
	}
	return run()
}

// StopMemory 在既有 stateOps 保护下结束候选和已开始的短空闲提交，不永久退休。
// 删除尚可能失败、卸载尚需复验时使用；调用期间不能再准入新的 run。
func (a *Agent) StopMemory() {
	a.stateMu.Lock()
	state := a.contextMemory
	a.stateMu.Unlock()
	if state != nil {
		state.Stop()
	}
	a.runMu.Lock()
	a.runMu.Unlock()
}

func (a *Agent) DisposeMemory() {
	a.stateMu.Lock()
	a.memoryRetired = true
	a.stateMu.Unlock()
	a.StopMemory()
}

func (a *Agent) HasUnsavedMemory() bool {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return a.saveErr != nil
}

// tryRunLock 只等待已开始的短空闲提交；活跃run仍即时拒绝，不能排第二份任务。
func (a *Agent) tryRunLock(ctx context.Context) bool {
	for {
		a.stateMu.Lock()
		idle := a.idleMemoryDone
		if idle == nil {
			ok := a.runMu.TryLock()
			a.stateMu.Unlock()
			return ok
		}
		a.stateMu.Unlock()
		select {
		case <-idle:
		case <-ctx.Done():
			return false
		}
	}
}
