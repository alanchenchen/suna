package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"time"

	"github.com/google/uuid"

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
	cfg       *config.Config
	router    *model.Router
	registry  *tool.Registry
	guard     *guard.Guard
	working   *memory.WorkingMemory
	episodic  *memory.EpisodicStore
	semantic  *memory.SemanticStore
	compressor *memory.Compressor
	prompts   *prompt.Loader
	store     *memory.Store
	sessionID string
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
	g := guard.NewGuard(store.DB(), sessionID)

	registry := tool.NewRegistry()
	registry.Register(tool.ReadFile{})
	registry.Register(tool.ListDir{})
	registry.Register(tool.ReadHTTP{})
	registry.Register(tool.Exec{})
	registry.Register(tool.WriteFile{})
	registry.Register(tool.EditFile{})
	registry.Register(tool.WriteHTTP{})

	prompts, err := prompt.New()
	if err != nil {
		return nil, fmt.Errorf("init prompts: %w", err)
	}

	episodic := memory.NewEpisodicStore(store.DB())
	semantic := memory.NewSemanticStore(store.DB())

	var fastProvider model.Provider
	if p, err := router.Provider("fast"); err == nil {
		fastProvider = p
	}

	return &Agent{
		cfg:        cfg,
		router:     router,
		registry:   registry,
		guard:      g,
		working:    memory.NewWorkingMemory(),
		episodic:   episodic,
		semantic:   semantic,
		compressor: memory.NewCompressor(fastProvider),
		prompts:    prompts,
		store:      store,
		sessionID:  sessionID,
	}, nil
}

// === EventType 事件类型 ===

type EventType int

const (
	// EventStream LLM 流式输出文本片段
	EventStream EventType = iota
	// EventToolCall 工具被调用（执行前通知）
	EventToolCall
	// EventToolResult 工具执行结果
	EventToolResult
	// EventStatus 状态变更（thinking / done / error）
	EventStatus
	// EventAskUser agent 需要用户输入，调用方通过 Reply channel 回传
	EventAskUser
)

