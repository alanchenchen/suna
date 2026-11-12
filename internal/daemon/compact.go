package daemon

import (
	"context"
	"errors"
	"sync"

	"github.com/alanchenchen/suna/internal/agent"
	"github.com/alanchenchen/suna/internal/logging"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/runner"
)

// logCompactState 是后台与手动终态共用的日志字段构造，避免两处字段漂移。
func logCompactState(sessionID string, state runner.CompactState, manual bool) {
	fields := logging.Event{"session_id": sessionID, "compact_id": state.ID, "kind": state.Kind, "state": state.State, "wait": state.Wait, "prepare_ms": state.PrepareElapsed.Milliseconds(), "wait_ms": state.WaitElapsed.Milliseconds(), "before_tokens": state.BeforeTokens, "after_tokens": state.AfterTokens, "reason": state.Reason, "error": state.Error}
	if manual {
		fields["manual"] = true
	}
	logging.Info("memory", "compact_state", fields)
}

func compactStatePayload(sessionID string, state runner.CompactState) protocol.CompactState {
	out := protocol.CompactState{SessionID: sessionID, CompactID: state.ID, Kind: string(state.Kind), State: state.State, Wait: state.Wait, PrepareElapsedMs: state.PrepareElapsed.Milliseconds(), WaitElapsedMs: state.WaitElapsed.Milliseconds(), Reason: state.Reason, Error: state.Error}
	if state.State == "applied" {
		out.Result = &protocol.CompactResult{BeforeTokens: state.BeforeTokens, AfterTokens: state.AfterTokens, ContextWindow: state.ContextWindow}
	}
	return out
}

func (s *service) bindMemoryNotifications(sessionID string, rt *sessionRuntime) {
	rt.agent.SetMemoryNotifications(func(state runner.CompactState) {
		s.handleMemoryState(sessionID, rt, state)
	})
}

func (s *service) handleMemoryState(sessionID string, rt *sessionRuntime, state runner.CompactState) {
	// 后台通知不依赖 run channel；runtime 卸载前取消候选，迟到生成不会重新提交。
	s.daemon.sessions.mu.RLock()
	active := s.daemon.sessions.runtime[sessionID] == rt
	s.daemon.sessions.mu.RUnlock()
	if !active {
		return
	}
	ctx := context.Background()
	logCompactState(sessionID, state, false)
	if state.Wait == "hard_limit" {
		s.daemon.sessions.mu.RLock()
		current := s.daemon.sessions.runtime[sessionID]
		runID := ""
		if current == rt && (rt.phase != protocol.AgentRunPhaseCompact || rt.status != sessionCompacting) {
			runID = rt.runID
		}
		s.daemon.sessions.mu.RUnlock()
		if runID != "" {
			accepted := s.daemon.sessions.transitionRunState(sessionID, runID, protocol.AgentRunRunning, func(protocol.AgentRunState) {
				// phase 与 status 同锁更新，使实时通知和 attach 快照一致；取消中的 run 不回退。
				s.daemon.sessions.mu.Lock()
				if s.daemon.sessions.runtime[sessionID] != rt || rt.runID != runID {
					s.daemon.sessions.mu.Unlock()
					return
				}
				rt.phase = protocol.AgentRunPhaseCompact
				s.daemon.sessions.setStatusLocked(sessionID, sessionCompacting)
				owner := rt.runOwner
				s.daemon.sessions.mu.Unlock()
				s.emitAgentRun(ctx, sessionID, owner, protocol.AgentRunParams{SessionID: sessionID, RunID: runID, State: protocol.AgentRunRunning, Phase: protocol.AgentRunPhaseCompact})
			})
			if accepted {
				s.daemon.broadcastSessionState(ctx, sessionID)
			}
		}
	}
	emit(ctx, multiSink(s.daemon.sessions.sinksForSession(s.daemon, sessionID)), protocol.NotifyCompactState, compactStatePayload(sessionID, state))
}

