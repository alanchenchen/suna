package runner

import (
	"context"
	"time"

	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/tools"
)

type Request struct {
	System        string
	ModelRef      string
	ModelID       string
	Purpose       string
	Working       *memory.WorkingMemory
	Messages      func(context.Context) []model.Message
	ToolDefs      func() []model.ToolDef
	MaxTokens     int
	StreamTimeout time.Duration

	EmitStream    bool
	EmitReasoning bool
	AutoCompress  bool
	SessionState  string

	MaxTurns     int
	MaxToolCalls int

	Retry RetryPolicy
}

type Result struct {
	FinalText     string
	HadToolCall   bool
	HadToolError  bool
	ContextWindow int
	SessionState  string
	Usage         *model.Usage
}

type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Multiplier  float64
	Jitter      bool
}

type StatusKind string

const (
	StatusCompactRunning StatusKind = "compact_running"
	StatusCompactDone    StatusKind = "compact_done"
	StatusCompactError   StatusKind = "compact_error"
	StatusWaitingLLM     StatusKind = "waiting_llm"
)

type StatusEvent struct {
	Kind    StatusKind
	Message string
}

type EventSink interface {
	Status(status StatusEvent)
	Stream(content string)
	Reasoning(content string)
	Usage(usage UsageEvent)
	ToolCall(call ToolCallEvent)
	ToolResult(result ToolResultEvent)
}

type ToolCallEvent struct {
	ID     string
	Name   string
	Params map[string]any
	Intent string
}

type ToolResultEvent struct {
	ID       string
	Name     string
	Result   string
	Error    bool
	Metadata map[string]any
}

type UsageEvent struct {
	InputTokens   int
	OutputTokens  int
	CachedTokens  int
	ContextTokens int
	ContextWindow int
	Duration      time.Duration
}

// ToolExecution 是一次具体工具调用的不可变上下文。
// Intent 与 AssistantContext 专供 smart guard review 判断“为什么要调用”。
// WorkingMessages 是当前 runner 的上下文快照；main/subtask 各自传入自己的 working，避免 Guard review 串用上下文。
type ToolExecution struct {
	ID               string
	Name             string
	Params           map[string]any
	Intent           string
	AssistantContext string
	WorkingMessages  []model.Message
}

type ToolExecutor interface {
	ExecuteTool(ctx context.Context, call ToolExecution) tools.Result
}

type UsageSink interface {
	RecordUsage(ctx context.Context, modelID string, usage *model.Usage)
}

type Hooks struct {
	CleanToolParams func(name string, params map[string]any) (map[string]any, string)
	OnAssistantText func(ctx context.Context, content string)
	OnToolResult    func(name string, result tools.Result)
}

type Runner struct {
	Router     *model.Router
	Compressor *memory.Compressor
	Executor   ToolExecutor
	Sink       EventSink
	UsageSink  UsageSink
	Hooks      Hooks
}

type preparedToolCall struct {
	tc               model.ToolCall
	params           map[string]any
	intent           string
	assistantContext string
}

type toolExecResult struct {
	index  int
	tc     model.ToolCall
	result tools.Result
}
