package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/alanchenchen/suna/internal/agent"
	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/logging"
	"github.com/alanchenchen/suna/internal/mcp"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/skill"
	"github.com/alanchenchen/suna/internal/version"
)

const maxToolResultBytes = 16 * 1024

type service struct {
	daemon *Daemon

	// AskUser / GuardConfirm 会阻塞 agent loop；pending 记录归属 session 和 owner，支持 owner 断开后的 Handoff 接力。
	pendingAsks   sync.Map
	pendingGuards sync.Map
}

type pendingInteraction struct {
	sessionID string
	ownerID   string
	reply     chan string
	ask       *protocol.AskUserParams
	guard     *protocol.GuardConfirmParams
}

func newService(d *Daemon) *service { return &service{daemon: d} }

func (s *service) OnConnect(ctx context.Context, connID string, sink protocol.EventSink) {
	// 连接建立阶段只登记 sink，不同步推送状态。
	// Windows Named Pipe 上同步写 notification 可能阻塞，从而挡住后续 daemon.status 请求。
	_ = ctx
	s.daemon.addConnection(connID, sink)
}

func (s *service) OnDisconnect(ctx context.Context, connID string) {
	s.daemon.removeConnection(connID)
}

func (s *service) Handle(ctx context.Context, req protocol.Request, sink protocol.EventSink) (any, error) {
	logging.Info("ipc", "request", logging.Event{"conn_id": req.ConnID, "method": req.Method, "request_id": req.ID})
	sink = s.daemon.sinkFor(req.ConnID, sink)
	if req.Method == protocol.MethodDebugMemory {
		return s.handleDebugMemory(ctx, req)
	}
	s.ensureConfigLoaded()
	if skill.IsProtocolMethod(req.Method) {
		if s.daemon.agent.Skills() == nil {
			return nil, protocolError{code: -32603, message: "skill runtime is not initialized"}
		}
		return s.daemon.agent.Skills().HandleProtocol(ctx, req, sink)
	}
	if mcp.IsProtocolMethod(req.Method) {
		if s.daemon.agent.MCP() == nil {
			return nil, protocolError{code: -32603, message: "mcp runtime is not initialized"}
		}
		result, err := s.daemon.agent.MCP().HandleProtocol(ctx, req)
		if err != nil {
			return nil, err
		}
		if req.Method == protocol.MethodMCPToggle {
			cfg := s.daemon.agent.Config()
			if cfg == nil {
				return nil, protocolError{code: -32603, message: "config not loaded"}
			}
			cfg.MCP = s.daemon.agent.MCP().Config()
			if err := cfg.Save(cfg.ConfigPath()); err != nil {
				return nil, protocolError{code: -32603, message: err.Error()}
			}
		}
		// MCP toggle/reload 会改变运行态可用工具集合；协议处理成功后刷新 tools manager，
		// 让下一轮模型请求看到最新 tool schema。mcp.list 只读，不触发刷新。
		if req.Method == protocol.MethodMCPToggle || req.Method == protocol.MethodMCPReload {
			if err := s.daemon.agent.ReloadTools(ctx); err != nil {
				return nil, protocolError{code: -32603, message: err.Error()}
			}
		}
		return result, nil
	}
	switch req.Method {
	case protocol.MethodRuntimeHello:
		return s.handleRuntimeHello(req)
	case protocol.MethodSendMessage:
		return s.handleSendMessage(ctx, req, sink)
	case protocol.MethodResumeRun:
		return s.handleResumeRun(ctx, req, sink)
	case protocol.MethodCancel:
		rt, _, err := s.daemon.sessions.ensureRunOwner(req.ConnID)
		if err != nil {
			return nil, protocolError{code: -32602, message: err.Error(), data: protocol.ProtocolErrorData{Kind: err.Error()}}
		}
		rt.agent.CancelCurrentRun()
		return map[string]string{"status": "cancelled"}, nil
	case protocol.MethodAskReply:
		return s.handleAskReply(req)
	case protocol.MethodGuardReply:
		return s.handleGuardReply(req)
	case protocol.MethodMemoryList:
		return s.handleMemoryList(ctx, sink)
	case protocol.MethodMemoryDelete:
		return s.handleMemoryDelete(ctx, req, sink)
	case protocol.MethodMemoryClear:
		return s.handleMemoryClear(ctx, sink)
	case protocol.MethodSessionList:
		return s.handleSessionList(ctx, req)
	case protocol.MethodSessionCreate:
		return s.handleSessionCreate(ctx, req)
	case protocol.MethodSessionAttach:
		return s.handleSessionAttach(ctx, req)
	case protocol.MethodSessionDetach:
		return s.handleSessionDetach(ctx, req)
	case protocol.MethodSessionUpdate:
		return s.handleSessionUpdate(ctx, req)
	case protocol.MethodSessionDelete:
		return s.handleSessionDelete(ctx, req)
	case protocol.MethodCompact:
		return s.handleCompact(ctx, req, sink)
	case protocol.MethodUsage:
		return s.handleUsage(ctx), nil
	case protocol.MethodAttachmentStatus:
		return s.handleAttachmentStatus(req)
	case protocol.MethodAttachmentClear:
		return s.handleAttachmentClear(req)
	case protocol.MethodDaemonStatus:
		// daemon.status 是 CLI 启动探测和 TUI 初始拉取的快路径：只返回 response，
		// 不在这里同步 Emit full_status，避免慢 pipe/短连接阻塞响应。
		return s.buildDaemonStatus(ctx), nil
	case protocol.MethodConfigGet:
		return configToParams(s.daemon.agent.Config()), nil
	case protocol.MethodConfigSet:
		return s.handleConfigSet(ctx, req, sink)
	case protocol.MethodDaemonStop:
		go func() {
			time.Sleep(100 * time.Millisecond)
			logging.Info("agent", "daemon_stop_requested", logging.Event{"conn_id": req.ConnID})
			s.daemon.Stop()
		}()
		return map[string]string{"status": "stopping"}, nil
	default:
		return nil, protocolError{code: -32601, message: fmt.Sprintf("method not found: %s", req.Method)}
	}
}

