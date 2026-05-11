package ipc

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/core"
	"github.com/alanchenchen/suna/internal/memory"
)

/*
Server Daemon 端 JSON-RPC 2.0 服务器。

职责：
  - 接收 TUI 的 JSON-RPC 请求 → 路由到 Agent → 返回响应
  - 将 Agent 事件流（LLM 输出、工具状态）推送给发起请求的 Conn
  - 广播通知（感知事件、记忆更新）到所有 Conn
  - AskUser 通过 pending ask map 跨请求协调回复

关键设计：
  - agent.stream 只推送给发起请求的 Conn（1:1）
  - perception.event 广播到所有 Conn（1:N）
  - AskUser: Agent 阻塞在 Reply channel → TUI 通过 agent.askReply 回传
*/
type Server struct {
	agent  *core.Agent
	daemon DaemonAPI

	// activeConn 当前正在接收 Agent 事件的连接
	// 一次只有一个连接可以驱动 Agent Loop
	activeConn atomic.Pointer[Conn]

	// pendingAsks 等待回复的 AskUser 请求
	// key = askID, value = Reply channel
	pendingAsks sync.Map

	mu sync.Mutex
}

// DaemonAPI Daemon 暴露给 Server 的接口
type DaemonAPI interface {
	Agent() *core.Agent
	ConnectionCount() int
	ProviderName() string
	ModelName() string
	BroadcastToAll(ctx context.Context, method string, params any)
	SendToConn(ctx context.Context, connID string, method string, params any)
	Stop()
	Uptime() time.Duration
}

func NewServer(agent *core.Agent, daemon DaemonAPI) *Server {
	return &Server{
		agent:  agent,
		daemon: daemon,
	}
}

func (s *Server) Close() {}

// HandleConn 为单个连接启动 JSON-RPC 处理循环
func (s *Server) HandleConn(ctx context.Context, conn Conn, onDone func()) {
	defer onDone()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		raw, err := conn.Receive()
		if err != nil {
			return
		}

		var req Request
		if err := json.Unmarshal(raw, &req); err != nil {
			s.sendError(conn, 0, ErrParse, "parse error")
			continue
		}

		s.route(ctx, conn, req)
	}
}

func (s *Server) route(ctx context.Context, conn Conn, req Request) {
	switch req.Method {
	case MethodSendMessage:
		s.handleSendMessage(ctx, conn, req)
	case MethodCancel:
		s.handleCancel(ctx, conn, req)
	case "agent.askReply":
		s.handleAskReply(ctx, conn, req)
	case MethodMemorySearch:
		s.handleMemorySearch(ctx, conn, req)
	case MethodMemoryFacts:
		s.handleMemoryFacts(ctx, conn, req)
	case MethodSkillList:
		s.handleSkillList(ctx, conn, req)
	case MethodSessionNew:
		s.handleSessionNew(ctx, conn, req)
	case MethodSessionRestore:
		s.handleSessionRestore(ctx, conn, req)
	case MethodCompact:
		s.handleCompact(ctx, conn, req)
	case MethodUsage:
		s.handleUsage(ctx, conn, req)
	case MethodDaemonStatus:
		s.handleDaemonStatus(ctx, conn, req)
	case MethodConfigGet:
		s.handleConfigGet(ctx, conn, req)
	case MethodConfigSet:
		s.handleConfigSet(ctx, conn, req)
	case MethodDaemonStop:
		s.handleDaemonStop(ctx, conn, req)
	default:
		s.sendError(conn, req.ID, ErrNotFound, fmt.Sprintf("method not found: %s", req.Method))
	}
}

func (s *Server) handleConfigGet(ctx context.Context, conn Conn, req Request) {
	s.ensureConfigLoaded()
	s.sendResult(conn, req.ID, configToParams(s.agent.Config()))
}

func (s *Server) handleConfigSet(ctx context.Context, conn Conn, req Request) {
	var params ConfigSetParams
	if err := decodeParams(req.Params, &params); err != nil {
		s.sendError(conn, req.ID, ErrInvalidParams, err.Error())
		return
	}
	updated, err := s.agent.UpdateConfig(core.ConfigSetParams{
		Action:      configActionToCore(params.Action),
		ModelRef:    params.ModelRef,
		ActiveModel: params.ActiveModel,
		APIKey:      params.APIKey,
		Locale:      params.Locale,
		Theme:       params.Theme,
		Model: core.ConfigModel{
			Provider:      params.Model.Provider,
			Model:         params.Model.Model,
			BaseURL:       params.Model.BaseURL,
			ContextWindow: params.Model.ContextWindow,
			Strengths:     params.Model.Strengths,
		},
	})
	if err != nil {
		s.sendError(conn, req.ID, ErrInvalidParams, err.Error())
		return
	}
	result := configToParams(updated)
	s.sendResult(conn, req.ID, result)
	s.Send(ctx, conn, "config.state", result)
	s.Send(ctx, conn, "daemon.full_status", s.buildDaemonStatus(ctx))
}

