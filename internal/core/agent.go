package core

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/alanchenchen/suna/internal/capability"
	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/guard"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/prompt"
	"github.com/alanchenchen/suna/internal/tool"
)

/*
Agent 是 Suna 内核的全部对外 API。

设计原则：
  - Agent 不知道谁在调用它（TUI / GUI / Gateway / CLI 都一样）
  - Agent 只做两件事：接收输入返回事件流，暴露管理方法
  - 事件流是唯一的输出通道，包含流式文本、工具调用、AskUser 请求等
  - AskUser 通过 Event.Reply channel 回传，调用方自行决定怎么交互

API 表面：

	Run(ctx, input) ←chan Event    核心：输入 → 事件流
	NewSession()                    新建会话
	ListModels() []string           列出可用模型
	SearchMemory(ctx, query)        搜索记忆
	Compact()                       压缩上下文
	Close()                         释放资源
*/
type Agent struct {
	cfg        *config.Config
	router     *model.Router
	registry   *tool.Registry
	guard      *guard.Guard
	working    *memory.WorkingMemory
	episodic   *memory.EpisodicStore
	semantic   *memory.SemanticStore
	sessions   *memory.SessionStore
	entities   *memory.EntityStore
	compressor *memory.Compressor
	prompts    *prompt.Loader
	store      *memory.Store
	caps       *capability.Loader
	sessionID  string
	turnCount  int

	extractQueue  *memory.ExtractQueue
	extractWorker *memory.Worker
	closeOnce     sync.Once
	closed        bool

	// cancelRun 用于中断当前正在执行的 Run
	cancelMu sync.Mutex
	cancelFn context.CancelFunc
}

/*
NewAgent 创建并初始化 Agent。

组装顺序：config → router → store → guard → registry → prompts → agent。
所有组件在此完成初始化，外部无需关心内部依赖。
*/
func NewAgent(cfg *config.Config) (*Agent, error) {
	router, err := model.NewRouter(cfg)
	if err != nil {
		return nil, fmt.Errorf("init router: %w", err)
	}

	store, err := memory.NewStore(cfg.DBPath())
	if err != nil {
		return nil, fmt.Errorf("init memory store: %w", err)
	}

	sessionID := uuid.New().String()
	var g *guard.Guard
	if len(cfg.Guard.Blocked) > 0 || len(cfg.Guard.Allowed) > 0 {
		var blockedPats, blockedReasons []string
		for _, b := range cfg.Guard.Blocked {
			blockedPats = append(blockedPats, b.Pattern)
			blockedReasons = append(blockedReasons, b.Reason)
		}
		var allowedPats, allowedTools []string
		for _, a := range cfg.Guard.Allowed {
			allowedPats = append(allowedPats, a.Pattern)
			allowedTools = append(allowedTools, a.Tool)
		}
		g = guard.NewGuardWithConfig(store.DB(), sessionID, blockedPats, blockedReasons, allowedPats, allowedTools)
	} else {
		g = guard.NewGuard(store.DB(), sessionID)
	}

	registry := tool.NewRegistry()
	registry.Register(tool.ReadFile{})
	registry.Register(tool.ListDir{})
	registry.Register(tool.ReadHTTP{})
	registry.Register(tool.Exec{})
	registry.Register(tool.WriteFile{})
	registry.Register(tool.EditFile{})
	registry.Register(tool.WriteHTTP{})

	capLoader := capability.NewLoader()
	capDir := filepath.Join(cfg.DataDir, "capabilities")
	capLoader.LoadAll(context.Background(), capDir)

	prompts, err := prompt.New()
	if err != nil {
		return nil, fmt.Errorf("init prompts: %w", err)
	}

	episodic := memory.NewEpisodicStore(store.DB())
	semantic := memory.NewSemanticStore(store.DB())
	sessions := memory.NewSessionStore(store.DB())
	entities := memory.NewEntityStore(store.DB())

	// 过期超过 7 天的 active 会话
	sessions.ExpireOldSessions(context.Background(), 7*24*time.Hour)

	var extractProvider model.Provider
	if p, _ := router.EmbeddingProvider(); p != nil {
		extractProvider = p
	} else if p, err := router.Provider("fast"); err == nil {
		extractProvider = p
	} else if p := router.DefaultProvider(); p != nil {
		extractProvider = p
	}

	extractQueue := memory.NewExtractQueue(store.DB())
	extractWorker := memory.NewWorker(
		extractQueue,
		episodic,
		semantic,
		entities,
		sessions,
		extractProvider,
	)

	agent := &Agent{
		cfg:           cfg,
		router:        router,
		registry:      registry,
		guard:         g,
		working:       memory.NewWorkingMemory(),
		episodic:      episodic,
		semantic:      semantic,
		sessions:      sessions,
		entities:      entities,
		compressor:    memory.NewCompressor(extractProvider),
		prompts:       prompts,
		store:         store,
		caps:          capLoader,
		sessionID:     sessionID,
		extractQueue:  extractQueue,
		extractWorker: extractWorker,
	}

	go extractWorker.Run()

	extractQueue.RecoverUnextracted(context.Background())

	return agent, nil
}

