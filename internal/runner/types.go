package runner

import (
	"context"
	"time"

	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/tools"
)

type Request struct {
	Binding       *model.ModelBinding
	Invocation    model.Invocation
	System        string
	Purpose       string
	Working       *memory.WorkingMemory
	Memory        *ContextMemory
	Project       func(context.Context) func([]memory.Message) []model.Message
	ToolDefs      func() []model.ToolDef
	MaxTokens     int
	StreamTimeout time.Duration

	EmitStream    bool
	EmitReasoning bool
	AutoCompress  bool

	MaxTurns     int
	MaxToolCalls int

	// TakeSteering 在模型请求之间的安全边界取出排队用户消息；seal=true 表示候选终态，队列为空时原子封闭。
	TakeSteering func(seal bool) []SteeringInput
	// SealSteering 在无法继续物理模型请求时原子封闭队列；返回是否仍有待应用消息。
	SealSteering func() bool
}

type SteeringInput struct {
	ID          string
	ClientMsgID string
	Sequence    uint64
	Message     memory.Message
}

type Result struct {
	FinalText     string
	HadToolCall   bool
	HadToolError  bool
	ContextWindow int
	Usage         *model.Usage
}

type StatusKind string

const (
	StatusWaitingLLM  StatusKind = "waiting_llm"
	StatusLLMRetrying StatusKind = "llm_retrying"
)

type StatusEvent struct {
	Kind    StatusKind
	Message string
	Attempt int
	Delay   time.Duration
	Elapsed time.Duration
	Budget  time.Duration
	Error   *model.ModelError
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
	InputTokens            int
	OutputTokens           int
	CacheReadTokens        int
	CacheCreationTokens    int
	ContextTokens          int
	EstimatedContextTokens int
	ContextWindow          int
	Duration               time.Duration
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
	CleanToolParams   func(name string, params map[string]any) (map[string]any, string)
	Checkpoint        func(context.Context) error
	CommitMemory      func(context.Context, string, []memory.Message) error
	OnAssistantSaved  func(context.Context)
	OnSteeringApplied func(context.Context, SteeringInput)
}

type Runner struct {
	Compressor  *memory.Compressor
	Executor    ToolExecutor
	Sink        EventSink
	UsageSink   UsageSink
	Hooks       Hooks
	RetryPolicy RetryPolicy
	// Calibrator 跨多次 Run 复用，用模型真实 input token 校准本地估算；
	// 由 Agent 注入共享实例，nil 时压缩判断退化为未校准行为。
	Calibrator *model.TokenCalibrator
	// toolImages 暂存本轮工具产出的图片块，注入 user 消息后清空；只存活于单次 Run，
	// 不参与并发（Runner 单 goroutine 执行工具结果回填）。
	toolImages []model.ContentBlock
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