func configActionToCore(action string) string {
	switch action {
	case ConfigActionUpsertModel, ConfigActionDeleteModel, ConfigActionActivateModel, ConfigActionUpdateGeneral:
		return action
	default:
		return action
	}
}

// handleSendMessage 核心方法：启动 Agent Loop 并流式推送事件
func (s *Server) handleSendMessage(ctx context.Context, conn Conn, req Request) {
	params, ok := req.Params.(map[string]any)
	if !ok {
		s.sendError(conn, req.ID, ErrInvalidParams, "invalid params")
		return
	}

	content, _ := params["content"].(string)
	if content == "" {
		s.sendError(conn, req.ID, ErrInvalidParams, "content is required")
		return
	}

	// 设置当前活跃连接
	s.activeConn.Store(&conn)

	// 立即响应：请求已接收
	s.sendResult(conn, req.ID, map[string]string{"status": "processing"})

	// 对话执行完全在 daemon/core 内完成。TUI 只收到流式 token、工具事件和最终统计，
	// 不自行推断 token、速率或上下文窗口，保证不同 UI 客户端看到一致状态。
	go func() {
		started := time.Now()
		events := s.agent.Run(ctx, content)
		for evt := range events {
			switch evt.Type {
			case core.EventStream:
				s.Send(ctx, conn, NotifyStream, StreamParams{Chunk: evt.Content})
			case core.EventReasoning:
				s.Send(ctx, conn, NotifyReasoning, StreamParams{Chunk: evt.Content})
			case core.EventToolCall:
				s.Send(ctx, conn, NotifyToolStart, ToolStartParams{
					ID: evt.ToolCallID, Tool: evt.ToolName, Params: evt.ToolParams, Intent: evt.ToolIntent,
				})
			case core.EventToolResult:
				s.Send(ctx, conn, NotifyToolEnd, ToolEndParams{
					ID: evt.ToolCallID, Tool: evt.ToolName, Result: evt.ToolResult, Error: evt.ToolError,
				})
			case core.EventAskUser:
				// 生成 askID，注册 pending，推送给 TUI
				askID := conn.ID() + "_" + fmt.Sprintf("%d", time.Now().UnixNano())
				if evt.Reply != nil {
					s.pendingAsks.Store(askID, evt.Reply)
				}
				s.Send(ctx, conn, NotifyAskUser, AskUserParams{
					Question: evt.Question,
					Options:  evt.Options,
					ID:       askID,
				})
			case core.EventStatus:
				if strings.HasPrefix(evt.Content, "error:") || evt.Content == "cancelled" {
					s.Send(ctx, conn, NotifyStream, StreamParams{Chunk: evt.Content, Done: true})
				} else if evt.Content == "done" {
					speed := 0.0
					if evt.HasUsage && evt.OutputTokens > 0 {
						if elapsed := time.Since(started).Seconds(); elapsed > 0 {
							speed = float64(evt.OutputTokens) / elapsed
						}
					}
					s.Send(ctx, conn, NotifyStream, StreamParams{
						Done:          true,
						InputTokens:   evt.InputTokens,
						OutputTokens:  evt.OutputTokens,
						CachedTokens:  evt.CachedTokens,
						HasUsage:      evt.HasUsage,
						ContextTokens: evt.ContextTokens,
						ContextWindow: evt.ContextWindow,
						TokensPerSec:  speed,
					})
				}
			}
		}
	}()
}

// handleAskReply 处理 TUI 回传的 AskUser 答案
func (s *Server) handleAskReply(ctx context.Context, conn Conn, req Request) {
	params, ok := req.Params.(map[string]any)
	if !ok {
		s.sendError(conn, req.ID, ErrInvalidParams, "invalid params")
		return
	}

	askID, _ := params["id"].(string)
	answer, _ := params["answer"].(string)

	val, ok := s.pendingAsks.LoadAndDelete(askID)
	if !ok {
		s.sendError(conn, req.ID, ErrNotFound, "ask session not found or expired")
		return
	}

	replyCh := val.(chan string)
	replyCh <- answer
	close(replyCh)

	s.sendResult(conn, req.ID, map[string]string{"status": "ok"})
}

func (s *Server) handleCancel(ctx context.Context, conn Conn, req Request) {
	s.agent.CancelCurrentRun()
	s.sendResult(conn, req.ID, map[string]string{"status": "cancelled"})
}