// === EventType 事件类型 ===

type EventType int

const (
	EventStream EventType = iota
	EventReasoning
	EventToolCall
	EventToolResult
	EventStatus
	EventAskUser
)

// Event agent 输出的原子事件。调用方根据 Type 决定如何处理。
type Event struct {
	Type EventType

	// EventStream / EventStatus
	Content string

	// EventToolCall
	ToolName   string
	ToolCallID string
	ToolParams map[string]any

	// EventToolResult
	ToolResult string
	ToolError  bool

	// EventAskUser：调用方收到后，应向用户展示问题并收集回复，写入 Reply channel
	Question string
	Options  []string
	Reply    chan string

	// EventDone（EventStatus with Content=="done"）携带的 token 用量
	InputTokens  int
	OutputTokens int
	CachedTokens int
}

// === 核心 API ===

/*
Run 接收一条输入，执行 agent loop，返回事件流。

Agent loop 流程：
 1. 路由：选择模型
 2. 构建 system prompt（模板 + 环境信息 + SOUL.md + SUNA.md + 用户偏好）
 3. 调用 LLM（streaming）
 4. 处理输出：
    - 纯文本 → EventStream
    - Tool Call → 执行工具 → EventToolCall + EventToolResult → 回到 3
    - AskUser → EventAskUser（阻塞等待 Reply channel）
 5. 终止：LLM 不再发起 tool_call → 后台提取记忆 → 关闭 channel

调用方应在一个 goroutine 中 range 遍历返回的 channel：

	for evt := range agent.Run(ctx, "hello") { ... }
*/
func (a *Agent) Run(ctx context.Context, input string) <-chan Event {
	events := make(chan Event, 64)

	// 创建可取消的 context
	runCtx, cancel := context.WithCancel(ctx)
	a.cancelMu.Lock()
	a.cancelFn = cancel
	a.cancelMu.Unlock()

	go func() {
		defer close(events)
		defer cancel()

		a.working.AddMessage(model.NewTextMessage(model.RoleUser, input))

		a.turnCount++
		if a.sessions != nil {
			if a.turnCount == 1 {
				a.sessions.CreateSession(runCtx, a.sessionID)
			}
			a.sessions.SaveMessage(runCtx, a.sessionID, a.turnCount, "user", input)
		}

		var hadToolCall bool

		for {
			// 检查是否被取消
			if runCtx.Err() != nil {
				events <- Event{Type: EventStatus, Content: "cancelled"}
				return
			}

			// 路由：选择模型
			provider, modelName, err := a.router.Route(runCtx, input)
			if err != nil {
				events <- Event{Type: EventStatus, Content: "error: " + err.Error()}
				return
			}

			// 构建请求
			systemPrompt, _ := a.buildSystemPrompt(runCtx)
			messages := a.working.Messages()
			tools := a.buildToolDefs()
			modelID := resolveModelID(a.cfg, modelName)

			req := &model.CompletionRequest{
				Model:     modelID,
				System:    systemPrompt,
				Messages:  messages,
				Tools:     tools,
				MaxTokens: 4096,
			}

			// 调用 LLM
			events <- Event{Type: EventStatus, Content: "waiting_llm"}
			ch, err := provider.Complete(runCtx, req)
			if err != nil {
				events <- Event{Type: EventStatus, Content: "error: " + err.Error()}
				return
			}

			var fullContent string
			var toolCalls []model.ToolCall
			var lastUsage *model.Usage

			// 流式读取，带 120 秒超时防卡死
			streamTimeout := time.NewTimer(120 * time.Second)
			defer streamTimeout.Stop()

		streamLoop:
			for {
				select {
				case chunk, ok := <-ch:
					if !ok {
						break streamLoop
					}
					streamTimeout.Reset(120 * time.Second)
					if runCtx.Err() != nil {
						events <- Event{Type: EventStatus, Content: "cancelled"}
						return
					}
					if chunk.ReasoningContent != "" {
						events <- Event{Type: EventReasoning, Content: chunk.ReasoningContent}
					}
					if chunk.Content != "" {
						fullContent += chunk.Content
						events <- Event{Type: EventStream, Content: chunk.Content}
					}
					if len(chunk.ToolCalls) > 0 {
						toolCalls = append(toolCalls, chunk.ToolCalls...)
					}
					if chunk.Usage != nil {
						lastUsage = chunk.Usage
					}
					if chunk.Done {
						break streamLoop
					}
				case <-streamTimeout.C:
					events <- Event{Type: EventStatus, Content: "error: LLM stream timeout (120s), try Esc to cancel"}
					return
				case <-runCtx.Done():
					events <- Event{Type: EventStatus, Content: "cancelled"}
					return
				}
			}

			if fullContent != "" || len(toolCalls) == 0 {
				// 处理 [LOAD_SKILL: name] 标记
				if a.caps != nil {
					cleaned, loaded := a.caps.ProcessLoadMarkers(fullContent)
					if len(loaded) > 0 && cleaned != fullContent {
						fullContent = cleaned
						// 将加载的能力注入为 system 消息
						for _, name := range loaded {
							if prompt, ok := a.caps.LoadSkill(name); ok {
								a.working.AddMessage(model.NewTextMessage(model.RoleSystem,
									fmt.Sprintf("[Capability: %s]\n%s", name, prompt)))
							}
						}
					}
				}
				a.working.AddMessage(model.NewTextMessage(model.RoleAssistant, fullContent))
			}

			if len(toolCalls) == 0 {
				doneEvt := Event{Type: EventStatus, Content: "done"}
				if lastUsage != nil {
					doneEvt.InputTokens = lastUsage.InputTokens
					doneEvt.OutputTokens = lastUsage.OutputTokens
					doneEvt.CachedTokens = lastUsage.CachedTokens
					if a.sessions != nil {
						go a.sessions.SaveUsage(runCtx, a.sessionID, modelID, lastUsage.InputTokens, lastUsage.OutputTokens, 0)
					}
				}
				if a.sessions != nil && fullContent != "" {
					a.sessions.SaveMessage(runCtx, a.sessionID, a.turnCount, "assistant", fullContent)
				}
				events <- doneEvt
				go a.extractMemories(runCtx, input, fullContent, hadToolCall, false, false, false)
				return
			}

			// 工具调用
			assistantMsg := model.Message{
				Role:        model.RoleAssistant,
				TextContent: fullContent,
				Content:     []model.ContentBlock{{Type: model.ContentText, Text: fullContent}},
				ToolCalls:   toolCalls,
			}
			a.working.AddMessage(assistantMsg)

			for _, tc := range toolCalls {
				hadToolCall = true
				params := model.ParseToolCallArguments(tc.Arguments)
				events <- Event{Type: EventToolCall, ToolCallID: tc.ID, ToolName: tc.Name, ToolParams: params}
			}

			type toolExecResult struct {
				tc     model.ToolCall
				result tool.Result
			}
			resultCh := make(chan toolExecResult, len(toolCalls))
			for _, tc := range toolCalls {
				go func(tc model.ToolCall) {
					params := model.ParseToolCallArguments(tc.Arguments)
					result := a.executeTool(runCtx, tc.Name, params, events)
					resultCh <- toolExecResult{tc: tc, result: result}
				}(tc)
			}

			for i := 0; i < len(toolCalls); i++ {
				if runCtx.Err() != nil {
					events <- Event{Type: EventStatus, Content: "cancelled"}
					return
				}
				r := <-resultCh
				events <- Event{Type: EventToolResult, ToolCallID: r.tc.ID, ToolName: r.tc.Name, ToolResult: r.result.Content, ToolError: r.result.IsError}
				a.working.AddMessage(model.Message{
					Role:        model.RoleTool,
					ToolCallID:  r.tc.ID,
					TextContent: r.result.Content,
					Content:     []model.ContentBlock{{Type: model.ContentText, Text: r.result.Content}},
				})
			}

			// 记录每次 LLM 调用的用量
			if lastUsage != nil && a.sessions != nil {
				go a.sessions.SaveUsage(runCtx, a.sessionID, modelID, lastUsage.InputTokens, lastUsage.OutputTokens, 0)
			}

			// 上下文压缩检查
			contextWindow := provider.ContextWindow()
			msgs := a.working.Messages()
			shouldCompress := a.compressor.ShouldCompress(msgs, contextWindow) ||
				(len(msgs) > memory.AutoCompactMinTurns && a.compressor.EstimateTokens(msgs) > contextWindow/2)
			if shouldCompress {
				compressed, _, compErr := a.compressor.CompressHistory(runCtx, msgs)
				if compErr == nil {
					a.working.SetMessages(compressed)
				}
			}
			// 回到循环顶部，继续调 LLM
		}
	}()

	return events
}

