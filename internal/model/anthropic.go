package model

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/alanchenchen/suna/internal/logging"
	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

type AnthropicProvider struct {
	client        *anthropic.Client
	model         string
	contextWindow int
	media         MediaResolver
}

func NewAnthropicProvider(apiKey, baseURL, model string, contextWindow int, mediaResolver MediaResolver) *AnthropicProvider {
	httpClient := compatibleHTTPClient(&http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}})
	client := anthropic.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(baseURL), option.WithHTTPClient(httpClient))
	return &AnthropicProvider{
		client:        &client,
		model:         model,
		contextWindow: contextWindow,
		media:         mediaResolver,
	}
}

func (p *AnthropicProvider) Complete(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error) {
	ch := make(chan Chunk, providerChunkBuffer)

	messages, buildErr := p.buildMessages(ctx, req)
	if buildErr != nil {
		return nil, buildErr
	}
	tools := p.buildTools(req.Tools)

	modelName := p.model
	if req.Model != "" {
		modelName = req.Model
	}

	maxTokens := ResolveMaxTokens(req.MaxTokens)

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
		if err := p.applyReasoning(&params, req.Reasoning); err != nil {
			logLLMFailure(req, err, loggingFields(started, nil))
			ch <- Chunk{Done: true, Error: err.Error()}
			return
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

func (p *AnthropicProvider) applyReasoning(params *anthropic.MessageNewParams, reasoning map[string]any) error {
	if len(reasoning) == 0 {
		return nil
	}
	if len(reasoning) != 1 {
		return fmt.Errorf("anthropic reasoning supports only thinking")
	}
	raw, ok := reasoning["thinking"]
	if !ok {
		return fmt.Errorf("anthropic reasoning supports only thinking")
	}
	thinking, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("anthropic thinking must be an object")
	}
	switch fmt.Sprint(thinking["type"]) {
	case "enabled":
		budget, ok := numericInt64(thinking["budget_tokens"])
		if !ok {
			return fmt.Errorf("anthropic thinking.budget_tokens is required")
		}
		params.Thinking = anthropic.ThinkingConfigParamOfEnabled(budget)
	case "disabled":
		disabled := anthropic.NewThinkingConfigDisabledParam()
		params.Thinking = anthropic.ThinkingConfigParamUnion{OfDisabled: &disabled}
	case "adaptive":
		params.Thinking = anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{}}
	default:
		return fmt.Errorf("anthropic thinking.type is invalid")
	}
	return nil
}

func numericInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	default:
		return 0, false
	}
}

func (p *AnthropicProvider) EstimateTokens(text string) int {
	return len(text) / 4
}

func (p *AnthropicProvider) ContextWindow() int {
	if p.contextWindow > 0 {
		return p.contextWindow
	}
	return DefaultContextWindow
}

func (p *AnthropicProvider) buildMessages(ctx context.Context, req *CompletionRequest) ([]anthropic.MessageParam, error) {
	msgs := make([]anthropic.MessageParam, 0, len(req.Messages))
	for i := 0; i < len(req.Messages); i++ {
		m := req.Messages[i]
		switch m.Role {
		case RoleUser:
			blocks, err := p.buildUserBlocks(ctx, m)
			if err != nil {
				return nil, err
			}
			msgs = append(msgs, anthropic.NewUserMessage(blocks...))
		case RoleAssistant:
			blocks, err := p.buildAssistantBlocks(ctx, m)
			if err != nil {
				return nil, err
			}
			msgs = append(msgs, anthropic.NewAssistantMessage(blocks...))
		case RoleTool:
			blocks := []anthropic.ContentBlockParamUnion{
				anthropic.NewToolResultBlock(m.ToolCallID, m.Text(), false),
			}
			for i+1 < len(req.Messages) && req.Messages[i+1].Role == RoleTool {
				i++
				tm := req.Messages[i]
				blocks = append(blocks, anthropic.NewToolResultBlock(tm.ToolCallID, tm.Text(), false))
			}
			msgs = append(msgs, anthropic.NewUserMessage(blocks...))
		}
	}
	return msgs, nil
}

func (p *AnthropicProvider) buildUserBlocks(ctx context.Context, m Message) ([]anthropic.ContentBlockParamUnion, error) {
	var blocks []anthropic.ContentBlockParamUnion
	for _, c := range m.Content {
		switch c.Type {
		case ContentText:
			blocks = append(blocks, anthropic.NewTextBlock(c.Text))
		case ContentImage:
			imageBlock, ok, err := p.anthropicImageBlock(ctx, c)
			if err != nil {
				return nil, err
			}
			if ok {
				blocks = append(blocks, imageBlock)
			}
		}
	}
	if len(blocks) == 0 && m.TextContent != "" {
		blocks = append(blocks, anthropic.NewTextBlock(m.TextContent))
	}
	return blocks, nil
}

func (p *AnthropicProvider) buildAssistantBlocks(ctx context.Context, m Message) ([]anthropic.ContentBlockParamUnion, error) {
	var blocks []anthropic.ContentBlockParamUnion
	for _, c := range m.Content {
		switch c.Type {
		case ContentText:
			if c.Text == "" {
				continue
			}
			blocks = append(blocks, anthropic.NewTextBlock(c.Text))
		case ContentImage:
			imageBlock, ok, err := p.anthropicImageBlock(ctx, c)
			if err != nil {
				return nil, err
			}
			if ok {
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
	return blocks, nil
}

func (p *AnthropicProvider) anthropicImageBlock(ctx context.Context, block ContentBlock) (anthropic.ContentBlockParamUnion, bool, error) {
	if block.Media == nil || p.media == nil {
		return anthropic.ContentBlockParamUnion{}, false, fmt.Errorf("image media resolver is unavailable")
	}
	resolved, err := p.media.Resolve(ctx, *block.Media, ResolveAsBase64)
	if err != nil {
		return anthropic.ContentBlockParamUnion{}, false, err
	}
	if resolved.URL != "" {
		return anthropic.NewImageBlock(anthropic.URLImageSourceParam{URL: resolved.URL}), true, nil
	}
	if resolved.Base64 != "" {
		mimeType := resolved.MimeType
		if mimeType == "" {
			mimeType = "image/png"
		}
		// Anthropic base64 图片必须拆成 source.type/media_type/data，不能使用 OpenAI 的 data URL 结构。
		return anthropic.NewImageBlockBase64(mimeType, resolved.Base64), true, nil
	}
	return anthropic.ContentBlockParamUnion{}, false, fmt.Errorf("resolved image is empty")
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
