package tool

import (
	"context"
	"encoding/json"
)

// Category 表示工具行为类别，用于 Guard 和 UI 展示区分感知、行动、沟通类工具。
type Category int

const (
	Perceive Category = iota
	Act
	Communicate
)

// Result 是工具执行结果；IsError 必须准确反映工具是否失败，避免 Agent 误判执行状态。
type Result struct {
	Content   string `json:"content"`
	Error     string `json:"error,omitempty"`
	IsError   bool   `json:"is_error"`
	Truncated bool   `json:"truncated,omitempty"`
}

// TextResult 创建成功的文本工具结果。
func TextResult(content string) Result {
	return Result{Content: content}
}

// ErrorResult 创建失败的工具结果，核心循环会把它传给模型并标记 toolFailed。
func ErrorResult(err string) Result {
	return Result{Content: err, Error: err, IsError: true}
}

// TruncatedResult 创建被截断的成功结果，调用方可在 UI 中提示用户。
func TruncatedResult(content string) Result {
	return Result{Content: content, Truncated: true}
}

func (r Result) MarshalJSON() ([]byte, error) {
	return json.Marshal(r)
}

// Tool 是所有本地能力的统一接口，对应设计文档中的 Tool Registry 层。
// Execute 必须尊重 ctx 取消，并且失败时返回 IsError=true。
type Tool interface {
	Name() string
	Description() string
	Category() Category
	Parameters() map[string]any
	Execute(ctx context.Context, params map[string]any) Result
}

// Registry 保存 daemon 当前可用的工具集合，Agent 每轮从这里生成模型 tool schema。
type Registry struct {
	tools map[string]Tool
}

// NewRegistry 创建空工具注册表。
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register 注册一个工具；同名工具会覆盖旧实现，便于测试替换。
func (r *Registry) Register(t Tool) {
	r.tools[t.Name()] = t
}

// Get 按工具名查找实现。
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// All 返回全部工具；顺序不保证稳定，渲染展示前应自行排序。
func (r *Registry) All() []Tool {
	tools := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		tools = append(tools, t)
	}
	return tools
}

// Names 返回所有工具名，用于状态输出和调试。
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	return names
}

// ToolsForAgent 返回可暴露给主 Agent 的工具集合，spawn 可按调用场景显式开关。
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

// ToolDefs 将工具接口转换为通用 function-call schema。
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

// AsModelTools 保留 Registry 语义入口，供需要从 registry 直接导出 schema 的调用方使用。
func (r *Registry) AsModelTools(tools []Tool) []map[string]any {
	return ToolDefs(tools)
}
