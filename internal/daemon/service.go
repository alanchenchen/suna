package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/alanchenchen/suna/internal/agent"
	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/logging"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/skill"
)

const maxToolResultBytes = 16 * 1024

type service struct {
	daemon *Daemon

	// AskUser / GuardConfirm 会阻塞 agent loop，这里按事件 ID 保存 reply channel，等待客户端回传。
	pendingAsks   sync.Map
	pendingGuards sync.Map
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
	logging.Info("transport", "request", logging.Event{"conn_id": req.ConnID, "method": req.Method, "request_id": req.ID})
	sink = s.daemon.sinkFor(req.ConnID, sink)
	s.ensureConfigLoaded()
	if skill.IsProtocolMethod(req.Method) {
		if s.daemon.agent.Skills() == nil {
			return nil, protocolError{code: -32603, message: "skill runtime is not initialized"}
		}
		return s.daemon.agent.Skills().HandleProtocol(ctx, req, sink)
	}
	switch req.Method {
	case protocol.MethodSendMessage:
		return s.handleSendMessage(ctx, req, sink)
	case protocol.MethodCancel:
		s.daemon.agent.CancelCurrentRun()
		return map[string]string{"status": "cancelled"}, nil
	case protocol.MethodAskReply:
		return s.handleAskReply(req)
	case protocol.MethodGuardReply:
		return s.handleGuardReply(req)
	case protocol.MethodMemoryList:
		return s.handleMemoryList(ctx, sink)
	case protocol.MethodSessionNew:
		s.daemon.agent.NewSession()
		_ = sink.Emit(ctx, protocol.Event{Method: protocol.NotifyDaemonFullStatus, Params: s.buildDaemonStatus(ctx)})
		return map[string]string{"status": "ok"}, nil
	case protocol.MethodSessionRestore:
		return s.handleSessionRestore(ctx, sink)
	case protocol.MethodCompact:
		return s.handleCompact(ctx, sink)
	case protocol.MethodUsage:
		return s.handleUsage(ctx), nil
	case protocol.MethodAttachmentStatus:
		return s.handleAttachmentStatus()
	case protocol.MethodAttachmentClear:
		return s.handleAttachmentClear()
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

func (s *service) handleSendMessage(ctx context.Context, req protocol.Request, sink protocol.EventSink) (any, error) {
	var params protocol.SendMessageParams
	if err := decodeParams(req.Params, &params); err != nil {
		return nil, invalidParams(err.Error())
	}
	input, err := s.agentInputFromParams(ctx, params)
	if err != nil {
		return nil, invalidParams(err.Error())
	}
	inputText := input.Text()
	if inputText == "" && len(input.Blocks) == 0 {
		return nil, invalidParams("content is required")
	}
	go s.runAgent(ctx, req.ConnID, inputText, input, sink)
	return map[string]string{"status": "processing"}, nil
}

func (s *service) runAgent(ctx context.Context, connID, inputText string, input agent.Input, sink protocol.EventSink) {
	started := time.Now()
	logging.Info("agent", "run_start", logging.Event{"conn_id": connID, "input_chars": len(inputText)})
	events := s.daemon.agent.Run(ctx, input)
	batcher := &streamBatcher{}
	ticker := time.NewTicker(streamBatchInterval)
	defer ticker.Stop()
	flush := func() {
		sink = s.daemon.sinkFor(connID, sink)
		batcher.flush(ctx, sink)
	}
	for {
		select {
		case evt, ok := <-events:
			if !ok {
				flush()
				return
			}
			sink = s.daemon.sinkFor(connID, sink)
			switch evt.Type {
			case agent.EventStream:
				if batcher.addStream(ctx, sink, evt.Content) {
					flush()
				}
			case agent.EventReasoning:
				if batcher.addReasoning(ctx, sink, evt.Content) {
					flush()
				}
			case agent.EventUsage:
				flush()
				speed := 0.0
				if evt.OutputTokens > 0 && evt.DurationMs > 0 {
					speed = float64(evt.OutputTokens) / (float64(evt.DurationMs) / 1000)
				}
				emit(ctx, sink, protocol.NotifyUsage, protocol.UsageParams{InputTokens: evt.InputTokens, OutputTokens: evt.OutputTokens, CachedTokens: evt.CachedTokens, ContextTokens: evt.ContextTokens, ContextWindow: evt.ContextWindow, DurationMs: evt.DurationMs, TokensPerSec: speed})
			case agent.EventToolCall:
				flush()
				logging.Info("agent", "tool_call", logging.Event{"conn_id": connID, "tool": evt.ToolName, "intent": evt.ToolIntent})
				emit(ctx, sink, protocol.NotifyToolStart, protocol.ToolStartParams{ID: evt.ToolCallID, Tool: evt.ToolName, Params: evt.ToolParams, Intent: evt.ToolIntent})
			case agent.EventToolGuard:
				flush()
				emit(ctx, sink, protocol.NotifyToolGuard, protocol.ToolGuardParams{ToolCallID: evt.GuardToolCallID, Tool: evt.GuardTool, Risk: evt.GuardRisk, Decision: evt.GuardDecision, Source: evt.GuardSource, Reason: evt.GuardReason, Suggestion: evt.GuardSuggestion})
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
				askID := connID + "_" + fmt.Sprintf("%d", time.Now().UnixNano())
				if evt.Reply != nil {
					s.pendingAsks.Store(askID, evt.Reply)
				}
				emit(ctx, sink, protocol.NotifyAskUser, protocol.AskUserParams{Question: evt.Question, Options: evt.Options, ID: askID, AllowCustom: evt.AllowCustom})
			case agent.EventGuardConfirm:
				flush()
				guardID := connID + "_guard_" + fmt.Sprintf("%d", time.Now().UnixNano())
				if evt.Reply != nil {
					s.pendingGuards.Store(guardID, evt.Reply)
				}
				emit(ctx, sink, protocol.NotifyGuardConfirm, protocol.GuardConfirmParams{ID: guardID, ToolCallID: evt.GuardToolCallID, Tool: evt.GuardTool, Params: evt.GuardParams, Risk: evt.GuardRisk, Reason: evt.GuardReason, Suggestion: evt.GuardSuggestion})
			case agent.EventStatus:
				flush()
				if strings.HasPrefix(evt.Content, "error:") || evt.Content == "cancelled" {
					logging.Error("agent", "run_failed", fmt.Errorf("%s", evt.Content), logging.Event{"conn_id": connID, "duration_ms": time.Since(started).Milliseconds()})
					emit(ctx, sink, protocol.NotifyStream, protocol.StreamParams{Chunk: evt.Content, Done: true})
					emit(ctx, sink, protocol.NotifyDaemonFullStatus, s.buildDaemonStatus(ctx))
				} else if evt.Content == "done" {
					logging.Info("agent", "run_done", logging.Event{"conn_id": connID, "duration_ms": time.Since(started).Milliseconds()})
					emit(ctx, sink, protocol.NotifyStream, protocol.StreamParams{Done: true, ContextWindow: evt.ContextWindow})
					emit(ctx, sink, protocol.NotifyDaemonFullStatus, s.buildDaemonStatus(ctx))
				}
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (s *service) handleAskReply(req protocol.Request) (any, error) {
	var params protocol.AskUserReply
	if err := decodeParams(req.Params, &params); err != nil {
		return nil, invalidParams(err.Error())
	}
	val, ok := s.pendingAsks.LoadAndDelete(params.ID)
	if !ok {
		return nil, protocolError{code: -32601, message: "ask session not found or expired"}
	}
	replyCh := val.(chan string)
	replyCh <- params.Answer
	close(replyCh)
	return map[string]string{"status": "ok"}, nil
}

func (s *service) handleGuardReply(req protocol.Request) (any, error) {
	var params protocol.GuardReplyParams
	if err := decodeParams(req.Params, &params); err != nil {
		return nil, invalidParams(err.Error())
	}
	val, ok := s.pendingGuards.LoadAndDelete(params.ID)
	if !ok {
		return nil, protocolError{code: -32601, message: "guard confirmation not found or expired"}
	}
	replyCh := val.(chan string)
	replyCh <- params.Decision
	close(replyCh)
	return map[string]string{"status": "ok"}, nil
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
	emit(ctx, sink, protocol.NotifyMemoryListResult, result)
	return map[string]string{"status": "ok"}, nil
}

func (s *service) handleSessionRestore(ctx context.Context, sink protocol.EventSink) (any, error) {
	count := s.daemon.agent.RestoreSession(ctx)
	if count > 0 {
		for _, m := range s.daemon.agent.WorkingMessages() {
			content := m.Text()
			if content == "" {
				continue
			}
			switch m.Role {
			case "user":
				emit(ctx, sink, protocol.NotifySessionRestoreMsg, map[string]string{"role": "user", "content": content})
			case "assistant":
				emit(ctx, sink, protocol.NotifySessionRestoreMsg, map[string]string{"role": "assistant", "content": content})
			}
		}
		if summary := s.daemon.agent.RestoreToolSummary(ctx); summary != "" {
			emit(ctx, sink, protocol.NotifySessionRestoreMsg, map[string]string{"role": "restore_summary", "content": summary})
		}
	}
	if input := s.daemon.agent.ConsumeResumeInput(); input != "" {
		emit(ctx, sink, protocol.NotifySessionRestoreInput, map[string]string{"content": input})
	}
	return map[string]int{"messages": count}, nil
}

func (s *service) handleCompact(ctx context.Context, sink protocol.EventSink) (any, error) {
	before, after, contextWindow, turnsCompressed, truncated, err := s.daemon.agent.Compact(ctx)
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

func (s *service) handleAttachmentStatus() (protocol.AttachmentStatusResult, error) {
	root, bytes, count, err := s.daemon.agent.AttachmentStatus()
	if err != nil {
		return protocol.AttachmentStatusResult{}, protocolError{code: -32603, message: err.Error()}
	}
	return protocol.AttachmentStatusResult{Root: root, Bytes: bytes, Count: count}, nil
}

func (s *service) handleAttachmentClear() (protocol.AttachmentClearResult, error) {
	root, removedBytes, removedCount, bytes, count, err := s.daemon.agent.ClearAttachments()
	if err != nil {
		return protocol.AttachmentClearResult{}, protocolError{code: -32603, message: err.Error()}
	}
	return protocol.AttachmentClearResult{Root: root, BytesRemoved: removedBytes, CountRemoved: removedCount, Bytes: bytes, Count: count}, nil
}

func (s *service) handleConfigSet(ctx context.Context, req protocol.Request, sink protocol.EventSink) (any, error) {
	var params protocol.ConfigSetParams
	if err := decodeParams(req.Params, &params); err != nil {
		return nil, invalidParams(err.Error())
	}
	updated, err := s.daemon.agent.UpdateConfig(agent.ConfigSetParams{Action: params.Action, ModelRef: params.ModelRef, ActiveModel: params.ActiveModel, APIKey: params.APIKey, DeleteAPIKey: params.DeleteAPIKey, Locale: params.Locale, Theme: params.Theme, GuardMode: params.GuardMode, Workspace: params.Workspace, Model: agent.ConfigModel{Provider: params.Model.Provider, Model: params.Model.Model, BaseURL: params.Model.BaseURL, ContextWindow: params.Model.ContextWindow, Strengths: params.Model.Strengths, Reasoning: params.Model.Reasoning}})
	if err != nil {
		logging.Error("config", "update_failed", err, logging.Event{"action": params.Action, "model_ref": params.ModelRef, "active_model": params.ActiveModel})
		return nil, invalidParams(err.Error())
	}
	logging.Info("config", "update_success", logging.Event{"action": params.Action, "model_ref": params.ModelRef, "active_model": params.ActiveModel})
	result := configToParams(updated)
	emit(ctx, sink, protocol.NotifyConfigState, result)
	emit(ctx, sink, protocol.NotifyDaemonFullStatus, s.buildDaemonStatus(ctx))
	return result, nil
}

func (s *service) buildDaemonStatus(ctx context.Context) protocol.DaemonStatusParams {
	s.ensureConfigLoaded()
	params := protocol.DaemonStatusParams{PID: os.Getpid(), AgentStatus: "idle", Uptime: s.daemon.Uptime().Truncate(time.Second).String(), Connections: s.daemon.ConnectionCount()}
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
		rt := s.daemon.agent.ActiveModelRuntime()
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
		out.Models = append(out.Models, protocol.ConfigModel{Provider: mc.Provider, Model: mc.Model, BaseURL: mc.BaseURL, ContextWindow: mc.ContextWindow, Strengths: mc.Strengths, Reasoning: mc.Reasoning, HasAPIKey: mc.APIKey != ""})
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
}

func (e protocolError) Error() string { return e.message }
func (e protocolError) Code() int     { return e.code }

func invalidParams(message string) protocolError {
	return protocolError{code: -32602, message: message}
}
