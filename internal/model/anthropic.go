package model

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

type AnthropicProvider struct {
	client          *anthropic.Client
	model           string
	contextWindow   int
	maxOutputTokens int
	media           MediaResolver
}

func NewAnthropicProvider(apiKey, baseURL, model string, contextWindow, maxOutputTokens int, mediaResolver MediaResolver) *AnthropicProvider {
	httpClient := compatibleHTTPClient(&http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}})
	// 关闭 SDK 隐式重试，避免一次 Suna Complete 在上游产生多次不可见请求；
	// 未来如需重试应由 Suna 自己实现并记录日志。
	opts := []option.RequestOption{option.WithAPIKey(apiKey), option.WithHTTPClient(httpClient), option.WithMaxRetries(0)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	client := anthropic.NewClient(opts...)
	return &AnthropicProvider{
		client:          &client,
		model:           model,
		contextWindow:   contextWindow,
		maxOutputTokens: maxOutputTokens,
		media:           mediaResolver,
	}
}

func (p *AnthropicProvider) Complete(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error) {
	if p.maxOutputTokens <= 0 {
		return nil, fmt.Errorf("max_output_tokens is required for model %q", p.model)
	}
	maxTokens := p.resolveMaxTokens(req.MaxTokens)
	req.MaxTokens = maxTokens
	messages, buildErr := p.buildMessages(ctx, req)
	if buildErr != nil {
		return nil, buildErr
	}
	tools := p.buildTools(req.Tools)

	modelName := p.model
	if req.Model != "" {
		modelName = req.Model
	}
	params := anthropic.MessageNewParams{
		Model:     modelName,
		MaxTokens: int64(maxTokens),
		Messages:  messages,
	}
	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}
	if len(tools) > 0 {
		params.Tools = tools
	}
	hasReasoning := len(req.Reasoning) > 0
	if err := p.applyReasoning(&params, req.Reasoning); err != nil {
		return nil, err
	}
	if !hasReasoning {
		params.Temperature = anthropic.Float(resolveAnthropicTemperature(req.Temperature))
	}

	ch := make(chan Chunk, providerChunkBuffer)
	go func() {
		defer close(ch)
		stream := p.client.Messages.NewStreaming(ctx, params)
		defer stream.Close()

		var usage *Usage
		toolCalls := map[int64]*anthropicToolCallAccum{}

		for stream.Next() {
			event := stream.Current()
			switch event.Type {
			case "message_start":
				start := event.AsMessageStart()
				usage = anthropicUsageFromMessage(start.Message.Usage)
			case "message_delta":
				delta := event.AsMessageDelta()
				usage = mergeAnthropicUsage(usage, anthropicUsageFromDelta(delta.Usage))
			case "content_block_start":
				start := event.AsContentBlockStart()
				block := start.ContentBlock
				switch block.Type {
				case "text":
					if block.Text != "" {
						ch <- Chunk{Content: block.Text, Done: false}
					}
				case "thinking":
					if block.Thinking != "" {
						ch <- Chunk{ReasoningContent: block.Thinking, Done: false}
					}
				case "tool_use":
					call := &anthropicToolCallAccum{ID: block.ID, Name: block.Name}
					if block.Input != nil {
						if b, err := json.Marshal(block.Input); err == nil && string(b) != "{}" {
							call.InitialArguments = string(b)
						}
					}
					toolCalls[start.Index] = call
				}
			case "content_block_delta":
				delta := event.AsContentBlockDelta()
				switch delta.Delta.Type {
				case "text_delta":
					if delta.Delta.Text != "" {
						ch <- Chunk{Content: delta.Delta.Text, Done: false}
					}
				case "thinking_delta":
					if delta.Delta.Thinking != "" {
						ch <- Chunk{ReasoningContent: delta.Delta.Thinking, Done: false}
					}
				case "input_json_delta":
					call := toolCalls[delta.Index]
					if call == nil {
						call = &anthropicToolCallAccum{}
						toolCalls[delta.Index] = call
					}
					call.Arguments.WriteString(delta.Delta.PartialJSON)
				}
			}
		}

		if err := stream.Err(); err != nil {
			ch <- Chunk{Done: true, Error: modelErrorFromProvider(err, "anthropic", modelName)}
			return
		}

		calls := anthropicAccumulatedToolCalls(toolCalls)
		if len(calls) > 0 {
			ch <- Chunk{ToolCalls: calls, Done: false}
		}
		if usage != nil {
			ch <- Chunk{Done: true, Usage: usage}
			return
		}
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

func resolveAnthropicTemperature(t float64) float64 {
	if t > 0 {
		return t
	}
	return 0.7
}

type anthropicToolCallAccum struct {
	ID               string
	Name             string
	InitialArguments string
	Arguments        strings.Builder
}

func anthropicAccumulatedToolCalls(acc map[int64]*anthropicToolCallAccum) []ToolCall {
	if len(acc) == 0 {
		return nil
	}
	indexes := make([]int64, 0, len(acc))
	for index := range acc {
		indexes = append(indexes, index)
	}
	sort.Slice(indexes, func(i, j int) bool { return indexes[i] < indexes[j] })

	calls := make([]ToolCall, 0, len(indexes))
	for _, index := range indexes {
		call := acc[index]
		if call == nil || call.ID == "" || call.Name == "" {
			continue
		}
		args := call.Arguments.String()
		if args == "" {
			args = call.InitialArguments
		}
		if strings.TrimSpace(args) == "" {
			args = "{}"
		}
		calls = append(calls, ToolCall{ID: call.ID, Name: call.Name, Arguments: args})
	}
	return calls
}

func anthropicUsageFromMessage(u anthropic.Usage) *Usage {
	return anthropicUsage(int(u.InputTokens), int(u.CacheCreationInputTokens), int(u.CacheReadInputTokens), int(u.OutputTokens))
}

func anthropicUsageFromDelta(u anthropic.MessageDeltaUsage) *Usage {
	return anthropicUsage(int(u.InputTokens), int(u.CacheCreationInputTokens), int(u.CacheReadInputTokens), int(u.OutputTokens))
}

func mergeAnthropicUsage(prev, next *Usage) *Usage {
	if prev == nil {
		return next
	}
	if next == nil {
		return prev
	}
	merged := *next
	if merged.InputTokens == 0 {
		merged.InputTokens = prev.InputTokens
	}
	if merged.CachedTokens == 0 {
		merged.CachedTokens = prev.CachedTokens
	}
	if merged.TotalTokens == merged.OutputTokens && merged.InputTokens > 0 {
		merged.TotalTokens = merged.InputTokens + merged.OutputTokens
	}
	return &merged
}

func anthropicUsage(inputTokens, cacheCreationTokens, cacheReadTokens, outputTokens int) *Usage {
	inputTotal := inputTokens + cacheCreationTokens + cacheReadTokens
	return &Usage{
		InputTokens:  inputTotal,
		OutputTokens: outputTokens,
		CachedTokens: cacheReadTokens,
		TotalTokens:  inputTotal + outputTokens,
	}
}

func (p *AnthropicProvider) EstimateTokens(text string) int {
	return len(text) / 4
}

func (p *AnthropicProvider) ContextWindow() int { return p.contextWindow }

func (p *AnthropicProvider) MaxOutputTokens() int {
	return p.maxOutputTokens
}

func (p *AnthropicProvider) resolveMaxTokens(m int) int {
	if m > 0 && m < p.maxOutputTokens {
		return m
	}
	return p.maxOutputTokens
}

func (p *AnthropicProvider) buildMessages(ctx context.Context, req *CompletionRequest) ([]anthropic.MessageParam, error) {
	msgs := make([]anthropic.MessageParam, 0, len(req.Messages)+1)
	if state := FormatSessionStateForModel(req.SessionState); state != "" {
		msgs = append(msgs, anthropic.NewUserMessage(anthropic.NewTextBlock(state)))
	}
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
		blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, ParseToolCallArguments(tc.Arguments), tc.Name))
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
		inputSchema := anthropicToolInputSchema(t.Parameters)
		result[i] = anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        t.Name,
				Description: anthropic.String(t.Description),
				InputSchema: inputSchema,
			},
		}
	}
	return result
}

func anthropicToolInputSchema(params map[string]any) anthropic.ToolInputSchemaParam {
	schema := anthropic.ToolInputSchemaParam{}
	if len(params) == 0 {
		return schema
	}
	if properties, ok := params["properties"]; ok {
		schema.Properties = properties
	} else {
		schema.Properties = map[string]any{}
	}
	schema.Required = stringList(params["required"])
	extra := make(map[string]any)
	for key, value := range params {
		switch key {
		case "type", "properties", "required":
			continue
		default:
			extra[key] = value
		}
	}
	if len(extra) > 0 {
		schema.ExtraFields = extra
	}
	return schema
}

func stringList(value any) []string {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
