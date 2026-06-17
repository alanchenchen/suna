package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

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
	if a.conversation == nil {
		return
	}
	st, _ := a.conversation.Load(ctx, memory.DefaultUserID)
	if st != nil && len(st.LastMessages) > 0 {
		active = 1
		lastID = "last"
	}
	return
}

func (a *Agent) UsageSummary(ctx context.Context, since time.Time) (*memory.UsageSummary, error) {
	if a.sessions == nil {
		return nil, fmt.Errorf("session store not initialized")
	}
	return a.sessions.UsageSummary(ctx, since)
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

func (a *Agent) ActiveModelRuntime() ModelRuntime {
	a.configMu.RLock()
	cfg := a.cfg
	router := a.router
	a.configMu.RUnlock()

	rt := ModelRuntime{}
	if cfg != nil {
		if mc, ok := cfg.ActiveModelConfig(); ok {
			rt.Provider = mc.Provider
			rt.Model = mc.Model
		}
	}
	if router != nil {
		// context window 以 runtime provider 为准；provider 内部统一处理配置值和默认值。
		rt.ContextWindow = router.ActiveContextWindow()
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

func (a *Agent) Compact(ctx context.Context) (int, int, int, int, int, error) {
	r := &runner.Runner{Router: a.router, Compressor: a.compressor}
	contextWindow := 0
	if a.router != nil {
		contextWindow = a.router.ActiveContextWindow()
	}
	started := time.Now()
	beforeEstimate := 0
	messageCount := 0
	if a.working != nil {
		beforeEstimate = a.working.EstimatedTokens()
		messageCount = a.working.Len()
	}
	logging.Info("memory", "session_compact_start", logging.Event{"mode": "manual", "context_window": contextWindow, "before_tokens": beforeEstimate, "messages": messageCount})
	before, after, turnsCompressed, truncated, state, err := r.Compact(ctx, a.working, a.sessionState, contextWindow)
	if err != nil {
		logging.Error("memory", "session_compact_failed", err, logging.Event{"mode": "manual", "context_window": contextWindow, "before_tokens": beforeEstimate, "messages": messageCount, "duration_ms": time.Since(started).Milliseconds()})
		return 0, 0, 0, 0, 0, err
	}
	if state != "" {
		if err := a.commitCompactState(ctx, state); err != nil {
			return 0, 0, 0, 0, 0, err
		}
	} else {
		a.saveConversationState(ctx)
	}
	logging.Info("memory", "session_compact_success", logging.Event{"mode": "manual", "context_window": contextWindow, "before_tokens": before, "after_tokens": after, "folded_messages": turnsCompressed, "truncated_tool_outputs": truncated, "duration_ms": time.Since(started).Milliseconds()})
	return before, after, contextWindow, turnsCompressed, truncated, nil
}

func (a *Agent) NewSession() {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	if a.mediaStore != nil {
		_, _, _ = a.mediaStore.Clear()
	}
	if a.conversation != nil {
		a.conversation.ClearLastMessages(context.Background(), memory.DefaultUserID)
	}
	a.sessionID = uuid.New().String()
	a.turnCount = 0
	a.guard = a.newGuardForSession(a.sessionID)
	a.working.Clear()
	a.sessionState = ""
	a.toolSummary = nil
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
	bytes, count, err = a.mediaStore.Usage()
	return root, removedBytes, removedCount, bytes, count, err
}

type RestoreSessionResult struct {
	Messages  int
	Compacted bool
}

func (a *Agent) RestoreSession(ctx context.Context) RestoreSessionResult {
	if a.conversation == nil {
		return RestoreSessionResult{}
	}
	st, err := a.conversation.Load(ctx, memory.DefaultUserID)
	if err != nil || st == nil || len(st.LastMessages) == 0 {
		return RestoreSessionResult{}
	}
	a.sessionID = uuid.New().String()
	a.turnCount = 0
	a.guard = a.newGuardForSession(a.sessionID)
	a.working.Clear()
	a.sessionState = strings.TrimSpace(st.SessionState)
	a.toolSummary = nil
	for _, m := range st.LastMessages {
		a.working.AddMessage(m)
	}
	return RestoreSessionResult{Messages: len(st.LastMessages), Compacted: a.sessionState != ""}
}

func (a *Agent) RestoreToolSummary(ctx context.Context) string {
	if a.conversation == nil {
		return ""
	}
	st, err := a.conversation.Load(ctx, memory.DefaultUserID)
	if err != nil || st == nil {
		return ""
	}
	return memory.FormatToolSummary(st.ToolSummary)
}

func (a *Agent) WorkingMessages() []model.Message {
	if a.working == nil {
		return nil
	}
	return a.working.Messages()
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