func (s *Server) handleMemorySearch(ctx context.Context, conn Conn, req Request) {
	params, ok := req.Params.(map[string]any)
	if !ok {
		s.sendError(conn, req.ID, ErrInvalidParams, "invalid params")
		return
	}

	query, _ := params["query"].(string)
	topK := 5
	if tk, ok := params["top_k"].(float64); ok && int(tk) > 0 {
		topK = int(tk)
	}

	memories, err := s.agent.SearchMemory(ctx, query, topK)
	if err != nil {
		s.sendError(conn, req.ID, ErrInternal, err.Error())
		return
	}

	result := MemorySearchResult{Memories: make([]MemoryItem, 0, len(memories))}
	for _, m := range memories {
		result.Memories = append(result.Memories, MemoryItem{
			ID: m.ID, Content: m.Content, Type: m.Type,
			Timestamp: m.Timestamp.Format("2006-01-02 15:04"),
		})
	}
	s.sendResult(conn, req.ID, map[string]string{"status": "ok"})
	s.Send(ctx, conn, NotifyMemorySearchResult, result)
}

func (s *Server) handleMemoryFacts(ctx context.Context, conn Conn, req Request) {
	summary, err := s.agent.SemanticSummary(ctx)
	if err != nil {
		s.sendError(conn, req.ID, ErrInternal, err.Error())
		return
	}
	s.sendResult(conn, req.ID, map[string]string{"summary": summary})
}

func (s *Server) handleSkillList(ctx context.Context, conn Conn, req Request) {
	caps := s.agent.ListCapabilities()
	s.sendResult(conn, req.ID, caps)
}

func (s *Server) handleSessionNew(ctx context.Context, conn Conn, req Request) {
	s.agent.NewSession()
	s.sendResult(conn, req.ID, map[string]string{"status": "ok"})
	s.Send(ctx, conn, "daemon.full_status", s.buildDaemonStatus(ctx))
}

func (s *Server) handleSessionRestore(ctx context.Context, conn Conn, req Request) {
	count := s.agent.RestoreSession(ctx)
	if count > 0 {
		msgs := s.agent.WorkingMessages()
		for _, m := range msgs {
			content := m.Text()
			if content == "" {
				continue
			}
			switch m.Role {
			case "user":
				s.Send(ctx, conn, NotifySessionRestoreMsg, map[string]string{"role": "user", "content": content})
			case "assistant":
				s.Send(ctx, conn, NotifySessionRestoreMsg, map[string]string{"role": "assistant", "content": content})
			}
		}
	}
	if input := s.agent.ConsumeResumeInput(); input != "" {
		s.Send(ctx, conn, NotifySessionRestoreInput, map[string]string{"content": input})
	}
	s.sendResult(conn, req.ID, map[string]int{"messages": count})
}

func (s *Server) handleCompact(ctx context.Context, conn Conn, req Request) {
	before, after, contextWindow, turnsCompressed, truncated, err := s.agent.Compact(ctx)
	if err != nil {
		s.sendError(conn, req.ID, ErrInternal, err.Error())
		return
	}
	result := CompactResult{
		BeforeTokens:     before,
		AfterTokens:      after,
		ContextWindow:    contextWindow,
		TurnsCompressed:  turnsCompressed,
		SummaryTokens:    (before - after) / 2,
		TruncatedOutputs: truncated,
	}
	s.sendResult(conn, req.ID, map[string]string{"status": "ok"})
	s.Send(ctx, conn, NotifyCompactResult, result)
}

func (s *Server) handleUsage(ctx context.Context, conn Conn, req Request) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	week := today.AddDate(0, 0, -7)
	month := today.AddDate(0, -1, 0)

	result := UsageResult{}
	if sum, err := s.agent.UsageSummary(ctx, today); err == nil && sum != nil {
		result.Today = periodFromSummary(sum)
	}
	if sum, err := s.agent.UsageSummary(ctx, week); err == nil && sum != nil {
		result.Week = periodFromSummary(sum)
	}
	if sum, err := s.agent.UsageSummary(ctx, month); err == nil && sum != nil {
		result.Month = periodFromSummary(sum)
	}
	s.sendResult(conn, req.ID, result)
}

func (s *Server) handleDaemonStatus(ctx context.Context, conn Conn, req Request) {
	s.ensureConfigLoaded()
	params := s.buildDaemonStatus(ctx)
	s.sendResult(conn, req.ID, params)
	s.Send(ctx, conn, "daemon.full_status", params)
}

func (s *Server) handleDaemonStop(ctx context.Context, conn Conn, req Request) {
	s.sendResult(conn, req.ID, map[string]string{"status": "stopping"})
	go func() {
		time.Sleep(100 * time.Millisecond)
		log.Println("[daemon] stop requested via IPC")
		s.daemon.Stop()
	}()
}

