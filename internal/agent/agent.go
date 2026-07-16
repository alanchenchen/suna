package agent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/guard"
	"github.com/alanchenchen/suna/internal/logging"
	"github.com/alanchenchen/suna/internal/mcp"
	"github.com/alanchenchen/suna/internal/media"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/prompt"
	"github.com/alanchenchen/suna/internal/runner"
	"github.com/alanchenchen/suna/internal/skill"
	"github.com/alanchenchen/suna/internal/tools"
	"github.com/alanchenchen/suna/internal/tools/agenttools"
	"github.com/alanchenchen/suna/internal/tools/builtin"
	"github.com/alanchenchen/suna/internal/tools/mcptools"
	"github.com/alanchenchen/suna/internal/tools/skilltools"
)

type Agent struct {
	// runtime 指向共享全局运行时；nil 表示当前对象就是运行时根 Agent。
	runtime      *Agent
	cfg          *config.Config
	router       *model.Router
	tools        *tools.Manager
	guard        *guard.Guard
	working      *memory.WorkingMemory
	usage        *memory.UsageStore
	sessionStore *memory.SessionStore
	stateStore   *memory.SessionStateStore
	memories     *memory.MemoryStore
	mediaStore   *media.Store
	compressor   *memory.Compressor
	calibrator   *model.TokenCalibrator
	prompts      *prompt.Loader
	store        *memory.Store
	skills       *skill.Runtime
	mcp          *mcp.Runtime
	sessionID    string
	cwd          string
	modelRef     string
	turnCount    int
	sessionState string
	toolSummary  memory.ToolSummary

	extractQueue  *memory.ExtractQueue
	extractWorker *memory.Worker
	closeOnce     sync.Once
	closed        bool
	configMu      sync.RWMutex
	configModTime time.Time
	runMu         sync.Mutex
	// currentInputBlocks 只在单次 Agent.Run 内保存当前用户消息的轻量媒体引用，供 spawn.input_images 显式转交给 subtask。
	// 这里不能保存到跨轮状态；Run 结束必须清空，避免附件引用被误当作历史上下文继续使用。
	currentInputBlocks []model.ContentBlock

	cancelMu sync.Mutex
	cancelFn context.CancelFunc
}

func NewAgent(cfg *config.Config) (*Agent, error) {
	var router *model.Router
	mediaStore := media.NewStore(cfg.AttachmentsDir())
	resolver := media.NewContextResolver(cfg.AttachmentsDir())
	if len(cfg.Models) > 0 {
		var err error
		router, err = model.NewRouter(cfg, resolver)
		if err != nil {
			return nil, fmt.Errorf("init router: %w", err)
		}
	}

	store, err := memory.NewStore(cfg.DBPath())
	if err != nil {
		return nil, fmt.Errorf("init memory store: %w", err)
	}

	sessionID := uuid.New().String()
	skillRuntime := skill.NewRuntime(cfg.SkillsDir(), cfg)
	skillRuntime.SetReviewer(agentSkillReviewer{})
	skillRuntime.SetPrompter(agentSkillPrompter{})
	skillRuntime.Reload(context.Background())

	toolManager := tools.NewManager()
	toolManager.RegisterProvider(builtin.NewProvider())
	toolManager.RegisterProvider(skilltools.NewProvider(skillRuntime))
	mcpRuntime := mcp.NewRuntime(cfg.MCP)
	if err := mcpRuntime.Start(context.Background()); err != nil {
		return nil, fmt.Errorf("init mcp: %w", err)
	}
	toolManager.RegisterProvider(mcptools.NewProvider(mcpRuntime, cfg.AttachmentsDir()))
	prompts, err := prompt.New()
	if err != nil {
		return nil, fmt.Errorf("init prompts: %w", err)
	}

	usage := memory.NewUsageStore(store.DB())
	sessionStore := memory.NewSessionStore(store.DB())
	stateStore := memory.NewSessionStateStore(store.DB())
	memories := memory.NewMemoryStore(store.DB())

	extractQueue := memory.NewExtractQueue(store.DB())
	extractWorker := memory.NewWorker(extractQueue, memories, store.DB(), func(ref string) (*model.ModelBinding, error) {
		if router == nil {
			return nil, fmt.Errorf("model router is not configured")
		}
		return router.Bind(ref)
	})

	agent := &Agent{
		cfg:           cfg,
		router:        router,
		tools:         toolManager,
		working:       memory.NewWorkingMemory(),
		usage:         usage,
		sessionStore:  sessionStore,
		stateStore:    stateStore,
		memories:      memories,
		mediaStore:    mediaStore,
		guard:         guard.NewGuard(nil, sessionID),
		compressor:    memory.NewCompressor(),
		calibrator:    model.NewTokenCalibrator(),
		prompts:       prompts,
		store:         store,
		skills:        skillRuntime,
		mcp:           mcpRuntime,
		sessionID:     sessionID,
		cwd:           mustGetwd(),
		extractQueue:  extractQueue,
		extractWorker: extractWorker,
	}
	toolManager.RegisterProvider(agenttools.NewProvider(agent))
	if err := toolManager.Reload(context.Background()); err != nil {
		return nil, fmt.Errorf("init agent tools: %w", err)
	}
	agent.guard = agent.newGuardForSession(sessionID)
	if info, err := os.Stat(cfg.ConfigPath()); err == nil {
		agent.configModTime = info.ModTime()
	}
	agent.compressor.SetPrompts(prompts)
	agent.extractWorker.SetPrompts(prompts)
	agent.materializeLegacyPendingMemoryQueueModelRef(cfg, router)

	go extractWorker.Run()
	extractQueue.RecoverUnextracted(context.Background())
	return agent, nil
}

