package runner

import (
	"context"
	"time"

	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/tool"
)

type Request struct {
	System        string
	ModelRef      string
	ModelID       string
	Purpose       string
	Working       *memory.WorkingMemory
	Messages      func(context.Context) []model.Message
	Tools         *tool.Registry
	ToolDefs      func() []model.ToolDef
	MaxTokens     int
	StreamTimeout time.Duration

	EmitStream    bool
	EmitReasoning bool
	AutoCompress  bool

	MaxTurns     int
	MaxToolCalls int

	Retry RetryPolicy
}

type Result struct {
	FinalText     string
	HadToolCall   bool
	HadToolError  bool
	ContextWindow int
	Usage         *model.Usage
}

type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Multiplier  float64
	Jitter      bool
}

type EventSink interface {
	Status(content string)
	Stream(content string)
	Reasoning(content string)
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
	ID     string
	Name   string
	Result string
	Error  bool
}

type ToolExecutor interface {
	ExecuteTool(ctx context.Context, id string, name string, params map[string]any) tool.Result
}

type UsageSink interface {
	RecordUsage(ctx context.Context, modelID string, usage *model.Usage)
}

type Capabilities interface {
	ProcessLoadMarkers(content string) (string, []string)
	LoadSkill(name string) (string, bool)
}

type Hooks struct {
	CleanToolParams func(name string, params map[string]any) (map[string]any, string)
	OnAssistantText func(ctx context.Context, content string)
	OnToolResult    func(name string, result tool.Result)
	Capabilities    Capabilities
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
	tc     model.ToolCall
	params map[string]any
	intent string
}

type toolExecResult struct {
	index  int
	tc     model.ToolCall
	result tool.Result
}