func inputProtocolParts(input agent.Input) []protocol.MessagePart {
	// 广播已保存输入的原始多模态块；StoredBlocks 仅供持久化文字投影，不能替代附件语义。
	parts := make([]protocol.MessagePart, 0, len(input.Blocks))
	for _, block := range input.Blocks {
		switch block.Type {
		case model.ContentText:
			parts = append(parts, protocol.MessagePart{Type: "text", Text: block.Text})
		case model.ContentImage:
			if block.Media == nil {
				continue
			}
			ref := block.Media
			parts = append(parts, protocol.MessagePart{Type: "image", Source: protocol.AttachmentRef{Kind: string(ref.Kind), Path: ref.Path, URL: ref.URL, MimeType: ref.MimeType, Name: ref.Name, Size: ref.Size}})
		}
	}
	return parts
}

func (s *service) handleCompact(ctx context.Context, req protocol.Request, _ protocol.EventSink) (any, error) {
	var id string
	var run func() error
	var reserveErr error
	rt, sessionID, runID, err := s.daemon.sessions.beginRunWithNotification(req.ConnID, func(current *sessionRuntime, session, runID string) {
		id, run, reserveErr = current.agent.ReserveCompact(ctx)
		if reserveErr != nil {
			return
		}
		s.daemon.sessions.setPhase(session, protocol.AgentRunPhaseCompact)
		s.daemon.sessions.setStatus(session, sessionCompacting)
		s.emitAgentRun(ctx, session, req.ConnID, protocol.AgentRunParams{SessionID: session, RunID: runID, State: protocol.AgentRunRunning, Phase: protocol.AgentRunPhaseCompact})
	})
	if err != nil {
		return nil, requestErrorForState(err)
	}
	if reserveErr != nil {
		s.daemon.sessions.setStatusIfCurrentRun(sessionID, runID, sessionIdle)
		return nil, reserveErr
	}
	s.bindMemoryNotifications(sessionID, rt)
	var mu sync.Mutex
	var terminal runner.CompactState
	// 手动结果必须等本run释放准入后再发布，避免结果已到但下一次发送仍busy。
	rt.agent.SetMemoryNotifications(func(state runner.CompactState) {
		if state.State == "applied" || state.State == "skipped" || state.State == "failed" || state.State == "cancelled" {
			mu.Lock()
			terminal = state
			mu.Unlock()
			// 手动终态不经过下面的 emit，必须单独落日志，否则手动压缩结果不可追溯。
			logCompactState(sessionID, state, true)
			return
		}
		emit(context.Background(), multiSink(s.daemon.sessions.sinksForSession(s.daemon, sessionID)), protocol.NotifyCompactState, compactStatePayload(sessionID, state))
	})
	s.daemon.broadcastSessionState(ctx, sessionID)
	go func() {
		err := run()
		mu.Lock()
		state := terminal
		mu.Unlock()
		if state.ID != id {
			state = runner.CompactState{ID: id, State: "skipped", Reason: "no_source"}
			if err != nil {
				state.State = "failed"
				state.Error = err.Error()
				if errors.Is(err, context.Canceled) {
					state.State = "cancelled"
				}
			}
		}
		s.daemon.sessions.transitionRunState(sessionID, runID, protocol.AgentRunDone, func(final protocol.AgentRunState) {
			if final == protocol.AgentRunCancelled && state.State != "applied" {
				state.State = "cancelled"
				state.Wait = ""
			}
			s.bindMemoryNotifications(sessionID, rt)
			s.daemon.sessions.setStatusIfCurrentRun(sessionID, runID, sessionIdle)
			s.emitAgentRun(context.Background(), sessionID, req.ConnID, protocol.AgentRunParams{SessionID: sessionID, RunID: runID, State: final, Phase: protocol.AgentRunPhaseCompact})
		})
		emit(context.Background(), multiSink(s.daemon.sessions.sinksForSession(s.daemon, sessionID)), protocol.NotifyCompactState, compactStatePayload(sessionID, state))
		s.daemon.broadcastSessionState(context.Background(), sessionID)
	}()
	return protocol.CompactAccepted{CompactID: id}, nil
}