func (a *Agent) BindModel(ref string) (*model.ModelBinding, error) {
	if a == nil {
		return nil, fmt.Errorf("model router is not configured")
	}
	root := a.root()
	root.configMu.RLock()
	router := root.router
	root.configMu.RUnlock()
	if router == nil {
		return nil, fmt.Errorf("model router is not configured")
	}
	return router.Bind(ref)
}

func (a *Agent) Run(ctx context.Context, input Input) <-chan Event {
	a.syncRuntime()
	events := make(chan Event, eventBuffer)
	if !a.runMu.TryLock() {
		events <- Event{Type: EventStatus, Content: "agent is already running", Error: true}
		close(events)
		return events
	}

	runCtx, cancel := context.WithCancel(ctx)
	a.cancelMu.Lock()
	a.cancelFn = cancel
	a.cancelMu.Unlock()

	go func() {
		defer a.finishRun(events, cancel)

		if a.router == nil {
			events <- Event{Type: EventStatus, Error: true, RunError: a.modelUnavailableRunError()}
			return
		}

		userMessage := input.Message(model.RoleUser)
		storedUserMessage := input.StoredMessage(model.RoleUser)
		inputText := userMessage.Text()
		if len(userMessage.Content) == 0 {
			events <- Event{Type: EventStatus, Content: "input is required", Error: true}
			return
		}

		a.working.AddMessage(userMessage)
		a.currentInputBlocks = cloneContentBlocks(userMessage.Content)
		// 多模态 raw media 只允许参与当前 agent run；run 结束后立即替换为轻量 metadata，避免进入下一轮上下文或会话快照。
		defer func() {
			a.currentInputBlocks = nil
			a.replaceLastUserMessage(inputText, storedUserMessage)
			a.saveConversationState(runCtx)
		}()
		a.turnCount++
		a.enqueueMemoryEvent(runCtx, model.RoleUser, inputText, false, false, false, false)

		a.runCurrentWorking(runCtx, inputText, events)
	}()
	return events
}

func (a *Agent) ResumeRun(ctx context.Context) <-chan Event {
	a.syncRuntime()
	// resume 只恢复未完成的当前 turn，不新增 user message；用于模型/服务中断后的干净重试。
	events := make(chan Event, eventBuffer)
	if !a.runMu.TryLock() {
		events <- Event{Type: EventStatus, Content: "agent is already running", Error: true}
		close(events)
		return events
	}

	runCtx, cancel := context.WithCancel(ctx)
	a.cancelMu.Lock()
	a.cancelFn = cancel
	a.cancelMu.Unlock()

	go func() {
		defer a.finishRun(events, cancel)
		defer a.saveConversationState(runCtx)

		if a.router == nil {
			events <- Event{Type: EventStatus, Error: true, RunError: a.modelUnavailableRunError()}
			return
		}
		if !a.canResumeRunLocked() {
			events <- Event{Type: EventStatus, Content: "no resumable run", Error: true}
			return
		}

		inputText := a.lastUserTextLocked()
		a.runCurrentWorking(runCtx, inputText, events)
	}()
	return events
}

