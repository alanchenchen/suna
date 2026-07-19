package agent

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/alanchenchen/suna/internal/logging"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/runner"
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

func (a *Agent) ListModels() []string {
	if a.router == nil {
		return nil
	}
	return a.router.ListProviders()
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

func (a *Agent) Compact(ctx context.Context) (int, int, int, int, int, error) {
	// Compact 不走 Run，但同样占用 session working state；用 runMu 与 Run/ResumeRun
	// 串行化，避免覆盖其他执行中的取消函数或并发改写上下文。
	a.runMu.Lock()
	defer a.runMu.Unlock()

	// 手动 compact 不经过 Run；仍需注册当前取消函数，保证最后一个 session
	// attachment 离开时可终止模型请求并让 runtime 尽快卸载。
	runCtx, cancel := context.WithCancel(ctx)
	a.cancelMu.Lock()
	a.cancelFn = cancel
	a.cancelMu.Unlock()
	defer func() {
		a.cancelMu.Lock()
		a.cancelFn = nil
		a.cancelMu.Unlock()
		cancel()
	}()

	// 手动 compact 不经过 Run；先同步 session 的共享运行时，避免配置重载后仍从旧 router 建立 binding。
	a.syncRuntime()
	// 本次 compact 从同一个局部快照读取并使用 modelRef，不能在校验后再次读取可变的 session 字段。
	modelRef := a.modelRef
	router := a.router
	if router == nil || modelRef == "" {
		return 0, 0, 0, 0, 0, fmt.Errorf("session model is not configured")
	}
	binding, err := router.Bind(modelRef)
	if err != nil {
		return 0, 0, 0, 0, 0, err
	}
	r := &runner.Runner{Compressor: a.compressor}
	contextWindow := binding.ContextWindow()
	outputBudget := binding.MaxOutputTokens()
	started := time.Now()
	beforeEstimate := 0
	messageCount := 0
	if a.working != nil {
		beforeEstimate = a.working.EstimatedTokens()
		messageCount = a.working.Len()
	}
	logging.Info("memory", "session_compact_start", logging.Event{"mode": "manual", "context_window": contextWindow, "output_budget": outputBudget, "before_tokens": beforeEstimate, "messages": messageCount})
	before, after, turnsCompressed, truncated, state, err := r.Compact(runCtx, binding, a.working, a.sessionState)
	if err != nil {
		logging.Error("memory", "session_compact_failed", err, logging.Event{"mode": "manual", "context_window": contextWindow, "output_budget": outputBudget, "before_tokens": beforeEstimate, "messages": messageCount, "duration_ms": time.Since(started).Milliseconds()})
		return 0, 0, 0, 0, 0, err
	}
	if state != "" {
		if err := a.commitCompactState(ctx, state); err != nil {
			return 0, 0, 0, 0, 0, err
		}
	} else {
		a.saveConversationState(ctx)
	}
	logging.Info("memory", "session_compact_success", logging.Event{"mode": "manual", "context_window": contextWindow, "output_budget": outputBudget, "before_tokens": before, "after_tokens": after, "folded_messages": turnsCompressed, "truncated_tool_outputs": truncated, "duration_ms": time.Since(started).Milliseconds()})
	return before, after, contextWindow, turnsCompressed, truncated, nil
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

func (a *Agent) Close() {
	a.closeOnce.Do(func() {
		a.closed = true
		if a.extractQueue != nil {
			a.extractQueue.Close()
		}
		if a.extractWorker != nil {
			a.extractWorker.Wait()
		}
		if a.tools != nil {
			_ = a.tools.Close(context.Background())
		}
		if a.store != nil {
			a.store.Close()
		}
	})
}

func (a *Agent) CancelCurrentRun() {
	a.cancelMu.Lock()
	defer a.cancelMu.Unlock()
	if a.cancelFn != nil {
		a.cancelFn()
		a.cancelFn = nil
	}
}