// === 管理 API ===

// NewSession 重置会话状态，创建新的 session ID 和 Guard
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

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
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

	// 其他 active sessions 全部标记 completed
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
	for _, m := range msgs {
		a.working.AddMessage(model.NewTextMessage(model.Role(m.Role), m.Content))
	}

	return len(msgs)
}

func (a *Agent) WorkingMessages() []model.Message {
	if a.working == nil {
		return nil
	}
	return a.working.Messages()
}

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

// UsageSummary 返回指定时间段的用量统计
func (a *Agent) UsageSummary(ctx context.Context, since time.Time) (*memory.UsageSummary, error) {
	if a.sessions == nil {
		return nil, fmt.Errorf("session store not initialized")
	}
	return a.sessions.UsageSummary(ctx, since)
}

// ListModels 返回所有已配置的模型名称
func (a *Agent) ListModels() []string {
	return a.router.ListProviders()
}

func (a *Agent) Config() *config.Config {
	return a.cfg
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

// WorkingTokens 返回当前上下文的估算 token 数
func (a *Agent) WorkingTokens() int {
	return a.working.EstimatedTokens()
}

// SearchMemory 搜索情景记忆
func (a *Agent) SearchMemory(ctx context.Context, query string, limit int) ([]*memory.EpisodicMemory, error) {
	return a.episodic.SearchFTS(ctx, query, limit)
}

// ListCapabilities 列出所有已加载的能力
func (a *Agent) ListCapabilities() []capability.Info {
	if a.caps == nil {
		return nil
	}
	return a.caps.List()
}

// SemanticSummary 返回用户语义记忆摘要
func (a *Agent) SemanticSummary(ctx context.Context) (string, error) {
	if a.semantic == nil {
		return "", nil
	}
	return a.semantic.Summary(ctx)
}

// ReloadCapabilities 重新加载能力目录
func (a *Agent) ReloadCapabilities() error {
	if a.caps == nil {
		a.caps = capability.NewLoader()
	}
	capDir := filepath.Join(a.cfg.DataDir, "capabilities")
	return a.caps.Reload(context.Background(), capDir)
}

// Compact 手动触发上下文压缩
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
	_ = len(summary) / 4
	a.working.SetMessages(compressed)
	after := a.working.EstimatedTokens()

	truncated := 0
	for _, m := range msgs {
		if m.Role == model.RoleTool {
			txt := m.Text()
			if len(txt) > 50*1024 {
				truncated++
			}
		}
	}

	contextWindow := 128000
	if provider, _, err := a.router.Route(ctx, ""); err == nil {
		contextWindow = provider.ContextWindow()
	}

	return before, after, contextWindow, turnsCompressed, truncated, nil
}

// Close 释放所有资源。幂等，可安全多次调用。
// 等待后台记忆提取完成后再关闭数据库。
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

// CancelCurrentRun 中断当前正在执行的 Run（LLM 请求或 tool 调用）。
// 不会关闭 Agent，只是停止当前操作。幂等。
func (a *Agent) CancelCurrentRun() {
	a.cancelMu.Lock()
	defer a.cancelMu.Unlock()
	if a.cancelFn != nil {
		a.cancelFn()
		a.cancelFn = nil
	}
}
