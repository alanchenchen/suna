package tool

import (
	"context"
	"encoding/json"
)

type Category int

const (
	Perceive Category = iota
	Act
	Communicate
)

type Result struct {
	Content   string `json:"content"`
	Error     string `json:"error,omitempty"`
	IsError   bool   `json:"is_error"`
	Truncated bool   `json:"truncated,omitempty"`
}

func TextResult(content string) Result {
	return Result{Content: content}
}

func ErrorResult(err string) Result {
	return Result{Content: err, Error: err, IsError: true}
}

func TruncatedResult(content string) Result {
	return Result{Content: content, Truncated: true}
}

func (r Result) MarshalJSON() ([]byte, error) {
	return json.Marshal(r)
}

type Tool interface {
	Name() string
	Description() string
	Category() Category
	Parameters() map[string]any
	Execute(ctx context.Context, params map[string]any) Result
}

type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
}

func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) All() []Tool {
	tools := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		tools = append(tools, t)
	}
	return tools
}

func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	return names
}

func (r *Registry) ToolsForAgent(includeSpawn bool) []Tool {
	tools := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		if !includeSpawn && t.Name() == "spawn" {
			continue
		}
		tools = append(tools, t)
	}
	return tools
}

func ToolDefs(tools []Tool) []map[string]any {
	defs := make([]map[string]any, len(tools))
	for i, t := range tools {
		defs[i] = map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name(),
				"description": t.Description(),
				"parameters":  t.Parameters(),
			},
		}
	}
	return defs
}

func (r *Registry) AsModelTools(tools []Tool) []map[string]any {
	return ToolDefs(tools)
}
