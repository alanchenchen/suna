package core

import (
	"context"
	"fmt"
	"os"
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
	cfg         *config.Config
	router      *model.Router
	registry    *tool.Registry
	guard       *guard.Guard
	working     *memory.WorkingMemory
	episodic    *memory.EpisodicStore
	semantic    *memory.SemanticStore
	sessions    *memory.SessionStore
	entities    *memory.EntityStore
	compressor  *memory.Compressor
	prompts     *prompt.Loader
	store       *memory.Store
	caps        *capability.Loader
	sessionID   string
	turnCount   int
	modelRef    string
	resumeInput string

	extractQueue  *memory.ExtractQueue
	extractWorker *memory.Worker
	closeOnce     sync.Once
	closed        bool
	configMu      sync.RWMutex
	configModTime time.Time
	runMu         sync.Mutex

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
	var router *model.Router
	if len(cfg.Models) > 0 && cfg.ActiveModel != "" {
		var err error
		router, err = model.NewRouter(cfg)
		if err != nil {
			return nil, fmt.Errorf("init router: %w", err)
		}
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

	sessions.ExpireOldSessions(context.Background(), 7*24*time.Hour)

	var extractProvider model.Provider
	if router != nil {
		if p, _ := router.EmbeddingProvider(); p != nil {
			extractProvider = p
		} else if p, err := router.Provider("fast"); err == nil {
			extractProvider = p
		} else if p := router.DefaultProvider(); p != nil {
			extractProvider = p
		}
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
	if info, err := os.Stat(cfg.ConfigPath()); err == nil {
		agent.configModTime = info.ModTime()
	}

	if router != nil {
		if cfg.Guard.IsEnabled() {
			g.SetLLMReviewer(agent.guardLLMReview)
		}
		router.SetPrompts(prompts)
	}
	agent.compressor.SetPrompts(prompts)
	agent.extractWorker.SetPrompts(prompts)

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
	ToolIntent string

	// EventToolResult
	ToolResult string
	ToolError  bool

	// EventAskUser：调用方收到后，应向用户展示问题并收集回复，写入 Reply channel
	Question string
	Options  []string
	Reply    chan string

	// EventDone（EventStatus with Content=="done"）携带的 token 用量
	InputTokens   int
	OutputTokens  int
	CachedTokens  int
	ContextTokens int
	ContextWindow int
	HasUsage      bool
}

// === 核心 API ===

/*
Run 接收一条输入，执行 agent loop，返回事件流。

Agent loop 流程：
 1. 路由：选择模型
 2. 构建 system prompt（模板 + 环境信息 + SUNA.md + 用户偏好）
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
	if !a.runMu.TryLock() {
		events <- Event{Type: EventStatus, Content: "error: agent is already running"}
		close(events)
		return events
	}

	// 创建可取消的 context
	runCtx, cancel := context.WithCancel(ctx)
	a.cancelMu.Lock()
	a.cancelFn = cancel
	a.cancelMu.Unlock()

	go func() {
		defer a.runMu.Unlock()
		defer close(events)
		defer cancel()

		if a.router == nil {
			events <- Event{Type: EventStatus, Content: "error: no model configured, please add a model in config"}
			return
		}
		defer func() {
			a.cancelMu.Lock()
			a.cancelFn = nil
			a.cancelMu.Unlock()
		}()

		a.working.AddMessage(model.NewTextMessage(model.RoleUser, input))

		a.turnCount++
		if a.sessions != nil {
			if a.turnCount == 1 {
				a.sessions.CreateSession(runCtx, a.sessionID)
			}
			a.sessions.SaveMessage(runCtx, a.sessionID, a.turnCount, "user", input)
		}

		var hadToolCall bool
		var hadToolFailure bool

		for {
			// 检查是否被取消
			if runCtx.Err() != nil {
				events <- Event{Type: EventStatus, Content: "cancelled"}
				return
			}

			// 路由：选择模型（sub-agent 使用指定的 modelRef）
			var modelRef string
			isSubAgent := a.modelRef != ""
			if isSubAgent {
				modelRef = a.modelRef
			} else {
				_, ref, err := a.router.Route(runCtx, input)
				if err != nil {
					events <- Event{Type: EventStatus, Content: "error: " + err.Error()}
					return
				}
				modelRef = ref
			}

			// 构建请求
			systemPrompt, _ := a.buildSystemPrompt(runCtx)
			messages := a.working.Messages()
			tools := a.buildToolDefs()
			modelID := resolveModelID(a.cfg, modelRef)

			req := &model.CompletionRequest{
				Model:     modelID,
				System:    systemPrompt,
				Messages:  messages,
				Tools:     tools,
				MaxTokens: 4096,
			}

			// sub-agent 走 rate limiter，main agent 直接调用
			events <- Event{Type: EventStatus, Content: "waiting_llm"}
			var ch <-chan model.Chunk
			var llmErr error
			if isSubAgent {
				ch, llmErr = a.router.Complete(runCtx, modelRef, req)
			} else {
				p, _ := a.router.Provider(modelRef)
				if p == nil {
					events <- Event{Type: EventStatus, Content: "error: provider not found"}
					return
				}
				ch, llmErr = p.Complete(runCtx, req)
			}
			if llmErr != nil {
				events <- Event{Type: EventStatus, Content: "error: " + llmErr.Error()}
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
					if chunk.Error != "" {
						events <- Event{Type: EventStatus, Content: "error: " + chunk.Error}
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
					doneEvt.HasUsage = true
					doneEvt.InputTokens = lastUsage.InputTokens
					doneEvt.OutputTokens = lastUsage.OutputTokens
					doneEvt.CachedTokens = lastUsage.CachedTokens
					doneEvt.ContextTokens = lastUsage.TotalTokens
					if a.sessions != nil {
						go a.sessions.SaveUsage(runCtx, a.sessionID, modelID, lastUsage.InputTokens, lastUsage.OutputTokens, 0)
					}
				}
				if p, err := a.router.Provider(modelRef); err == nil && p != nil {
					doneEvt.ContextWindow = p.ContextWindow()
				}
				if doneEvt.HasUsage && doneEvt.ContextTokens <= 0 {
					doneEvt.ContextTokens = doneEvt.InputTokens + doneEvt.OutputTokens
				}
				if a.sessions != nil && fullContent != "" {
					a.sessions.SaveMessage(runCtx, a.sessionID, a.turnCount, "assistant", fullContent)
				}
				events <- doneEvt
				go a.extractMemories(runCtx, input, fullContent, hadToolCall, hadToolFailure, false, false)
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

			toolIntent := extractToolIntent(fullContent)
			for _, tc := range toolCalls {
				hadToolCall = true
				params := model.ParseToolCallArguments(tc.Arguments)
				intent := consumeToolIntent(params)
				if intent == "" {
					intent = toolIntent
				}
				events <- Event{Type: EventToolCall, ToolCallID: tc.ID, ToolName: tc.Name, ToolParams: params, ToolIntent: intent}
			}

			type toolExecResult struct {
				index  int
				tc     model.ToolCall
				result tool.Result
			}
			resultCh := make(chan toolExecResult, len(toolCalls))
			for i, tc := range toolCalls {
				go func(index int, tc model.ToolCall) {
					params := model.ParseToolCallArguments(tc.Arguments)
					result := a.executeTool(runCtx, tc.Name, params, events)
					resultCh <- toolExecResult{index: index, tc: tc, result: result}
				}(i, tc)
			}

			results := make([]toolExecResult, len(toolCalls))
			toolFailed := false
			for i := 0; i < len(toolCalls); i++ {
				if runCtx.Err() != nil {
					events <- Event{Type: EventStatus, Content: "cancelled"}
					return
				}
				r := <-resultCh
				events <- Event{Type: EventToolResult, ToolCallID: r.tc.ID, ToolName: r.tc.Name, ToolResult: r.result.Content, ToolError: r.result.IsError}
				if r.result.IsError {
					toolFailed = true
				}
				results[r.index] = r
			}
			// LLM 上下文中的 tool_result 顺序保持与 assistant tool_calls 一致，避免兼容 API 拒绝请求。
			for _, r := range results {
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
			p, _ := a.router.Provider(modelRef)
			contextWindow := 128000
			if p != nil {
				contextWindow = p.ContextWindow()
			}
			msgs := a.working.Messages()
			shouldCompress := a.compressor.ShouldCompress(msgs, contextWindow) ||
				(len(msgs) > memory.AutoCompactMinTurns && a.compressor.EstimateTokens(msgs) > contextWindow/2)
			if shouldCompress {
				compressed, _, compErr := a.compressor.CompressHistory(runCtx, msgs)
				if compErr == nil {
					a.working.SetMessages(compressed)
				}
			}
			if toolFailed {
				hadToolFailure = true
			}
			// 回到循环顶部，继续调 LLM
		}
	}()

	return events
}

func consumeToolIntent(params map[string]any) string {
	if params == nil {
		return ""
	}
	intent, _ := params["intent"].(string)
	delete(params, "intent")
	return strings.TrimSpace(intent)
}

func truncateStr(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	for i := range s {
		if i > max {
			return s[:i] + "..."
		}
	}
	return s
}

func extractToolIntent(fullContent string) string {
	text := strings.TrimSpace(fullContent)
	if text == "" {
		return ""
	}
	sentences := strings.FieldsFunc(text, func(r rune) bool {
		return r == '.' || r == '。' || r == '\n'
	})
	for i := len(sentences) - 1; i >= 0; i-- {
		s := strings.TrimSpace(sentences[i])
		if s != "" {
			return s
		}
	}
	return ""
}