// Event agent 输出的原子事件。调用方根据 Type 决定如何处理。
type Event struct {
	Type EventType

	// EventStream / EventStatus
	Content string

	// EventToolCall
	ToolName   string
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

	go func() {
		defer close(events)
		a.working.AddMessage(model.NewTextMessage(model.RoleUser, input))

		for {
			// 路由：选择模型
			provider, modelName, err := a.router.Route(ctx, input)
			if err != nil {
				events <- Event{Type: EventStatus, Content: "error: " + err.Error()}
				return
			}

			// 构建请求
			systemPrompt, _ := a.buildSystemPrompt(ctx)
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
			events <- Event{Type: EventStatus, Content: "thinking"}
			ch, err := provider.Complete(ctx, req)
			if err != nil {
				events <- Event{Type: EventStatus, Content: "error: " + err.Error()}
				return
			}

			var fullContent string
			var toolCalls []model.ToolCall
			var lastUsage *model.Usage

			for chunk := range ch {
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
					break
				}
			}

			if fullContent != "" || len(toolCalls) == 0 {
				a.working.AddMessage(model.NewTextMessage(model.RoleAssistant, fullContent))
			}

			if len(toolCalls) == 0 {
				doneEvt := Event{Type: EventStatus, Content: "done"}
				if lastUsage != nil {
					doneEvt.InputTokens = lastUsage.InputTokens
					doneEvt.OutputTokens = lastUsage.OutputTokens
					doneEvt.CachedTokens = lastUsage.CachedTokens
				}
				events <- doneEvt
				go a.extractMemories(ctx, input, fullContent)
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
				params := model.ParseToolCallArguments(tc.Arguments)
				events <- Event{Type: EventToolCall, ToolName: tc.Name, ToolParams: params}

				result := a.executeTool(ctx, tc.Name, params, events)

				events <- Event{Type: EventToolResult, ToolResult: result.Content, ToolError: result.IsError}

				a.working.AddMessage(model.Message{
					Role:        model.RoleTool,
					ToolCallID:  tc.ID,
					TextContent: result.Content,
					Content:     []model.ContentBlock{{Type: model.ContentText, Text: result.Content}},
				})
			}

			// 上下文压缩检查
			contextWindow := provider.ContextWindow()
			if a.compressor.ShouldCompress(a.working.Messages(), contextWindow) {
				compressed, _, compErr := a.compressor.CompressHistory(ctx, a.working.Messages())
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
	a.sessionID = uuid.New().String()
	a.guard = guard.NewGuard(a.store.DB(), a.sessionID)
	a.working.Clear()
}

// ListModels 返回所有已配置的模型名称
func (a *Agent) ListModels() []string {
	return a.router.ListProviders()
}

// SearchMemory 搜索情景记忆
func (a *Agent) SearchMemory(ctx context.Context, query string, limit int) ([]*memory.EpisodicMemory, error) {
	return a.episodic.SearchFTS(ctx, query, limit)
}

// Compact 手动触发上下文压缩
func (a *Agent) Compact(ctx context.Context) (int, int, error) {
	before := a.working.EstimatedTokens()
	// 使用 default provider 的 context window
	_, _, err := a.router.Route(ctx, "")
	if err != nil {
		return 0, 0, err
	}
	compressed, _, compErr := a.compressor.CompressHistory(ctx, a.working.Messages())
	if compErr != nil {
		return 0, 0, compErr
	}
	a.working.SetMessages(compressed)
	after := a.working.EstimatedTokens()
	return before, after, nil
}

// Close 释放所有资源
func (a *Agent) Close() {
	if a.store != nil {
		a.store.Close()
	}
}

// === 内部实现 ===

// executeTool 执行单个工具调用
// 包含敏感文件拦截和内容脱敏逻辑：
//   - readfile: 拦截敏感路径，脱敏输出内容
//   - exec: 脱敏输出内容
//   - 所有工具结果都经过内容脱敏
func (a *Agent) executeTool(ctx context.Context, name string, params map[string]any, events chan<- Event) tool.Result {
	// AskUser：向调用方请求用户输入
	if name == "askuser" {
		question, _ := params["question"].(string)
		if question == "" {
			return tool.ErrorResult("question is required")
		}
		var options []string
		if o, ok := params["options"].([]any); ok {
			for _, v := range o {
				if s, ok := v.(string); ok {
					options = append(options, s)
				}
			}
		}
		replyCh := make(chan string, 1)
		events <- Event{Type: EventAskUser, Question: question, Options: options, Reply: replyCh}
		select {
		case <-ctx.Done():
			return tool.ErrorResult("cancelled")
		case answer := <-replyCh:
			b, _ := json.Marshal(map[string]string{"answer": answer})
			return tool.TextResult(string(b))
		}
	}

	// Spawn：创建子 agent
	if name == "spawn" {
		return a.executeSpawn(ctx, params)
	}

	// 查找注册工具
	t, ok := a.registry.Get(name)
	if !ok {
		return tool.ErrorResult(fmt.Sprintf("tool %q not found", name))
	}

	// 敏感文件路径拦截：readfile 读取敏感路径直接拒绝
	if name == "readfile" {
		if path, ok := params["path"].(string); ok {
			if sensitive, reason := guard.IsSensitivePath(path); sensitive {
				return tool.ErrorResult(fmt.Sprintf("blocked: sensitive file (%s). Reading credential/secret files is not allowed.", reason))
			}
		}
	}

	// Act 工具经过 Guard 审查
	if t.Category() == tool.Act {
		result := a.guard.Check(ctx, name, params)
		if result.Decision == guard.Reject {
			return tool.ErrorResult("blocked: " + result.Reason)
		}
	}

	result := t.Execute(ctx, params)

	// 内容脱敏：所有工具输出都经过敏感信息脱敏
	if !result.IsError {
		result.Content = guard.MaskSensitiveContent(result.Content)
	}

	return result
}

// executeSpawn 创建子 agent 执行子任务
func (a *Agent) executeSpawn(ctx context.Context, params map[string]any) tool.Result {
	task, _ := params["task"].(string)
	if task == "" {
		return tool.ErrorResult("task is required")
	}

	timeout := 300
	if t, ok := params["timeout"].(float64); ok && int(t) > 0 {
		timeout = int(t)
	}

	var toolNames []string
	if t, ok := params["tools"].([]any); ok {
		for _, v := range t {
			if s, ok := v.(string); ok {
				toolNames = append(toolNames, s)
			}
		}
	}
	if len(toolNames) == 0 {
		toolNames = []string{"readfile", "listdir", "readhttp", "exec"}
	}

	for _, t := range toolNames {
		if t == "spawn" {
			return tool.ErrorResult("sub agent cannot spawn (nesting not allowed)")
		}
	}

	subRegistry := tool.NewRegistry()
	for _, name := range toolNames {
		if t, ok := a.registry.Get(name); ok {
			subRegistry.Register(t)
		}
	}

	sessionID := uuid.New().String()
	sub := &Agent{
		cfg:       a.cfg,
		router:    a.router,
		registry:  subRegistry,
		guard:     guard.NewGuard(nil, sessionID),
		working:   memory.NewWorkingMemory(),
		episodic:  a.episodic,
		semantic:  a.semantic,
		prompts:   a.prompts,
		sessionID: sessionID,
	}

	if sys, ok := params["system"].(string); ok && sys != "" {
		sub.working.AddMessage(model.NewTextMessage(model.RoleSystem, sys))
	}
	if extra, ok := params["context"].(string); ok && extra != "" {
		sub.working.AddMessage(model.NewTextMessage(model.RoleSystem, "Additional context:\n"+extra))
	}

	subCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	subEvents := sub.Run(subCtx, task)
	// 排空子 agent 事件
	for range subEvents {
	}

	msgs := sub.working.Messages()
	var result string
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == model.RoleAssistant && msgs[i].Text() != "" {
			result = msgs[i].Text()
			break
		}
	}

	out, _ := json.Marshal(map[string]any{"result": result, "success": true})
	return tool.TextResult(string(out))
}

// buildSystemPrompt 通过模板渲染系统提示词
func (a *Agent) buildSystemPrompt(ctx context.Context) (string, error) {
	env := getEnvInfo()

	soulMD := ""
	homeDir, _ := os.UserHomeDir()
	if data, err := os.ReadFile(filepath.Join(homeDir, ".suna", "SOUL.md")); err == nil {
		soulMD = string(data)
	}

	projectConfig := ""
	wd, _ := os.Getwd()
	for _, name := range []string{"SUNA.md", ".suna/AGENTS.md"} {
		if data, err := os.ReadFile(filepath.Join(wd, name)); err == nil {
			projectConfig = string(data)
			break
		}
	}

	userPrefs := ""
	if summary, err := a.semantic.Summary(ctx); err == nil && summary != "" {
		userPrefs = summary
	}

	return a.prompts.RenderSystem(prompt.SystemPromptData{
		OS:              env["OS"],
		Arch:            env["Arch"],
		WorkDir:         env["WorkDir"],
		User:            env["User"],
		Time:            env["Time"],
		SoulMD:          soulMD,
		ProjectConfig:   projectConfig,
		UserPreferences: userPrefs,
	})
}

// buildToolDefs 构建 LLM tool calling 定义
// AskUser 和 Spawn 不在 registry 里，动态追加
func (a *Agent) buildToolDefs() []model.ToolDef {
	tools := a.registry.All()
	defs := make([]model.ToolDef, 0, len(tools)+2)

	for _, t := range tools {
		defs = append(defs, model.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		})
	}

	defs = append(defs, model.ToolDef{
		Name:        "askuser",
		Description: "Ask the user a question and wait for their reply",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"question": map[string]any{"type": "string", "description": "Question to ask"},
				"options":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Options"},
			},
			"required": []string{"question"},
		},
	})

	defs = append(defs, model.ToolDef{
		Name:        "spawn",
		Description: "Create a sub agent to execute a sub-task",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task":    map[string]any{"type": "string", "description": "Sub-task description"},
				"model":   map[string]any{"type": "string", "description": "Model to use"},
				"system":  map[string]any{"type": "string", "description": "Sub agent system prompt"},
				"tools":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Available tools"},
				"timeout": map[string]any{"type": "integer", "description": "Timeout seconds"},
				"context": map[string]any{"type": "string", "description": "Extra context"},
			},
			"required": []string{"task"},
		},
	})

	return defs
}

// extractMemories 仅添加式提取记忆，不做覆盖/删除
func (a *Agent) extractMemories(ctx context.Context, userInput, agentOutput string) {
	if a.episodic == nil {
		return
	}
	content := fmt.Sprintf("User: %s\nAssistant: %s", userInput, agentOutput)
	a.episodic.Store(ctx, &memory.EpisodicMemory{
		Content:   content,
		Type:      "interaction",
		Source:    "auto_extract",
		SessionID: a.sessionID,
	})
}

func getEnvInfo() map[string]string {
	wd, _ := os.Getwd()
	username := "unknown"
	if u, err := user.Current(); err == nil {
		username = u.Username
	}
	return map[string]string{
		"OS":      runtime.GOOS,
		"Arch":    runtime.GOARCH,
		"WorkDir": wd,
		"User":    username,
		"Time":    time.Now().Format("2006-01-02 15:04:05"),
	}
}

func resolveModelID(cfg *config.Config, modelName string) string {
	if mc, ok := cfg.Models[modelName]; ok {
		return mc.Model
	}
	if mc, ok := cfg.Models["default"]; ok {
		return mc.Model
	}
	return modelName
}
