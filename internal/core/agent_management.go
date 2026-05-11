package core

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/alanchenchen/suna/internal/capability"
	"github.com/alanchenchen/suna/internal/guard"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
)

func (a *Agent) MemoryStats(ctx context.Context) (episodes, entities, facts int) {
	if a.episodic != nil {
		episodes, _ = a.episodic.Count(ctx)
	}
	if a.semantic != nil {
		facts, _ = a.semantic.Count(ctx)
	}
	if a.entities != nil {
		entities, _ = a.entities.Count(ctx)
	}
	return
}

func (a *Agent) SessionStats(ctx context.Context) (active, completed int, lastID string) {
	if a.sessions == nil {
		return
	}
	active, _ = a.sessions.CountByStatus(ctx, "active")
	completed, _ = a.sessions.CountByStatus(ctx, "completed")
	info, _ := a.sessions.LastActiveSession(ctx)
	if info != nil {
		lastID = info.ID
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

func (a *Agent) WorkingTokens() int {
	return a.working.EstimatedTokens()
}

func (a *Agent) SearchMemory(ctx context.Context, query string, limit int) ([]*memory.EpisodicMemory, error) {
	return a.episodic.SearchFTS(ctx, query, limit)
}

func (a *Agent) ListCapabilities() []capability.Info {
	if a.caps == nil {
		return nil
	}
	return a.caps.List()
}

func (a *Agent) SemanticSummary(ctx context.Context) (string, error) {
	if a.semantic == nil {
		return "", nil
	}
	return a.semantic.Summary(ctx)
}

func (a *Agent) ReloadCapabilities() error {
	if a.caps == nil {
		a.caps = capability.NewLoader()
	}
	capDir := filepath.Join(a.cfg.DataDir, "capabilities")
	return a.caps.Reload(context.Background(), capDir)
}

func (a *Agent) Compact(ctx context.Context) (int, int, int, int, int, error) {
	msgs := a.working.Messages()
	if len(msgs) <= 10 {
		return 0, 0, 0, 0, 0, fmt.Errorf("too few messages to compress (%d)", len(msgs))
	}
	before := a.working.EstimatedTokens()
	compressed, summary, compErr := a.compressor.CompressHistory(ctx, msgs)
	if compErr != nil {
		return 0, 0, 0, 0, 0, compErr
	}
	if summary == "" {
		return 0, 0, 0, 0, 0, fmt.Errorf("compression produced no summary")
	}
	turnsCompressed := len(msgs) - len(compressed)
	if turnsCompressed < 0 {
		turnsCompressed = 0
	}
	a.working.SetMessages(compressed)
	after := a.working.EstimatedTokens()

	truncated := 0
	for _, m := range msgs {
		if m.Role == model.RoleTool && len(m.Text()) > 50*1024 {
			truncated++
		}
	}
	contextWindow := 128000
	if p, err := a.router.Provider(""); err == nil && p != nil {
		contextWindow = p.ContextWindow()
	}
	return before, after, contextWindow, turnsCompressed, truncated, nil
}

func (a *Agent) NewSession() {
	pendingCtx := ""
	if a.sessions != nil && a.sessionID != "" {
		msgs := a.working.Messages()
		hasContent := false
		for _, m := range msgs {
			if m.Role == model.RoleUser || m.Role == model.RoleAssistant {
				if m.Text() != "" {
					hasContent = true
					break
				}
			}
		}
		if hasContent {
			a.sessions.CompleteSession(context.Background(), a.sessionID)
			unextracted, _ := a.sessions.LoadUnextractedMessages(context.Background(), a.sessionID, 5)
			if len(unextracted) > 0 {
				var parts []string
				for _, m := range unextracted {
					parts = append(parts, fmt.Sprintf("- [%s] %s (source: previous session)", m.Role, truncateStr(m.Content, 200)))
				}
				pendingCtx = strings.Join(parts, "\n")
			}
			a.extractQueue.EnqueueSession(context.Background(), a.sessionID)
		}
	}
	a.sessionID = uuid.New().String()
	a.turnCount = 0
	if len(a.cfg.Guard.Blocked) > 0 || len(a.cfg.Guard.Allowed) > 0 {
		var blockedPats, blockedReasons []string
		for _, b := range a.cfg.Guard.Blocked {
			blockedPats = append(blockedPats, b.Pattern)
			blockedReasons = append(blockedReasons, b.Reason)
		}
		var allowedPats, allowedTools []string
		for _, al := range a.cfg.Guard.Allowed {
			allowedPats = append(allowedPats, al.Pattern)
			allowedTools = append(allowedTools, al.Tool)
		}
		a.guard = guard.NewGuardWithConfig(a.store.DB(), a.sessionID, blockedPats, blockedReasons, allowedPats, allowedTools)
	} else {
		a.guard = guard.NewGuard(a.store.DB(), a.sessionID)
	}
	a.working.Clear()
	if pendingCtx != "" {
		a.working.AddMessage(model.NewTextMessage(model.RoleSystem,
			"## Relevant memory from previous session\n"+pendingCtx))
	}
}

func (a *Agent) RestoreSession(ctx context.Context) int {
	if a.sessions == nil {
		return 0
	}

	info, err := a.sessions.LastActiveSession(ctx)
	if err != nil || info == nil {
		a.sessions.CreateSession(ctx, a.sessionID)
		return 0
	}

	a.sessions.CompleteOtherSessions(ctx, info.ID)

	msgs, err := a.sessions.LoadMessages(ctx, info.ID)
	if err != nil || len(msgs) == 0 {
		a.sessionID = info.ID
		a.guard = guard.NewGuard(a.store.DB(), a.sessionID)
		return 0
	}

	a.sessionID = info.ID
	a.turnCount = msgs[len(msgs)-1].Turn
	a.guard = guard.NewGuard(a.store.DB(), a.sessionID)

	a.working.Clear()
	a.resumeInput = ""
	visibleMsgs := msgs
	if last := len(visibleMsgs) - 1; last >= 0 && visibleMsgs[last].Role == string(model.RoleUser) {
		// 最后一条孤立用户消息代表上次尚未获得回复，恢复到输入框并避免重复进入 LLM 上下文。
		a.resumeInput = visibleMsgs[last].Content
		a.sessions.DeleteLastMessage(ctx, info.ID, visibleMsgs[last].Turn, visibleMsgs[last].Role, visibleMsgs[last].Content)
		visibleMsgs = visibleMsgs[:last]
		a.turnCount = 0
		if len(visibleMsgs) > 0 {
			a.turnCount = visibleMsgs[len(visibleMsgs)-1].Turn
		}
	}
	for _, m := range visibleMsgs {
		a.working.AddMessage(model.NewTextMessage(model.Role(m.Role), m.Content))
	}

	return len(msgs)
}

func (a *Agent) ConsumeResumeInput() string {
	input := a.resumeInput
	a.resumeInput = ""
	return input
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
