package agenttools

import (
	"context"

	"github.com/alanchenchen/suna/internal/tools"
)

const (
	ToolAskUser = "askuser"
	ToolSpawn   = "spawn"
)

type eventContextKey struct{}

// WithEvents 把 Agent 当前事件流挂到 context，agenttools 不依赖 agent.Event 类型，避免包循环。
func WithEvents(ctx context.Context, events any) context.Context {
	return context.WithValue(ctx, eventContextKey{}, events)
}

func Events(ctx context.Context) any {
	return ctx.Value(eventContextKey{})
}

// Runtime 由 Agent 实现。agenttools 只负责 schema/路由，不持有 Agent 具体类型。
type Runtime interface {
	ExecuteAskUserTool(ctx context.Context, params map[string]any) tools.Result
	ExecuteSpawnTool(ctx context.Context, id string, params map[string]any) tools.Result
}

type Provider struct {
	runtime Runtime
}

func NewProvider(runtime Runtime) *Provider {
	return &Provider{runtime: runtime}
}

func (p *Provider) Specs(ctx context.Context) ([]tools.Spec, error) {
	return p.SpecsWithCatalog(ctx, nil)
}

func (p *Provider) SpecsWithCatalog(ctx context.Context, catalog []tools.Spec) ([]tools.Spec, error) {
	return []tools.Spec{
		askUserSpec(),
		spawnSpec(spawnToolNames(catalog)),
	}, nil
}

func (p *Provider) Execute(ctx context.Context, call tools.Call) (tools.Result, bool) {
	if p == nil || p.runtime == nil {
		return tools.ErrorResult("agent runtime tools are not initialized"), call.Name == ToolAskUser || call.Name == ToolSpawn
	}
	switch call.Name {
	case ToolAskUser:
		return p.runtime.ExecuteAskUserTool(ctx, call.Params), true
	case ToolSpawn:
		return p.runtime.ExecuteSpawnTool(ctx, call.ID, call.Params), true
	default:
		return tools.Result{}, false
	}
}

func (p *Provider) Close(ctx context.Context) error { return nil }

func askUserSpec() tools.Spec {
	return tools.Spec{
		Name:        ToolAskUser,
		Description: "Ask the user for missing information or a decision. Provide several concise options when helpful. Keep allow_custom=true or omit it for normal questions so the user can type freely; use allow_custom=false only for strict system/workflow confirmations that must choose one provided option.",
		Category:    tools.Act,
		Source:      tools.Source{Kind: tools.SourceAgent, ID: "runtime"},
		Guard:       tools.GuardNever,
		Parameters: map[string]any{"type": "object", "properties": map[string]any{
			"question":     map[string]any{"type": "string", "description": "Question to ask the user"},
			"options":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional quick-pick answers for the user"},
			"allow_custom": map[string]any{"type": "boolean", "description": "Whether the user may type a custom answer. Default true."},
		}, "required": []string{"question"}},
	}
}

func spawnSpec(toolNames []string) tools.Spec {
	return tools.Spec{
		Name:        ToolSpawn,
		Description: "Delegate an isolated subtask to a selected model. It sees only task/context, allowed tools, and images explicitly passed with input_images; it does not inherit main history or images.",
		Category:    tools.Act,
		Source:      tools.Source{Kind: tools.SourceAgent, ID: "runtime"},
		Guard:       tools.GuardNever,
		Parameters: map[string]any{"type": "object", "properties": map[string]any{
			"task":         map[string]any{"type": "string", "description": "Self-contained task for the subtask"},
			"model":        map[string]any{"type": "string", "description": "Exact model ref from Available subtask models"},
			"system":       map[string]any{"type": "string", "description": "Optional subtask system prompt"},
			"tools":        map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": toolNames}, "description": "Allowed tools for the isolated subtask; use [] for model-only tasks"},
			"input_images": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "Indexes of images attached to the current user message only, e.g. [0]. Not prior-turn/restored image summaries; spawn does not inherit images unless listed."},
			"context":      map[string]any{"type": "string", "description": "Extra context"},
		}, "required": []string{"task", "model", "tools"}},
	}
}

func spawnToolNames(catalog []tools.Spec) []string {
	names := make([]string, 0, len(catalog))
	for _, spec := range catalog {
		if !tools.CanGrantToSubtask(spec) {
			continue
		}
		names = append(names, spec.Name)
	}
	return names
}