func (s *service) handleRuntimeHello(req protocol.Request) (protocol.RuntimeHelloResult, error) {
	var params protocol.RuntimeHelloParams
	if err := decodeParams(req.Params, &params); err != nil {
		return protocol.RuntimeHelloResult{}, invalidParams(err.Error())
	}
	requestedVersion := strings.TrimSpace(params.ProtocolVersion)
	if requestedVersion != "" && requestedVersion != "0.2" {
		return protocol.RuntimeHelloResult{}, protocolError{code: -32602, message: "unsupported protocol version", data: protocol.ProtocolErrorData{Kind: "unsupported_capability", Reason: "protocol_version"}}
	}
	transport := strings.TrimSpace(params.Transport)
	if transport == "" {
		transport = "unknown"
	}
	return protocol.RuntimeHelloResult{
		ProtocolVersion: "0.2",
		RuntimeVersion:  version.Current(),
		Transport:       transport,
		Capabilities: map[string]bool{
			"agent": true, "streaming": true, "tools": true, "guard": true, "ask_user": true,
			"session": true, "multi_session": true, "handoff": true, "config": true, "memory": true, "skills": true, "mcp": true,
		},
		ContentSources: map[string]bool{"text": true, "image_path": true, "image_url": true},
		Limits:         map[string]int{"max_tool_result_bytes": maxToolResultBytes},
	}, nil
}

func (s *service) handleSendMessage(ctx context.Context, req protocol.Request, sink protocol.EventSink) (any, error) {
	var params protocol.SendMessageParams
	if err := decodeParams(req.Params, &params); err != nil {
		return nil, invalidParams(err.Error())
	}
	// 在 RPC 内预留 run，绑定本次请求的 session runtime 与附件根目录；连接随后切换 session 不得改变消息归属。
	rt, sessionID, err := s.daemon.sessions.beginRun(req.ConnID)
	if err != nil {
		return nil, protocolError{code: -32602, message: err.Error(), data: protocol.ProtocolErrorData{Kind: err.Error()}}
	}
	input, err := s.agentInputFromParams(ctx, rt.agent, params)
	if err != nil {
		s.daemon.sessions.setStatus(sessionID, sessionIdle)
		return nil, invalidParams(err.Error())
	}
	inputText := input.Text()
	if inputText == "" && len(input.Blocks) == 0 {
		s.daemon.sessions.setStatus(sessionID, sessionIdle)
		return nil, invalidParams("content is required")
	}
	go s.runAgentEvents(ctx, req.ConnID, sessionID, inputText, rt.agent.Run(ctx, input), sink)
	s.emitUserMessage(ctx, sessionID, req.ConnID, protocol.UserMessageParams{SessionID: sessionID, Parts: params.Parts})
	s.emitAgentRun(ctx, sessionID, req.ConnID, protocol.AgentRunParams{State: protocol.AgentRunRunning, Phase: protocol.AgentRunPhaseModel})
	return map[string]string{"status": "processing"}, nil
}

func (s *service) handleResumeRun(ctx context.Context, req protocol.Request, sink protocol.EventSink) (any, error) {
	rt, sessionID, err := s.daemon.sessions.beginRun(req.ConnID)
	if err != nil {
		return nil, protocolError{code: -32602, message: err.Error(), data: protocol.ProtocolErrorData{Kind: err.Error()}}
	}
	go s.runAgentEvents(ctx, req.ConnID, sessionID, "resume", rt.agent.ResumeRun(ctx), sink)
	s.emitAgentRun(ctx, sessionID, req.ConnID, protocol.AgentRunParams{State: protocol.AgentRunRunning, Phase: protocol.AgentRunPhaseModel})
	return map[string]string{"status": "processing"}, nil
}