func (a *Agent) finishRun(events chan Event, cancel context.CancelFunc) {
	a.cancelMu.Lock()
	a.cancelFn = nil
	a.cancelMu.Unlock()
	cancel()
	close(events)
	a.runMu.Unlock()
}

func (a *Agent) modelUnavailableRunError() *RunError {
	if modelRef := a.modelRef; modelRef != "" {
		return &RunError{Kind: RunErrorSessionModelUnavailable, ModelRef: modelRef}
	}
	return &RunError{Kind: RunErrorNoModelConfigured}
}

func (a *Agent) runCurrentWorking(runCtx context.Context, inputText string, events chan<- Event) {
	runCtx = tools.WithExecutionContext(runCtx, tools.ExecutionContext{SessionID: a.sessionID, CWD: a.cwd, AttachmentDir: a.attachmentRoot()})
	modelRef := a.modelRef
	if a.router == nil {
		events <- Event{Type: EventStatus, Error: true, RunError: a.modelUnavailableRunError()}
		return
	}
	if modelRef == "" {
		// 正常 session 在 create 或 legacy attach 时已固化 model_ref；此处仅处理损坏持久化数据。
		events <- Event{Type: EventStatus, Error: true, RunError: &RunError{Kind: RunErrorNoModelConfigured}}
		return
	}
	binding, err := a.router.Bind(modelRef)
	if err != nil {
		logging.Error("agent", "bind_model_failed", err, logging.Event{"session_id": a.sessionID, "model_ref": modelRef})
		var bindingErr *model.BindingError
		if errors.As(err, &bindingErr) {
			runErr := &RunError{Kind: RunErrorNoModelConfigured}
			if bindingErr.Kind == model.BindingErrorModelNotFound {
				runErr = &RunError{Kind: RunErrorSessionModelUnavailable, ModelRef: modelRef}
			}
			events <- Event{Type: EventStatus, Error: true, RunError: runErr}
			return
		}
		events <- Event{Type: EventStatus, Content: err.Error(), Error: true}
		return
	}
	runCtx = model.WithBinding(runCtx, binding)
	systemPrompt, _ := a.buildSystemPrompt(runCtx)

	r := a.newRunner(events)
	res, err := r.Run(runCtx, runner.Request{
		Binding:       binding,
		System:        systemPrompt,
		Working:       a.working,
		Messages:      a.buildRequestMessages,
		ToolDefs:      a.buildToolDefs,
		EmitStream:    true,
		EmitReasoning: true,
		AutoCompress:  true,
		SessionState:  a.sessionState,
	})
	if err != nil {
		content := err.Error()
		modelErr := model.NewModelError(err)
		resumeAvailable := a.canResumeRunLocked()
		if runCtx.Err() != nil {
			content = "cancelled"
			modelErr = &model.ModelError{Kind: model.ModelErrorCancelled, Message: content}
			resumeAvailable = false
		}
		events <- Event{Type: EventStatus, Content: content, Error: true, ResumeAvailable: resumeAvailable, ModelError: modelErr}
		return
	}

	a.sessionState = res.SessionState
	// 长期用户画像只从用户信号提取；assistant 输出属于当前会话结果，应进入 Session State/recent，而不是跨会话画像。
	events <- Event{Type: EventStatus, Status: StatusDone, ContextWindow: res.ContextWindow}
}

func (a *Agent) CanResumeRun() bool {
	if a == nil || a.working == nil {
		return false
	}
	return a.canResumeRunLocked()
}

func (a *Agent) canResumeRunLocked() bool {
	msgs := a.working.Messages()
	if len(msgs) == 0 {
		return false
	}
	switch msgs[len(msgs)-1].Role {
	case model.RoleUser, model.RoleTool:
		return true
	default:
		return false
	}
}

func (a *Agent) lastUserTextLocked() string {
	msgs := a.working.Messages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == model.RoleUser {
			return msgs[i].Text()
		}
	}
	return ""
}

