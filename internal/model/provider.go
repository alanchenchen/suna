package model

import (
	"context"
	"encoding/json"
)

// Role 表示发送给模型的消息角色，和主流 Chat Completion 协议保持一致。
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ContentBlockType 表示多模态消息块类型；当前主路径主要使用文本块。
type ContentBlockType string

const (
	ContentText  ContentBlockType = "text"
	ContentImage ContentBlockType = "image"
	ContentAudio ContentBlockType = "audio"
)

// ContentBlock 是模型消息的最小内容单元，为后续图片/音频输入保留结构。
type ContentBlock struct {
	Type     ContentBlockType `json:"type"`
	Text     string           `json:"text,omitempty"`
	MediaURL string           `json:"media_url,omitempty"`
	MediaB64 string           `json:"media_b64,omitempty"`
	MimeType string           `json:"mime_type,omitempty"`
}

// ToolCall 表示模型请求执行的函数工具调用，Arguments 必须是 JSON 字符串。
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Message 是 core 与 provider 之间的统一消息结构，屏蔽 OpenAI/Anthropic 的协议差异。
type Message struct {
	Role        Role           `json:"role"`
	Content     []ContentBlock `json:"content,omitempty"`
	TextContent string         `json:"-"`
	ToolCalls   []ToolCall     `json:"tool_calls,omitempty"`
	ToolCallID  string         `json:"tool_call_id,omitempty"`
}

// NewTextMessage 创建纯文本消息，是 Agent Loop 组装上下文的常用入口。
func NewTextMessage(role Role, text string) Message {
	return Message{
		Role:        role,
		TextContent: text,
		Content:     []ContentBlock{{Type: ContentText, Text: text}},
	}
}

// Text 返回消息中的文本内容，优先使用 TextContent 以避免重复扫描内容块。
func (m Message) Text() string {
	if m.TextContent != "" {
		return m.TextContent
	}
	for _, b := range m.Content {
		if b.Type == ContentText {
			return b.Text
		}
	}
	return ""
}

// ToolDef 是暴露给模型的工具 schema，来自 internal/tool.Registry。
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// CompletionRequest 是 provider 的统一请求格式，对应设计文档中的 Model Router 输出。
type CompletionRequest struct {
	Model       string    `json:"model"`
	System      string    `json:"system,omitempty"`
	Messages    []Message `json:"messages"`
	Tools       []ToolDef `json:"tools,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
}

// Chunk 是 provider 流式输出的原子片段；Error 非空时调用方必须停止并按失败处理。
type Chunk struct {
	Content          string     `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	Done             bool       `json:"done"`
	Usage            *Usage     `json:"usage,omitempty"`
	Error            string     `json:"error,omitempty"`
}

// Usage 记录一次模型调用的 token 用量，用于 TUI 状态栏和 usage_log。
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	CachedTokens int `json:"cached_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// Provider 抽象具体模型供应商，Agent 只依赖这个接口而不直接依赖 SDK。
type Provider interface {
	Complete(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error)
	EstimateTokens(text string) int
	ContextWindow() int
	SupportsEmbedding() bool
	Embed(ctx context.Context, texts []string) ([][]float64, error)
}

// ParseToolCallArguments 将模型返回的工具参数 JSON 解为 map；非法 JSON 会显式保留 raw 字段便于排错。
func ParseToolCallArguments(raw string) map[string]any {
	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		result = map[string]any{"raw": raw}
	}
	return result
}
