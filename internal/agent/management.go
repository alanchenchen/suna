package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
)

func (a *Agent) MemoryStats(ctx context.Context) (active, core, queued int) {
	if a.memories != nil {
		active, core = a.memories.Count(ctx, memory.DefaultUserID)
	}
	if a.store != nil {
		queued = memory.QueueCount(ctx, a.store.DB(), memory.DefaultUserID)
	}
	return active, core, queued
}

func (a *Agent) SessionStats(ctx context.Context) (active, completed int, lastID string) {
	if a.sessionStore == nil {
		return
	}
	items, _ := a.sessionStore.List(ctx)
	for _, item := range items {
		if item.MessageCount > 0 {
			active++
			if lastID == "" {
				lastID = item.ID
			}
		}
	}
	return
}

func (a *Agent) UsageSummary(ctx context.Context, since time.Time) (*memory.UsageSummary, error) {
	if a.usage == nil {
		return nil, fmt.Errorf("session store not initialized")
	}
	return a.usage.UsageSummary(ctx, since)
}

func (a *Agent) PruneOperationalLogs(ctx context.Context, now time.Time) (memory.OperationalPruneResult, error) {
	root := a.root()
	if root == nil || root.store == nil {
		return memory.OperationalPruneResult{}, fmt.Errorf("memory store not initialized")
	}
	return root.store.PruneOperationalLogs(ctx, now)
}

func (a *Agent) ListModels() []string {
	if a.router == nil {
		return nil
	}
	return a.router.ListModelRefs()
}

type ModelRuntime struct {
	Provider      string
	Model         string
	ContextWindow int
}

func (a *Agent) DefaultModelRuntime() ModelRuntime {
	a.configMu.RLock()
	cfg := a.cfg
	a.configMu.RUnlock()

	rt := ModelRuntime{}
	if cfg == nil {
		return rt
	}
	if mc, ok := cfg.ActiveModelConfig(); ok {
		rt.Provider = mc.Provider
		rt.Model = mc.Model
		rt.ContextWindow = mc.ContextWindow
	}
	return rt
}

func (a *Agent) PopLastUserMessage() {
	if a.working == nil {
		return
	}
	msgs := a.working.Messages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == model.RoleUser {
			a.working.SetMessages(append(msgs[:i], msgs[i+1:]...))
			return
		}
	}
}

func (a *Agent) WorkingTokens() int { return a.working.EstimatedTokens() }

func (a *Agent) ListMemory(ctx context.Context) ([]memory.UserMemory, error) {
	if a.memories == nil {
		return nil, nil
	}
	return a.memories.List(ctx, memory.DefaultUserID, memory.MaxActiveMemories)
}

func (a *Agent) DeleteMemory(ctx context.Context, id string) (bool, error) {
	if a.memories == nil {
		return false, nil
	}
	return a.memories.Delete(ctx, memory.DefaultUserID, id)
}

func (a *Agent) ClearMemory(ctx context.Context) (int, error) {
	if a.memories == nil {
		return 0, nil
	}
	return a.memories.Clear(ctx, memory.DefaultUserID)
}

func (a *Agent) AttachmentStatus() (root string, bytes int64, count int, err error) {
	if a.mediaStore == nil {
		return "", 0, 0, nil
	}
	bytes, count, err = a.mediaStore.Usage()
	return a.mediaStore.Root, bytes, count, err
}

func (a *Agent) ClearAttachments() (root string, removedBytes int64, removedCount int, bytes int64, count int, err error) {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	if a.mediaStore == nil {
		return "", 0, 0, 0, 0, nil
	}
	root = a.mediaStore.Root
	removedBytes, removedCount, err = a.mediaStore.Clear()
	if err != nil {
		return root, removedBytes, removedCount, 0, 0, err
	}
	if root != "" {
		if err := os.RemoveAll(root); err != nil {
			return root, removedBytes, removedCount, 0, 0, err
		}
	}
	bytes, count, err = a.mediaStore.Usage()
	return root, removedBytes, removedCount, bytes, count, err
}

func (a *Agent) CleanupToolSession(ctx context.Context, sessionID string) error {
	root := a.root()
	if root == nil || root.tools == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	return root.tools.CleanupSession(ctx, sessionID)
}

func (a *Agent) Close() {
	a.closeOnce.Do(func() {
		a.CancelMemory()
		a.lifecycleMu.Lock()
		a.closed = true
		workerStarted := a.workerStarted
		a.lifecycleMu.Unlock()
		a.catalogMu.Lock()
		if a.catalogTimer != nil {
			a.catalogTimer.Stop()
			a.catalogTimer = nil
		}
		waiters := a.catalogWaiters
		a.catalogWaiters = nil
		a.catalogMu.Unlock()
		for _, waiter := range waiters {
			waiter <- context.Canceled
		}
		if a.extractQueue != nil {
			a.extractQueue.Close()
		}
		if a.extractWorker != nil && workerStarted {
			a.extractWorker.Wait()
		}
		if a.tools != nil {
			closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = a.tools.Close(closeCtx)
			cancel()
		}
		if a.store != nil {
			a.store.Close()
		}
	})
}

func (a *Agent) CancelCurrentRun() {
	// 先关闭run准入上下文，再推进候选代次；排槽/规划中的请求也必须观察到取消。
	a.cancelMu.Lock()
	cancel := a.cancelFn
	a.cancelFn = nil
	a.cancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.CancelMemory()
}
