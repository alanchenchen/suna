package model

import (
	"context"
	"encoding/json"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type ContentBlockType string

const (
	ContentText  ContentBlockType = "text"
	ContentImage ContentBlockType = "image"
	ContentAudio ContentBlockType = "audio"
)

type ContentBlock struct {
	Type     ContentBlockType `json:"type"`
	Text     string           `json:"text,omitempty"`
	MediaURL string           `json:"media_url,omitempty"`
	MediaB64 string           `json:"media_b64,omitempty"`
	MimeType string           `json:"mime_type,omitempty"`
}

type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Message struct {
	Role        Role           `json:"role"`
	Content     []ContentBlock `json:"content,omitempty"`
	TextContent string         `json:"-"`
	ToolCalls   []ToolCall     `json:"tool_calls,omitempty"`
	ToolCallID  string         `json:"tool_call_id,omitempty"`
}

func NewTextMessage(role Role, text string) Message {
	return Message{
		Role:        role,
		TextContent: text,
		Content:     []ContentBlock{{Type: ContentText, Text: text}},
	}
}

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

type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type CompletionRequest struct {
	Model       string    `json:"model"`
	System      string    `json:"system,omitempty"`
	Messages    []Message `json:"messages"`
	Tools       []ToolDef `json:"tools,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
}

type Chunk struct {
	Content          string     `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	Done             bool       `json:"done"`
	Usage            *Usage     `json:"usage,omitempty"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	CachedTokens int `json:"cached_tokens"`
}

type Provider interface {
	Complete(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error)
	EstimateTokens(text string) int
	ContextWindow() int
	SupportsEmbedding() bool
	Embed(ctx context.Context, texts []string) ([][]float64, error)
}

func ParseToolCallArguments(raw string) map[string]any {
	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		result = map[string]any{"raw": raw}
	}
	return result
}
