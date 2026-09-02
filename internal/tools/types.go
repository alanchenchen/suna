package tools

import (
	"context"

	"github.com/alanchenchen/suna/internal/model"
)

// Category 表示工具行为类别，供 Guard、UI 和权限策略区分感知类/行动类工具。
type Category int

const (
	Perceive Category = iota
	Act
)

// Result 是工具执行结果。Content 会进入 LLM 上下文；Metadata 只给 UI/API 展示使用。
// IsError 必须准确反映工具是否失败，避免 Agent 误判执行状态。
// Images 是工具产出的图片块（如 read_image），由 Runner 统一注入为独立的 user 消息，
// 不进入 Content 文本；图片块只参与当前 run，run 结束后由 Agent 层替换为摘要文本。
type Result struct {
	Content   string               `json:"content"`
	Error     string               `json:"error,omitempty"`
	IsError   bool                 `json:"is_error"`
	Truncated bool                 `json:"truncated,omitempty"`
	Metadata  map[string]any       `json:"metadata,omitempty"`
	Images    []model.ContentBlock `json:"images,omitempty"`
}

func TextResult(content string) Result { return Result{Content: content} }
func ErrorResult(err string) Result    { return Result{Content: err, Error: err, IsError: true} }
func TruncatedResult(content string) Result {
	return Result{Content: content, Truncated: true}
}

type SourceKind string

const (
	SourceBuiltin SourceKind = "builtin"
	SourceAgent   SourceKind = "agent"
	SourceSkill   SourceKind = "skill"
	SourceMCP     SourceKind = "mcp"
)

type GuardPolicy string

const (
	GuardDefault GuardPolicy = ""
	GuardAlways  GuardPolicy = "always"
	GuardNever   GuardPolicy = "never"
)

// ShouldGuard returns whether Agent Guard should review this tool call.
// 新工具默认走 Guard；只有明确标记 GuardNever 的运行时/说明类工具才跳过。
func ShouldGuard(spec Spec) bool {
	switch spec.Guard {
	case GuardNever:
		return false
	case GuardAlways:
		return true
	default:
		return true
	}
}

// Source 标识工具来自哪个能力来源；Guard/UI 可以基于来源做规则和展示。
type Source struct {
	Kind SourceKind `json:"kind"`
	ID   string     `json:"id,omitempty"`
}

// Spec 是工具目录中的稳定描述，Provider 只暴露 Spec，不暴露自身实现细节。
type Spec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	Category    Category       `json:"category"`
	Source      Source         `json:"source"`
	Guard       GuardPolicy    `json:"guard,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// Call 是一次工具调用的统一结构；Manager 会按 Name 路由到对应 Provider。
type Call struct {
	ID               string
	Name             string
	Params           map[string]any
	Intent           string
	AssistantContext string
	Spec             *Spec
}

// CatalogProvider 可以在生成自身 Spec 时读取此前已注册 Provider 的工具目录。
// 这用于 spawn 这类 schema 依赖当前可授权工具列表的运行时工具。
type CatalogProvider interface {
	Provider
	SpecsWithCatalog(ctx context.Context, catalog []Spec) ([]Spec, error)
}

// CanGrantToSubtask returns whether a tool may be explicitly granted to an isolated subtask.
// Subtasks can use concrete work tools from builtin providers and trusted MCP servers; agent
// runtime tools and skill workflow/context tools stay under main-agent control.
func CanGrantToSubtask(spec Spec) bool {
	switch spec.Source.Kind {
	case SourceBuiltin, SourceMCP:
		return true
	default:
		return false
	}
}

// Provider 表示一组同源工具。MCP、Skill、内置工具都可以通过 Provider 接入。
type Provider interface {
	Specs(ctx context.Context) ([]Spec, error)
	Execute(ctx context.Context, call Call) (Result, bool)
	Close(ctx context.Context) error
}

// ScopedLifecycleProvider 允许状态化工具按 run 或 session 回收受管资源。
// Manager 只负责转发生命周期，具体所有权和清理语义仍由 Provider 实现。
type ScopedLifecycleProvider interface {
	Provider
	CleanupRun(ctx context.Context, execCtx ExecutionContext) error
	CleanupSession(ctx context.Context, sessionID string) error
}
