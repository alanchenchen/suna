package model

import (
	"context"
	"fmt"
	"time"

	"github.com/alanchenchen/suna/internal/logging"
	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

type AnthropicProvider struct {
	client        *anthropic.Client
	model         string
	contextWindow int
}

func NewAnthropicProvider(apiKey, model string, contextWindow int) *AnthropicProvider {
	client := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &AnthropicProvider{
		client:        &client,
		model:         model,
		contextWindow: contextWindow,
	}
}

func (p *AnthropicProvider) Complete(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error) {
	ch := make(chan Chunk, 64)

	messages := p.buildMessages(req)
	tools := p.buildTools(req.Tools)

	modelName := p.model
	if req.Model != "" {
		modelName = req.Model
	}

	maxTokens := 4096
	if req.MaxTokens > 0 {
		maxTokens = req.MaxTokens
	}

	go func() {
		defer close(ch)
		started := time.Now()

		params := anthropic.MessageNewParams{
			Model:     modelName,
			MaxTokens: int64(maxTokens),
			Messages:  messages,
		}
		if req.System != "" {
			params.System = []anthropic.TextBlockParam{
				{Text: req.System},
			}
		}
		if len(tools) > 0 {
			params.Tools = tools
		}

		msg, err := p.client.Messages.New(ctx, params)
		if err != nil {
			logLLMFailure(req, err, loggingFields(started, nil))
			ch <- Chunk{Done: true, Error: fmt.Sprintf("%v", err)}
			return
		}

		var textContent string
		var toolCalls []ToolCall

		for _, block := range msg.Content {
			switch block.Type {
			case "text":
				textContent += block.Text
			case "tool_use":
				argsJSON := "{}"
				if block.Input != nil {
					if b, err := block.Input.MarshalJSON(); err == nil {
						argsJSON = string(b)
					}
				}
				toolCalls = append(toolCalls, ToolCall{
					ID:        block.ID,
					Name:      block.Name,
					Arguments: argsJSON,
				})
			}
		}

		if textContent != "" {
			ch <- Chunk{Content: textContent, Done: false}
		}
		if len(toolCalls) > 0 {
			ch <- Chunk{ToolCalls: toolCalls, Done: false}
		}
		logLLMSuccess(req, logging.Event{"duration_ms": time.Since(started).Milliseconds(), "tool_calls": len(toolCalls)})
		ch <- Chunk{Done: true}
	}()

	return ch, nil
}

func (p *AnthropicProvider) EstimateTokens(text string) int {
	return len(text) / 4
}

func (p *AnthropicProvider) ContextWindow() int {
	if p.contextWindow > 0 {
		return p.contextWindow
	}
	return 200000
}

func (p *AnthropicProvider) buildMessages(req *CompletionRequest) []anthropic.MessageParam {
	msgs := make([]anthropic.MessageParam, 0, len(req.Messages))
	for _, m := range req.Messages {
		switch m.Role {
		case RoleUser:
			blocks := p.buildUserBlocks(m)
			msgs = append(msgs, anthropic.NewUserMessage(blocks...))
		case RoleAssistant:
			blocks := p.buildAssistantBlocks(m)
			msgs = append(msgs, anthropic.NewAssistantMessage(blocks...))
		case RoleTool:
			content := m.Text()
			msgs = append(msgs, anthropic.NewUserMessage(
				anthropic.NewToolResultBlock(m.ToolCallID, content, false),
			))
		}
	}
	return msgs
}

func (p *AnthropicProvider) buildUserBlocks(m Message) []anthropic.ContentBlockParamUnion {
	var blocks []anthropic.ContentBlockParamUnion
	for _, c := range m.Content {
		switch c.Type {
		case ContentText:
			blocks = append(blocks, anthropic.NewTextBlock(c.Text))
		case ContentImage:
			if imageBlock, ok := anthropicImageBlock(c); ok {
				blocks = append(blocks, imageBlock)
			}
		}
	}
	if len(blocks) == 0 && m.TextContent != "" {
		blocks = append(blocks, anthropic.NewTextBlock(m.TextContent))
	}
	return blocks
}

func (p *AnthropicProvider) buildAssistantBlocks(m Message) []anthropic.ContentBlockParamUnion {
	var blocks []anthropic.ContentBlockParamUnion
	for _, c := range m.Content {
		switch c.Type {
		case ContentText:
			if c.Text == "" {
				continue
			}
			blocks = append(blocks, anthropic.NewTextBlock(c.Text))
		case ContentImage:
			if imageBlock, ok := anthropicImageBlock(c); ok {
				blocks = append(blocks, imageBlock)
			}
		}
	}
	if m.TextContent != "" && len(blocks) == 0 {
		blocks = append(blocks, anthropic.NewTextBlock(m.TextContent))
	}
	for _, tc := range m.ToolCalls {
		blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, tc.Arguments, tc.Name))
	}
	return blocks
}

func anthropicImageBlock(block ContentBlock) (anthropic.ContentBlockParamUnion, bool) {
	if block.MediaB64 != "" {
		mimeType := block.MimeType
		if mimeType == "" {
			mimeType = "image/png"
		}
		return anthropic.NewImageBlockBase64(mimeType, block.MediaB64), true
	}
	if block.MediaURL != "" {
		return anthropic.NewImageBlock(anthropic.URLImageSourceParam{URL: block.MediaURL}), true
	}
	return anthropic.ContentBlockParamUnion{}, false
}

func (p *AnthropicProvider) buildTools(tools []ToolDef) []anthropic.ToolUnionParam {
	if len(tools) == 0 {
		return nil
	}
	result := make([]anthropic.ToolUnionParam, len(tools))
	for i, t := range tools {
		result[i] = anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        t.Name,
				Description: anthropic.String(t.Description),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: t.Parameters,
				},
			},
		}
	}
	return result
}