func (a *Agent) newRunner(events chan<- Event) *runner.Runner {
	return &runner.Runner{
		Compressor: a.compressor,
		Calibrator: a.calibrator,
		Executor:   mainExecutor{agent: a, events: events},
		Sink:       eventSink{events: events},
		UsageSink:  a,
		Hooks: runner.Hooks{
			CleanToolParams: a.cleanToolParams,
			OnToolResult:    a.addToolSummary,
			OnCompactCommit: a.commitCompactState,
		},
	}
}

func (a *Agent) Skills() *skill.Runtime { return a.skills }
func (a *Agent) MCP() *mcp.Runtime      { return a.mcp }

// ReloadTools 刷新 agent 暴露给模型的工具目录；MCP 运行态启停/重载后必须调用，
// 否则下一轮请求仍可能使用旧的 tool schema。
func (a *Agent) ReloadTools(ctx context.Context) error {
	if a == nil || a.tools == nil {
		return nil
	}
	return a.tools.Reload(ctx)
}

func (a *Agent) RecordUsage(ctx context.Context, modelID string, usage *model.Usage) {
	if a.usage == nil || usage == nil {
		return
	}
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err := a.usage.SaveUsage(saveCtx, a.sessionID, modelID, usage.InputTokens, usage.OutputTokens); err != nil {
		logging.Error("agent", "save_usage_failed", err, logging.Event{"session_id": a.sessionID, "model": modelID})
	}
}

func (a *Agent) newGuardForSession(sessionID string) *guard.Guard {
	if a.cfg == nil {
		return guard.NewGuard(nil, sessionID)
	}
	mode := guard.NormalizeMode(a.cfg.Guard.ModeOrDefault())
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
	var db *sql.DB
	if a.store != nil {
		db = a.store.DB()
	}
	g := guard.NewGuardWithConfigModeAndWorkspace(db, sessionID, mode, a.cfg.Guard.Workspace, blockedPats, blockedReasons, allowedPats, allowedTools)
	if a.router != nil && mode == guard.ModeSmart {
		g.SetLLMReviewer(a.guardLLMReview)
	}
	return g
}

type eventSink struct {
	events chan<- Event
}

func (s eventSink) Status(status runner.StatusEvent) {
	// runner 只描述模型循环内部状态；agent 在这里转换成自身事件，避免 daemon 越过 agent 依赖 runner 细节。
	switch status.Kind {
	case runner.StatusCompactRunning:
		s.events <- Event{Type: EventStatus, Status: StatusCompactRunning}
	case runner.StatusCompactDone:
		s.events <- Event{Type: EventStatus, Status: StatusCompactDone}
	case runner.StatusCompactError:
		s.events <- Event{Type: EventStatus, Status: StatusCompactError, Content: status.Message}
	case runner.StatusWaitingLLM:
		s.events <- Event{Type: EventStatus, Status: StatusWaitingLLM}
	case runner.StatusLLMRetrying:
		s.events <- Event{Type: EventStatus, Status: StatusLLMRetrying, Content: status.Message, Attempt: status.Attempt, MaxAttempts: status.MaxAttempts, DelayMs: status.Delay.Milliseconds(), ModelError: status.Error}
	}
}
func (s eventSink) Stream(content string) { s.events <- Event{Type: EventStream, Content: content} }
func (s eventSink) Reasoning(content string) {
	s.events <- Event{Type: EventReasoning, Content: content}
}
func (s eventSink) Usage(usage runner.UsageEvent) {
	s.events <- Event{
		Type:                   EventUsage,
		InputTokens:            usage.InputTokens,
		OutputTokens:           usage.OutputTokens,
		CachedTokens:           usage.CachedTokens,
		ContextTokens:          usage.ContextTokens,
		EstimatedContextTokens: usage.EstimatedContextTokens,
		ContextWindow:          usage.ContextWindow,
		DurationMs:             usage.Duration.Milliseconds(),
	}
}
func (s eventSink) ToolCall(call runner.ToolCallEvent) {
	s.events <- Event{Type: EventToolCall, ToolCallID: call.ID, ToolName: call.Name, ToolParams: call.Params, ToolIntent: call.Intent}
}
func (s eventSink) ToolResult(result runner.ToolResultEvent) {
	s.events <- Event{Type: EventToolResult, ToolCallID: result.ID, ToolName: result.Name, ToolResult: result.Result, ToolError: result.Error, ToolMetadata: result.Metadata}
}