// SendDaemonState 向新连接推送 daemon 初始状态
func (s *Server) SendDaemonState(ctx context.Context, conn Conn) {
	s.ensureConfigLoaded()
	params := DaemonStateParams{
		AgentStatus: "idle",
	}
	if s.daemon != nil {
		params.PID = os.Getpid()
		params.Uptime = s.daemon.Uptime().Truncate(time.Second).String()
		params.Connections = s.daemon.ConnectionCount()
		params.ProviderName = s.daemon.ProviderName()
		params.ModelName = s.daemon.ModelName()
	}
	s.Send(ctx, conn, NotifyDaemonState, params)

	s.Send(ctx, conn, "daemon.full_status", s.buildDaemonStatus(ctx))
}

func (s *Server) buildDaemonStatus(ctx context.Context) DaemonStatusParams {
	s.ensureConfigLoaded()
	params := DaemonStatusParams{
		PID:         os.Getpid(),
		AgentStatus: "idle",
		Provider:    s.daemon.ProviderName(),
		Model:       s.daemon.ModelName(),
		Uptime:      s.daemon.Uptime().Truncate(time.Second).String(),
		Connections: s.daemon.ConnectionCount(),
	}
	if s.agent != nil {
		ep, en, fa := s.agent.MemoryStats(ctx)
		params.Memory = &MemoryStats{Episodes: ep, Entities: en, Facts: fa}
		active, completed, lastID := s.agent.SessionStats(ctx)
		params.Sessions = &SessionStats{Active: active, Completed: completed, LastID: lastID}
		if sum, err := s.agent.UsageSummary(ctx, time.Now().Add(-24*time.Hour)); err == nil && sum != nil {
			usage := periodFromSummary(sum)
			params.UsageToday = &usage
		}
	}
	if mc, ok := s.agent.Config().ActiveModelConfig(); ok {
		if params.Provider == "" {
			params.Provider = mc.Provider
		}
		if params.Model == "" {
			params.Model = mc.Model
		}
		params.ContextWindow = mc.ContextWindow
	}
	if params.ContextWindow <= 0 && s.agent != nil {
		if mc, ok := s.agent.Config().ActiveModelConfig(); ok {
			switch mc.Provider {
			case "anthropic":
				params.ContextWindow = 200000
			default:
				params.ContextWindow = 128000
			}
		}
	}
	return params
}

func (s *Server) ensureConfigLoaded() {
	if s == nil || s.agent == nil {
		return
	}
	if _, err := s.agent.ReloadConfigFromDiskIfNeeded(); err != nil {
		log.Printf("[config] reload skipped: %v", err)
	}
}

// Send 向单个连接发送 JSON-RPC 通知
func (s *Server) Send(ctx context.Context, conn Conn, method string, params any) {
	notif := Notification{JSONRPC: "2.0", Method: method, Params: params}
	data, err := json.Marshal(notif)
	if err != nil {
		return
	}
	sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := conn.Send(sendCtx, data); err != nil {
		log.Printf("[ipc] send to %s error: %v", conn.ID(), err)
	}
}

// Broadcast 向多个连接广播通知
func (s *Server) Broadcast(ctx context.Context, conns []Conn, method string, params any) {
	var wg sync.WaitGroup
	for _, conn := range conns {
		wg.Add(1)
		go func(c Conn) {
			defer wg.Done()
			s.Send(ctx, c, method, params)
		}(conn)
	}
	wg.Wait()
}

func (s *Server) sendResult(conn Conn, id int, result any) {
	resp := Response{JSONRPC: "2.0", ID: id, Result: result}
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn.Send(ctx, data)
}

func (s *Server) sendError(conn Conn, id int, code int, message string) {
	resp := Response{JSONRPC: "2.0", ID: id, Error: &Error{Code: code, Message: message}}
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn.Send(ctx, data)
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

func periodFromSummary(sum *memory.UsageSummary) UsagePeriod {
	return UsagePeriod{
		InputTokens:  sum.InputTokens,
		OutputTokens: sum.OutputTokens,
		Cost:         sum.Cost,
		Requests:     sum.Requests,
	}
}

func configToParams(cfg *config.Config) ConfigParams {
	out := ConfigParams{ActiveModel: cfg.ActiveModel, Locale: cfg.UI.Locale, Theme: cfg.UI.Theme}
	for _, mc := range cfg.Models {
		out.Models = append(out.Models, ConfigModel{Provider: mc.Provider, Model: mc.Model, BaseURL: mc.BaseURL, ContextWindow: mc.ContextWindow, Strengths: mc.Strengths, HasAPIKey: mc.APIKey != ""})
	}
	return out
}