func (s *service) runAgentEvents(ctx context.Context, connID, sessionID, inputLabel string, events <-chan agent.Event, sink protocol.EventSink) {
	// Agent 只会在状态保存 defer 完成后关闭 events；因此只能在这里把会话转为 idle，
	// 避免首轮 run 的 done 通知先到、客户端断开后被空会话清理。
	defer func() {
		// run 可能在最后一个连接离开时被取消；无论结束原因如何，都不能让
		// AskUser / GuardConfirm 的协议交互继续保留 session runtime 的引用。
		s.cancelPendingInteractions(sessionID)
		s.daemon.sessions.setStatus(sessionID, sessionIdle)
		emit(ctx, multiSink(s.daemon.sessions.sinksForSession(s.daemon, sessionID)), protocol.NotifyDaemonFullStatus, s.buildDaemonStatus(ctx))
	}()
	started := time.Now()
	compactFailed := false
	logging.Info("agent", "run_start", logging.Event{"conn_id": connID, "input_chars": len(inputLabel)})
	batcher := &streamBatcher{}
	ticker := time.NewTicker(streamBatchInterval)
	defer ticker.Stop()
	flush := func() {
		sink = multiSink(s.daemon.sessions.sinksForSession(s.daemon, sessionID))
		batcher.flush(ctx, sink)
	}
	for {
		select {
		case evt, ok := <-events:
			if !ok {
				flush()
				return
			}
			sink = multiSink(s.daemon.sessions.sinksForSession(s.daemon, sessionID))
			switch evt.Type {
			case agent.EventStream:
				s.daemon.sessions.appendStream(sessionID, evt.Content)
				if batcher.addStream(ctx, sink, evt.Content) {
					flush()
				}
			case agent.EventReasoning:
				s.daemon.sessions.appendReasoning(sessionID, evt.Content)
				if batcher.addReasoning(ctx, sink, evt.Content) {
					flush()
				}
			case agent.EventUsage:
				flush()
				speed := 0.0
				if evt.OutputTokens > 0 && evt.DurationMs > 0 {
					speed = float64(evt.OutputTokens) / (float64(evt.DurationMs) / 1000)
				}
				emit(ctx, sink, protocol.NotifyUsage, protocol.UsageParams{InputTokens: evt.InputTokens, OutputTokens: evt.OutputTokens, CachedTokens: evt.CachedTokens, ContextTokens: evt.ContextTokens, EstimatedContextTokens: evt.EstimatedContextTokens, ContextWindow: evt.ContextWindow, DurationMs: evt.DurationMs, TokensPerSec: speed})
			case agent.EventToolCall:
				flush()
				s.daemon.sessions.setPhase(sessionID, protocol.AgentRunPhaseTool)
				logging.Info("agent", "tool_call", logging.Event{"conn_id": connID, "tool": evt.ToolName, "intent": evt.ToolIntent})
				emit(ctx, sink, protocol.NotifyToolStart, protocol.ToolStartParams{ID: evt.ToolCallID, Tool: evt.ToolName, Params: evt.ToolParams, Intent: evt.ToolIntent})
			case agent.EventToolGuard:
				flush()
				emit(ctx, sink, protocol.NotifyToolGuard, protocol.ToolGuardParams{ToolCallID: evt.GuardToolCallID, Tool: evt.GuardTool, Risk: evt.GuardRisk, Decision: evt.GuardDecision, Source: evt.GuardSource, Reason: evt.GuardReason, Suggestion: evt.GuardSuggestion, ReviewCode: evt.GuardReviewCode, ReviewMessage: evt.GuardReviewMsg})
			case agent.EventToolResult:
				flush()
				display := limitToolResult(evt.ToolResult)
				logging.Info("agent", "tool_result", logging.Event{"conn_id": connID, "tool": evt.ToolName, "tool_error": evt.ToolError, "result_chars": len(evt.ToolResult), "display_truncated": display.truncated})
				emit(ctx, sink, protocol.NotifyToolEnd, protocol.ToolEndParams{ID: evt.ToolCallID, Tool: evt.ToolName, Result: display.text, Error: evt.ToolError, ResultTruncated: display.truncated, ResultBytes: display.bytes, Metadata: evt.ToolMetadata})
			case agent.EventSkillLoad:
				flush()
				emit(ctx, sink, protocol.NotifySkillLoad, protocol.SkillLoadParams{Name: evt.SkillName, Status: evt.SkillLoadStatus})
			case agent.EventSkillReview:
				flush()
				emit(ctx, sink, protocol.NotifySkillReview, protocol.SkillReviewParams{Name: evt.SkillName, Status: evt.SkillReviewStatus, Review: evt.SkillReview, Error: evt.Content})
			case agent.EventAskUser:
				flush()
				s.daemon.sessions.setPhase(sessionID, protocol.AgentRunPhaseAsk)
				s.daemon.sessions.setWaiting(sessionID, protocol.RunWaitingAsk)
				// 交互 ID 是公开协议字段，必须保持 opaque，不能包含 daemon 内部连接标识。
				askID := uuid.NewString()
				params := protocol.AskUserParams{Question: evt.Question, Options: evt.Options, ID: askID, SessionID: sessionID, AllowCustom: evt.AllowCustom}
				if evt.Reply != nil {
					s.daemon.sessions.withAttachedClients(sessionID, func() {
						s.pendingAsks.Store(askID, pendingInteraction{sessionID: sessionID, ownerID: connID, reply: evt.Reply, ask: &params})
					})
				}
				s.emitAskUser(ctx, sessionID, connID, params)
			case agent.EventGuardConfirm:
				flush()
				s.daemon.sessions.setPhase(sessionID, protocol.AgentRunPhaseGuard)
				s.daemon.sessions.setWaiting(sessionID, protocol.RunWaitingGuard)
				// Guard 确认 ID 同样是公开协议字段，只能作为 opaque token 使用。
				guardID := uuid.NewString()
				params := protocol.GuardConfirmParams{ID: guardID, ToolCallID: evt.GuardToolCallID, Tool: evt.GuardTool, Params: evt.GuardParams, Risk: evt.GuardRisk, Reason: evt.GuardReason, Suggestion: evt.GuardSuggestion, ReviewCode: evt.GuardReviewCode, ReviewMessage: evt.GuardReviewMsg, SessionID: sessionID}
				if evt.Reply != nil {
					s.daemon.sessions.withAttachedClients(sessionID, func() {
						s.pendingGuards.Store(guardID, pendingInteraction{sessionID: sessionID, ownerID: connID, reply: evt.Reply, guard: &params})
					})
				}
				s.emitGuardConfirm(ctx, sessionID, connID, params)
			case agent.EventStatus:
				flush()
				switch evt.Status {
				case agent.StatusCompactRunning:
					s.daemon.sessions.setPhase(sessionID, protocol.AgentRunPhaseCompact)
					s.daemon.sessions.setStatus(sessionID, sessionCompacting)
					running := true
					emit(ctx, sink, protocol.NotifyCompactResult, protocol.CompactResult{Running: &running})
				case agent.StatusCompactDone:
					running := false
					emit(ctx, sink, protocol.NotifyCompactResult, protocol.CompactResult{Running: &running})
				case agent.StatusCompactError:
					running := false
					emit(ctx, sink, protocol.NotifyCompactResult, protocol.CompactResult{Running: &running, Error: evt.Content})
					// 压缩错误已有专用通知；仍需继续消费 events，等状态保存完成后再转 idle。
					compactFailed = true
					continue
				case agent.StatusWaitingLLM:
					s.daemon.sessions.setPhase(sessionID, protocol.AgentRunPhaseModel)
					s.daemon.sessions.setStatus(sessionID, sessionRunning)
					ownerID := s.daemon.sessions.runOwner(sessionID)
					if ownerID == "" {
						ownerID = connID
					}
					s.emitAgentRun(ctx, sessionID, ownerID, protocol.AgentRunParams{State: protocol.AgentRunRunning, Phase: protocol.AgentRunPhaseModel})
				case agent.StatusLLMRetrying:
					emit(ctx, sink, protocol.NotifyAgentRun, protocol.AgentRunParams{State: protocol.AgentRunRetrying, Phase: protocol.AgentRunPhaseModel, Message: evt.Content, Attempt: evt.Attempt, MaxAttempts: evt.MaxAttempts, DelayMs: evt.DelayMs, Error: protocolModelError(evt.ModelError)})
				case agent.StatusDone:
					logging.Info("agent", "run_done", logging.Event{"conn_id": connID, "duration_ms": time.Since(started).Milliseconds()})
					emit(ctx, sink, protocol.NotifyAgentRun, protocol.AgentRunParams{State: protocol.AgentRunDone})
					emit(ctx, sink, protocol.NotifyDaemonFullStatus, s.buildDaemonStatus(ctx))
				default:
					if evt.Error {
						if compactFailed {
							// 压缩失败已由 CompactResult 展示，避免重复的通用模型错误。
							continue
						}
						resumeAvailable := evt.ResumeAvailable
						failure := fmt.Errorf("agent run failed")
						fields := logging.Event{"conn_id": connID, "duration_ms": time.Since(started).Milliseconds()}
						if evt.RunError != nil {
							failure = fmt.Errorf("agent run precondition failed: %s", evt.RunError.Kind)
							fields["run_error_kind"] = evt.RunError.Kind
							if evt.RunError.ModelRef != "" {
								fields["model_ref"] = evt.RunError.ModelRef
							}
						}
						logging.Error("agent", "run_failed", failure, fields)
						state := protocol.AgentRunFailed
						if evt.ModelError != nil && evt.ModelError.Kind == model.ModelErrorCancelled {
							state = protocol.AgentRunCancelled
						}
						emit(ctx, sink, protocol.NotifyAgentRun, protocol.AgentRunParams{State: state, Phase: protocol.AgentRunPhaseModel, Message: evt.Content, Error: protocolModelError(evt.ModelError), RunError: protocolRunError(evt.RunError), ResumeAvailable: resumeAvailable})
						emit(ctx, sink, protocol.NotifyDaemonFullStatus, s.buildDaemonStatus(ctx))
					}
				}
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (s *service) broadcastSessionState(ctx context.Context, sessionID string) {
	s.daemon.broadcastSessionState(ctx, sessionID)
}

func (s *service) emitAgentRun(ctx context.Context, sessionID, ownerID string, params protocol.AgentRunParams) {
	for _, targetConnID := range s.daemon.sessions.connIDsForSession(sessionID) {
		p := params
		p.CanControl = targetConnID == ownerID
		emit(ctx, s.daemon.sinkFor(targetConnID, nil), protocol.NotifyAgentRun, p)
	}
}

func (s *service) emitUserMessage(ctx context.Context, sessionID, ownerID string, params protocol.UserMessageParams) {
	for _, targetConnID := range s.daemon.sessions.connIDsForSession(sessionID) {
		if targetConnID == ownerID {
			continue
		}
		emit(ctx, s.daemon.sinkFor(targetConnID, nil), protocol.NotifySessionUserMessage, params)
	}
}

func (s *service) emitAskUser(ctx context.Context, sessionID, ownerID string, params protocol.AskUserParams) {
	for _, targetConnID := range s.daemon.sessions.connIDsForSession(sessionID) {
		p := params
		p.CanReply = targetConnID == ownerID
		emit(ctx, s.daemon.sinkFor(targetConnID, nil), protocol.NotifyAskUser, p)
	}
}

func (s *service) emitGuardConfirm(ctx context.Context, sessionID, ownerID string, params protocol.GuardConfirmParams) {
	for _, targetConnID := range s.daemon.sessions.connIDsForSession(sessionID) {
		p := params
		p.CanReply = targetConnID == ownerID
		emit(ctx, s.daemon.sinkFor(targetConnID, nil), protocol.NotifyGuardConfirm, p)
	}
}

func (s *service) emitInteractionResolved(ctx context.Context, sessionID, id string) {
	sink := multiSink(s.daemon.sessions.sinksForSession(s.daemon, sessionID))
	emit(ctx, sink, protocol.NotifyInteractionResolved, protocol.InteractionResolvedParams{ID: id, SessionID: sessionID})
}

func (s *service) onClientDetached(ctx context.Context, connID, sessionID string) {
	if sessionID == "" {
		return
	}
	// owner 断开时，等待中的 ask/guard 会重新发给仍 attached 的客户端；daemon 只改变可回复权限，不引入 host/guest 概念。
	s.pendingAsks.Range(func(key, value any) bool {
		pending := value.(pendingInteraction)
		if pending.sessionID != sessionID || pending.ownerID != connID || pending.ask == nil {
			return true
		}
		for _, targetConnID := range s.daemon.sessions.connIDsForSession(sessionID) {
			p := *pending.ask
			p.CanReply = true
			emit(ctx, s.daemon.sinkFor(targetConnID, nil), protocol.NotifyAskUser, p)
		}
		return true
	})
	s.pendingGuards.Range(func(key, value any) bool {
		pending := value.(pendingInteraction)
		if pending.sessionID != sessionID || pending.ownerID != connID || pending.guard == nil {
			return true
		}
		for _, targetConnID := range s.daemon.sessions.connIDsForSession(sessionID) {
			p := *pending.guard
			p.CanReply = true
			emit(ctx, s.daemon.sinkFor(targetConnID, nil), protocol.NotifyGuardConfirm, p)
		}
		return true
	})
}

// cancelPendingInteractions 删除已失去所有 attached client 的 session 的交互记录。
// Agent run 会由取消 context 唤醒，因此这里不能关闭 reply，避免把零值误当成用户回复。
func (s *service) cancelPendingInteractions(sessionID string) {
	if sessionID == "" {
		return
	}
	s.pendingAsks.Range(func(key, value any) bool {
		if pending := value.(pendingInteraction); pending.sessionID == sessionID {
			s.pendingAsks.Delete(key)
		}
		return true
	})
	s.pendingGuards.Range(func(key, value any) bool {
		if pending := value.(pendingInteraction); pending.sessionID == sessionID {
			s.pendingGuards.Delete(key)
		}
		return true
	})
}

func (s *service) handleAskReply(req protocol.Request) (any, error) {
	var params protocol.AskUserReply
	if err := decodeParams(req.Params, &params); err != nil {
		return nil, invalidParams(err.Error())
	}
	pending, err := s.claimInteractionReply(req.ConnID, params.ID, &s.pendingAsks, "ask session not found or expired")
	if err != nil {
		return nil, err
	}
	pending.reply <- params.Answer
	close(pending.reply)
	s.emitInteractionResolved(context.Background(), pending.sessionID, params.ID)
	return map[string]string{"status": "ok"}, nil
}

func (s *service) handleGuardReply(req protocol.Request) (any, error) {
	var params protocol.GuardReplyParams
	if err := decodeParams(req.Params, &params); err != nil {
		return nil, invalidParams(err.Error())
	}
	pending, err := s.claimInteractionReply(req.ConnID, params.ID, &s.pendingGuards, "guard confirmation not found or expired")
	if err != nil {
		return nil, err
	}
	pending.reply <- params.Decision
	close(pending.reply)
	s.emitInteractionResolved(context.Background(), pending.sessionID, params.ID)
	return map[string]string{"status": "ok"}, nil
}

func (s *service) claimInteractionReply(connID, id string, store *sync.Map, notFound string) (pendingInteraction, error) {
	val, ok := store.Load(id)
	if !ok {
		return pendingInteraction{}, protocolError{code: -32601, message: notFound}
	}
	pending := val.(pendingInteraction)
	if err := s.ensureInteractionReplyAllowed(connID, pending); err != nil {
		return pendingInteraction{}, err
	}
	if connID != pending.ownerID {
		// owner 已离线后的 Handoff 接手：回复者成为后续 run 控制者，Esc/后续通知才会落到正确窗口。
		s.daemon.sessions.setRunOwner(pending.sessionID, connID)
	}
	val, ok = store.LoadAndDelete(id)
	if !ok {
		return pendingInteraction{}, protocolError{code: -32601, message: notFound}
	}
	return val.(pendingInteraction), nil
}

func (s *service) ensureInteractionReplyAllowed(connID string, pending pendingInteraction) error {
	if connID == pending.ownerID {
		return nil
	}
	if !s.daemon.sessions.isClientAttached(connID, pending.sessionID) {
		return protocolError{code: -32602, message: "reply client is not attached to the waiting session", data: protocol.ProtocolErrorData{Kind: "session_required"}}
	}
	if s.daemon.sessions.isClientAttached(pending.ownerID, pending.sessionID) {
		return protocolError{code: -32602, message: "interaction reply is owned by another client", data: protocol.ProtocolErrorData{Kind: "session_busy"}}
	}
	return nil
}

func (s *service) handleMemoryList(ctx context.Context, sink protocol.EventSink) (any, error) {
	memories, err := s.daemon.agent.ListMemory(ctx)
	if err != nil {
		return nil, protocolError{code: -32603, message: err.Error()}
	}
	result := protocol.MemoryListResult{Memories: make([]protocol.MemoryItem, 0, len(memories))}
	for _, m := range memories {
		result.Memories = append(result.Memories, protocol.MemoryItem{ID: m.ID, Content: m.Content, Kind: m.Kind, Tags: m.Tags, Priority: m.Priority, IsCore: m.IsCore})
	}
	return result, nil
}

func (s *service) handleMemoryDelete(ctx context.Context, req protocol.Request, sink protocol.EventSink) (any, error) {
	var params protocol.MemoryDeleteParams
	if err := decodeParams(req.Params, &params); err != nil {
		return nil, invalidParams(err.Error())
	}
	deleted, err := s.daemon.agent.DeleteMemory(ctx, params.ID)
	if err != nil {
		return nil, protocolError{code: -32603, message: err.Error()}
	}
	if err := s.emitMemoryList(ctx, sink); err != nil {
		return nil, err
	}
	return protocol.MemoryDeleteResult{Deleted: deleted}, nil
}

func (s *service) handleMemoryClear(ctx context.Context, sink protocol.EventSink) (any, error) {
	deleted, err := s.daemon.agent.ClearMemory(ctx)
	if err != nil {
		return nil, protocolError{code: -32603, message: err.Error()}
	}
	if err := s.emitMemoryList(ctx, sink); err != nil {
		return nil, err
	}
	return protocol.MemoryClearResult{DeletedCount: deleted}, nil
}

func (s *service) emitMemoryList(ctx context.Context, sink protocol.EventSink) error {
	memories, err := s.daemon.agent.ListMemory(ctx)
	if err != nil {
		return protocolError{code: -32603, message: err.Error()}
	}
	result := protocol.MemoryListResult{Memories: make([]protocol.MemoryItem, 0, len(memories))}
	for _, m := range memories {
		result.Memories = append(result.Memories, protocol.MemoryItem{ID: m.ID, Content: m.Content, Kind: m.Kind, Tags: m.Tags, Priority: m.Priority, IsCore: m.IsCore})
	}
	emit(ctx, sink, protocol.NotifyMemoryState, result)
	return nil
}

func toolSummaryPayload(summary memory.ToolSummary) *protocol.ToolSummaryPayload {
	summary = summary.Normalize()
	if summary.Empty() {
		return nil
	}
	out := &protocol.ToolSummaryPayload{Total: summary.Total, Success: summary.Success, Failed: summary.Failed, Omitted: summary.Omitted}
	for _, item := range summary.Changes {
		out.Changes = append(out.Changes, protocol.ToolChangeItem{Tool: item.Name, Count: item.Count})
	}
	for _, item := range summary.Failures {
		out.Failures = append(out.Failures, protocol.ToolSummaryItem{Tool: item.Name, Status: item.Status, Summary: item.Summary})
	}
	for _, item := range summary.Recent {
		out.Recent = append(out.Recent, protocol.ToolSummaryItem{Tool: item.Name, Status: item.Status, Summary: item.Summary})
	}
	return out
}

func (s *service) handleCompact(ctx context.Context, req protocol.Request, sink protocol.EventSink) (any, error) {
	rt, sessionID, err := s.daemon.sessions.beginRun(req.ConnID)
	if err != nil {
		return nil, err
	}
	// compact 会重写当前 session 的 working state，必须像普通 run 一样独占 session，不能和 LLM/tool run 并发。
	s.daemon.sessions.setPhase(sessionID, protocol.AgentRunPhaseCompact)
	s.daemon.sessions.setStatus(sessionID, sessionCompacting)
	defer s.daemon.sessions.setStatus(sessionID, sessionIdle)
	before, after, contextWindow, turnsCompressed, truncated, err := rt.agent.Compact(ctx)
	if err != nil {
		return nil, protocolError{code: -32603, message: err.Error()}
	}
	result := protocol.CompactResult{BeforeTokens: before, AfterTokens: after, ContextWindow: contextWindow, TurnsCompressed: turnsCompressed, SummaryTokens: (before - after) / 2, TruncatedOutputs: truncated, Noop: turnsCompressed == 0}
	emit(ctx, sink, protocol.NotifyCompactResult, result)
	return map[string]string{"status": "ok"}, nil
}

func (s *service) handleUsage(ctx context.Context) protocol.UsageResult {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	week := today.AddDate(0, 0, -7)
	month := today.AddDate(0, -1, 0)
	result := protocol.UsageResult{}
	if sum, err := s.daemon.agent.UsageSummary(ctx, today); err == nil && sum != nil {
		result.Today = periodFromSummary(sum)
	}
	if sum, err := s.daemon.agent.UsageSummary(ctx, week); err == nil && sum != nil {
		result.Week = periodFromSummary(sum)
	}
	if sum, err := s.daemon.agent.UsageSummary(ctx, month); err == nil && sum != nil {
		result.Month = periodFromSummary(sum)
	}
	return result
}

func (s *service) handleAttachmentStatus(req protocol.Request) (protocol.AttachmentStatusResult, error) {
	rt, sessionID, err := s.requireSession(req.ConnID)
	if err != nil {
		return protocol.AttachmentStatusResult{}, err
	}
	root, bytes, count, err := rt.agent.AttachmentStatus()
	if err != nil {
		return protocol.AttachmentStatusResult{}, protocolError{code: -32603, message: err.Error()}
	}
	return protocol.AttachmentStatusResult{SessionID: sessionID, Root: root, Bytes: bytes, Count: count}, nil
}

func (s *service) handleAttachmentClear(req protocol.Request) (protocol.AttachmentClearResult, error) {
	rt, sessionID, err := s.requireSession(req.ConnID)
	if err != nil {
		return protocol.AttachmentClearResult{}, err
	}
	root, removedBytes, removedCount, bytes, count, err := rt.agent.ClearAttachments()
	if err != nil {
		return protocol.AttachmentClearResult{}, protocolError{code: -32603, message: err.Error()}
	}
	return protocol.AttachmentClearResult{SessionID: sessionID, Root: root, BytesRemoved: removedBytes, CountRemoved: removedCount, Bytes: bytes, Count: count}, nil
}

func (s *service) handleConfigSet(ctx context.Context, req protocol.Request, sink protocol.EventSink) (any, error) {
	var params protocol.ConfigSetParams
	if err := decodeParams(req.Params, &params); err != nil {
		return nil, invalidParams(err.Error())
	}
	updated, err := s.daemon.agent.UpdateConfig(agent.ConfigSetParams{Action: params.Action, ModelRef: params.ModelRef, ActiveModel: params.ActiveModel, APIKey: params.APIKey, DeleteAPIKey: params.DeleteAPIKey, Locale: params.Locale, Theme: params.Theme, GuardMode: params.GuardMode, Workspace: params.Workspace, Model: agent.ConfigModel{Provider: params.Model.Provider, Protocol: config.ModelProtocol(params.Model.Protocol), Model: params.Model.Model, BaseURL: params.Model.BaseURL, ContextWindow: params.Model.ContextWindow, MaxOutputTokens: params.Model.MaxOutputTokens, Strengths: params.Model.Strengths, SubtaskFor: params.Model.SubtaskFor, Reasoning: params.Model.Reasoning}})
	if err != nil {
		logging.Error("config", "update_failed", err, logging.Event{"action": params.Action, "model_ref": params.ModelRef, "active_model": params.ActiveModel})
		return nil, invalidParams(err.Error())
	}
	logging.Info("config", "update_success", logging.Event{"action": params.Action, "model_ref": params.ModelRef, "active_model": params.ActiveModel})
	result := configToParams(updated)
	s.daemon.BroadcastToAll(ctx, protocol.NotifyConfigState, result)
	s.daemon.BroadcastToAll(ctx, protocol.NotifyDaemonFullStatus, s.buildDaemonStatus(ctx))
	return result, nil
}

func (s *service) buildDaemonStatus(ctx context.Context) protocol.DaemonStatusParams {
	s.ensureConfigLoaded()
	params := protocol.DaemonStatusParams{PID: os.Getpid(), AgentStatus: "idle", Uptime: s.daemon.Uptime().Truncate(time.Second).String(), Connections: s.daemon.ConnectionCount()}
	for _, tr := range s.daemon.transports {
		if tcpTransport, ok := tr.(interface{ Endpoint() string }); ok && tr.Name() == "tcp" {
			params.TCPEndpoint = tcpTransport.Endpoint()
			break
		}
	}
	if s.daemon.agent != nil {
		activeMem, coreMem, queuedMem := s.daemon.agent.MemoryStats(ctx)
		params.Memory = &protocol.MemoryStats{Active: activeMem, Core: coreMem, Queued: queuedMem}
		active, completed, lastID := s.daemon.agent.SessionStats(ctx)
		params.Sessions = &protocol.SessionStats{Active: active, Completed: completed, LastID: lastID}
		if sum, err := s.daemon.agent.UsageSummary(ctx, time.Now().Add(-24*time.Hour)); err == nil && sum != nil {
			usage := periodFromSummary(sum)
			params.UsageToday = &usage
		}
	}
	if s.daemon.agent != nil {
		rt := s.daemon.agent.DefaultModelRuntime()
		if params.Provider == "" {
			params.Provider = rt.Provider
		}
		if params.Model == "" {
			params.Model = rt.Model
		}
		params.ContextWindow = rt.ContextWindow
	}
	return params
}

func (s *service) ensureConfigLoaded() {
	if s == nil || s.daemon == nil || s.daemon.agent == nil {
		return
	}
	if _, err := s.daemon.agent.ReloadConfigFromDiskIfNeeded(); err != nil {
		logging.Error("config", "reload_skipped", err, nil)
	}
}

func emit(ctx context.Context, sink protocol.EventSink, method string, params any) {
	if sink != nil {
		_ = sink.Emit(ctx, protocol.Event{Method: method, Params: params})
	}
}

func decodeParams(src any, dst any) error {
	data, err := json.Marshal(src)
	if err != nil {
		return err
	}
	if len(data) == 0 || string(data) == "null" {
		return fmt.Errorf("missing params")
	}
	return json.Unmarshal(data, dst)
}

func periodFromSummary(sum *memory.UsageSummary) protocol.UsagePeriod {
	return protocol.UsagePeriod{InputTokens: sum.InputTokens, OutputTokens: sum.OutputTokens, Requests: sum.Requests}
}

func configToParams(cfg *config.Config) protocol.ConfigParams {
	out := protocol.ConfigParams{ActiveModel: cfg.ActiveModel, Locale: cfg.UI.Locale, Theme: cfg.UI.Theme, GuardMode: cfg.Guard.ModeOrDefault(), Workspace: cfg.Guard.Workspace}
	for _, mc := range cfg.Models {
		out.Models = append(out.Models, protocol.ConfigModel{Provider: mc.Provider, Protocol: string(mc.ProtocolOrDefault()), Model: mc.Model, BaseURL: mc.BaseURL, ContextWindow: mc.ContextWindow, MaxOutputTokens: mc.MaxOutputTokens, Strengths: mc.Strengths, SubtaskFor: mc.SubtaskFor, Reasoning: mc.Reasoning, HasAPIKey: mc.APIKey != ""})
	}
	return out
}

type toolDisplay struct {
	text      string
	truncated bool
	bytes     int
}

func limitToolResult(s string) toolDisplay {
	if len(s) <= maxToolResultBytes {
		return toolDisplay{text: s, bytes: len(s)}
	}
	return toolDisplay{text: truncateUTF8(s, maxToolResultBytes), truncated: true, bytes: len(s)}
}

func truncateUTF8(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	end := 0
	for i := range s {
		if i > maxBytes {
			break
		}
		end = i
	}
	if end == 0 {
		return ""
	}
	return s[:end]
}

type protocolError struct {
	code    int
	message string
	data    any
}

func (e protocolError) Error() string { return e.message }
func (e protocolError) Code() int     { return e.code }
func (e protocolError) Data() any {
	if e.data != nil {
		return e.data
	}
	switch e.code {
	case -32601:
		return protocol.ProtocolErrorData{Kind: "unsupported_method"}
	case -32602:
		return protocol.ProtocolErrorData{Kind: "invalid_request"}
	default:
		return protocol.ProtocolErrorData{Kind: "internal_error"}
	}
}

func invalidParams(message string) protocolError {
	return protocolError{code: -32602, message: message, data: protocol.ProtocolErrorData{Kind: "invalid_request"}}
}

func protocolRunError(err *agent.RunError) *protocol.RunError {
	if err == nil {
		return nil
	}
	return &protocol.RunError{
		Kind:     protocol.RunErrorKind(err.Kind),
		ModelRef: err.ModelRef,
	}
}

func protocolModelError(err *model.ModelError) *protocol.ModelError {
	if err == nil {
		return nil
	}
	return &protocol.ModelError{
		Kind:       protocol.ModelErrorKind(err.Kind),
		Message:    err.Message,
		StatusCode: err.StatusCode,
		Code:       err.Code,
		Type:       err.Type,
		Provider:   err.Provider,
		Model:      err.Model,
	}
}
