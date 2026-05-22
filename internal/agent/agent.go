package agent

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/alanchenchen/suna/internal/capability"
	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/guard"
	"github.com/alanchenchen/suna/internal/logging"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/prompt"
	"github.com/alanchenchen/suna/internal/runner"
	"github.com/alanchenchen/suna/internal/tool"
)

type Agent struct {
	cfg          *config.Config
	router       *model.Router
	registry     *tool.Registry
	guard        *guard.Guard
	working      *memory.WorkingMemory
	sessions     *memory.SessionStore
	memories     *memory.MemoryStore
	conversation *memory.ConversationStore
	compressor   *memory.Compressor
	prompts      *prompt.Loader
	store        *memory.Store
	caps         *capability.Loader
	sessionID    string
	turnCount    int
	resumeInput  string
	toolSummary  []memory.ToolSummaryItem

	extractQueue  *memory.ExtractQueue
	extractWorker *memory.Worker
	closeOnce     sync.Once
	closed        bool
	configMu      sync.RWMutex
	configModTime time.Time
	runMu         sync.Mutex

	cancelMu sync.Mutex
	cancelFn context.CancelFunc
}

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

	sessions := memory.NewSessionStore(store.DB())
	memories := memory.NewMemoryStore(store.DB())
	conversation := memory.NewConversationStore(store.DB())

	var extractProvider model.Provider
	if router != nil {
		if p, err := router.Provider("fast"); err == nil {
			extractProvider = p
		} else if p := router.DefaultProvider(); p != nil {
			extractProvider = p
		}
	}

	extractQueue := memory.NewExtractQueue(store.DB())
	extractWorker := memory.NewWorker(extractQueue, memories, store.DB(), extractProvider)

	agent := &Agent{
		cfg:           cfg,
		router:        router,
		registry:      registry,
		working:       memory.NewWorkingMemory(),
		sessions:      sessions,
		memories:      memories,
		conversation:  conversation,
		compressor:    memory.NewCompressor(extractProvider),
		prompts:       prompts,
		store:         store,
		caps:          capLoader,
		sessionID:     sessionID,
		extractQueue:  extractQueue,
		extractWorker: extractWorker,
	}
	agent.guard = agent.newGuardForSession(sessionID)
	if info, err := os.Stat(cfg.ConfigPath()); err == nil {
		agent.configModTime = info.ModTime()
	}
	if router != nil {
		router.SetPrompts(prompts)
	}
	agent.compressor.SetPrompts(prompts)
	agent.extractWorker.SetPrompts(prompts)

	go extractWorker.Run()
	extractQueue.RecoverUnextracted(context.Background())
	return agent, nil
}

func (a *Agent) Run(ctx context.Context, input Input) <-chan Event {
	events := make(chan Event, 64)
	if !a.runMu.TryLock() {
		events <- Event{Type: EventStatus, Content: "error: agent is already running"}
		close(events)
		return events
	}

	runCtx, cancel := context.WithCancel(ctx)
	a.cancelMu.Lock()
	a.cancelFn = cancel
	a.cancelMu.Unlock()

	go func() {
		defer a.runMu.Unlock()
		defer close(events)
		defer cancel()
		defer func() {
			a.cancelMu.Lock()
			a.cancelFn = nil
			a.cancelMu.Unlock()
		}()

		if a.router == nil {
			events <- Event{Type: EventStatus, Content: "error: no model configured, please add a model in config"}
			return
		}

		userMessage := input.Message(model.RoleUser)
		inputText := userMessage.Text()
		if len(userMessage.Content) == 0 {
			events <- Event{Type: EventStatus, Content: "error: input is required"}
			return
		}

		a.working.AddMessage(userMessage)
		a.turnCount++
		a.enqueueMemoryEvent(runCtx, model.RoleUser, inputText, false, false, false, false)
		a.saveConversationState(runCtx)

		_, modelRef, err := a.router.Route(runCtx, inputText)
		if err != nil {
			logging.Error("agent", "route_failed", err, logging.Event{"session_id": a.sessionID})
			events <- Event{Type: EventStatus, Content: "error: " + err.Error()}
			return
		}
		systemPrompt, _ := a.buildSystemPrompt(runCtx)
		modelID := resolveModelID(a.cfg, modelRef)

		r := a.newRunner(events)
		res, err := r.Run(runCtx, runner.Request{
			System:        systemPrompt,
			ModelRef:      modelRef,
			ModelID:       modelID,
			Working:       a.working,
			Messages:      a.buildRequestMessages,
			ToolDefs:      a.buildToolDefs,
			EmitStream:    true,
			EmitReasoning: true,
			AutoCompress:  true,
		})
		if err != nil {
			content := "error: " + err.Error()
			if runCtx.Err() != nil {
				content = "cancelled"
			}
			events <- Event{Type: EventStatus, Content: content}
			return
		}

		a.enqueueMemoryEvent(runCtx, model.RoleAssistant, res.FinalText, res.HadToolCall, res.HadToolError, false, false)
		a.saveConversationState(runCtx)
		done := Event{Type: EventStatus, Content: "done", ContextWindow: res.ContextWindow}
		if res.Usage != nil {
			done.HasUsage = true
			done.InputTokens = res.Usage.InputTokens
			done.OutputTokens = res.Usage.OutputTokens
			done.CachedTokens = res.Usage.CachedTokens
			done.ContextTokens = res.Usage.TotalTokens
			if done.ContextTokens <= 0 {
				done.ContextTokens = done.InputTokens + done.OutputTokens
			}
		}
		events <- done
	}()
	return events
}

func (a *Agent) newRunner(events chan<- Event) *runner.Runner {
	return &runner.Runner{
		Router:     a.router,
		Compressor: a.compressor,
		Executor:   mainExecutor{agent: a, events: events},
		Sink:       eventSink{events: events},
		UsageSink:  a,
		Hooks: runner.Hooks{
			CleanToolParams: a.cleanToolParams,
			OnToolResult:    a.addToolSummary,
			Capabilities:    a.caps,
		},
	}
}

func (a *Agent) RecordUsage(ctx context.Context, modelID string, usage *model.Usage) {
	if a.sessions == nil || usage == nil {
		return
	}
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err := a.sessions.SaveUsage(saveCtx, a.sessionID, modelID, usage.InputTokens, usage.OutputTokens, 0); err != nil {
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
	g := guard.NewGuardWithConfigAndMode(db, sessionID, mode, blockedPats, blockedReasons, allowedPats, allowedTools)
	if a.router != nil && mode == guard.ModeSmart {
		g.SetLLMReviewer(a.guardLLMReview)
	}
	return g
}

type eventSink struct {
	events chan<- Event
}

func (s eventSink) Status(content string) { s.events <- Event{Type: EventStatus, Content: content} }
func (s eventSink) Stream(content string) { s.events <- Event{Type: EventStream, Content: content} }
func (s eventSink) Reasoning(content string) {
	s.events <- Event{Type: EventReasoning, Content: content}
}
func (s eventSink) ToolCall(call runner.ToolCallEvent) {
	s.events <- Event{Type: EventToolCall, ToolCallID: call.ID, ToolName: call.Name, ToolParams: call.Params, ToolIntent: call.Intent}
}
func (s eventSink) ToolResult(result runner.ToolResultEvent) {
	s.events <- Event{Type: EventToolResult, ToolCallID: result.ID, ToolName: result.Name, ToolResult: result.Result, ToolError: result.Error}
}
