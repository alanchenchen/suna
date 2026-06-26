package model

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

type OpenAIResponsesProvider struct {
	client          openai.Client
	model           string
	contextWindow   int
	maxOutputTokens int
	media           MediaResolver
}

func NewOpenAIResponsesProvider(apiKey, baseURL, model string, contextWindow, maxOutputTokens int, mediaResolver MediaResolver) *OpenAIResponsesProvider {
	httpClient := compatibleHTTPClient(&http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}})
	// 关闭 SDK 隐式重试，避免一次 Suna Complete 在上游产生多次不可见请求；
	// 未来如需重试应由 Suna 自己实现并记录日志。
	client := openai.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(baseURL), option.WithHTTPClient(httpClient), option.WithMaxRetries(0))
	return &OpenAIResponsesProvider{client: client, model: model, contextWindow: contextWindow, maxOutputTokens: maxOutputTokens, media: mediaResolver}
}

func (p *OpenAIResponsesProvider) Complete(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error) {
	if p.maxOutputTokens <= 0 {
		return nil, fmt.Errorf("max_output_tokens is required for model %q", p.model)
	}
	maxTokens := p.resolveMaxTokens(req.MaxTokens)
	req.MaxTokens = maxTokens
	input, err := p.buildInput(ctx, req)
	if err != nil {
		return nil, err
	}
	params := responses.ResponseNewParams{
		Model:             responses.ResponsesModel(p.resolveModel(req.Model)),
		Input:             responses.ResponseNewParamsInputUnion{OfInputItemList: input},
		MaxOutputTokens:   openai.Int(int64(maxTokens)),
		Temperature:       openai.Float(p.resolveTemperature(req.Temperature)),
		ParallelToolCalls: openai.Bool(true),
	}
	if req.System != "" {
		params.Instructions = openai.String(req.System)
	}
	if tools := p.buildTools(req.Tools); len(tools) > 0 {
		params.Tools = tools
	}
	opts, err := reasoningRequestOptions(req.Reasoning, responsesGeneratedKeys())
	if err != nil {
		return nil, err
	}

	ch := make(chan Chunk, providerChunkBuffer)
	go func() {
		defer close(ch)
		stream := p.client.Responses.NewStreaming(ctx, params, opts...)
		defer stream.Close()

		var usage *Usage
		toolCallsByID := map[string]*responseToolCall{}
		var toolCallOrder []string

		for stream.Next() {
			event := stream.Current()
			switch event.Type {
			case "response.output_text.delta":
				if event.Delta != "" {
					ch <- Chunk{Content: event.Delta, Done: false}
				}
			case "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
				if reasoning := responseReasoningContent(event); reasoning != "" {
					ch <- Chunk{ReasoningContent: reasoning, Done: false}
				}
			case "response.function_call_arguments.delta", "response.function_call_arguments.done", "response.output_item.done", "response.output_item.added":
				mergeResponseToolCall(event, toolCallsByID, &toolCallOrder)
			case "response.completed":
				u := event.Response.Usage
				if event.JSON.Response.Valid() {
					usage = &Usage{InputTokens: int(u.InputTokens), OutputTokens: int(u.OutputTokens), CachedTokens: int(u.InputTokensDetails.CachedTokens), TotalTokens: int(u.TotalTokens)}
					collectResponseOutputToolCalls(event.Response.Output, toolCallsByID, &toolCallOrder)
				}
			case "error":
				err := fmt.Errorf("responses error: %s", event.Message)
				ch <- Chunk{Done: true, Error: modelErrorFromProvider(err, "openai", p.resolveModel(req.Model))}
				return
			}
		}
		if err := stream.Err(); err != nil {
			ch <- Chunk{Done: true, Error: modelErrorFromProvider(err, "openai", p.resolveModel(req.Model))}
			return
		}
		toolCalls := orderedResponseToolCalls(toolCallsByID, toolCallOrder)
		if len(toolCalls) > 0 {
			ch <- Chunk{ToolCalls: toolCalls, Done: false}
		}
		if usage != nil {
			ch <- Chunk{Done: true, Usage: usage}
			return
		}
		ch <- Chunk{Done: true}
	}()
	return ch, nil
}

func responsesGeneratedKeys() map[string]bool {
	return map[string]bool{"model": true, "input": true, "max_output_tokens": true, "temperature": true, "parallel_tool_calls": true, "instructions": true, "tools": true, "stream": true}
}

func responseReasoningContent(event responses.ResponseStreamEventUnion) string {
	switch event.Type {
	case "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
		return event.Delta
	default:
		return ""
	}
}

func (p *OpenAIResponsesProvider) EstimateTokens(text string) int { return len(text) / 4 }

func (p *OpenAIResponsesProvider) ContextWindow() int { return p.contextWindow }

func (p *OpenAIResponsesProvider) MaxOutputTokens() int {
	return p.maxOutputTokens
}

func (p *OpenAIResponsesProvider) resolveModel(m string) string {
	if m != "" {
		return m
	}
	return p.model
}

func (p *OpenAIResponsesProvider) resolveMaxTokens(m int) int {
	if m > 0 && m < p.maxOutputTokens {
		return m
	}
	return p.maxOutputTokens
}

func (p *OpenAIResponsesProvider) resolveTemperature(t float64) float64 {
	if t > 0 {
		return t
	}
	return 0.7
}

func (p *OpenAIResponsesProvider) buildInput(ctx context.Context, req *CompletionRequest) (responses.ResponseInputParam, error) {
	input := make(responses.ResponseInputParam, 0, len(req.Messages)*2+1)
	if state := FormatSessionStateForModel(req.SessionState); state != "" {
		input = append(input, responses.ResponseInputItemParamOfMessage(state, responses.EasyInputMessageRoleUser))
	}
	for _, m := range req.Messages {
		switch m.Role {
		case RoleUser:
			content, err := p.buildInputContent(ctx, m)
			if err != nil {
				return nil, err
			}
			input = append(input, responses.ResponseInputItemParamOfMessage(content, responses.EasyInputMessageRoleUser))
		case RoleAssistant:
			if text := m.Text(); text != "" {
				input = append(input, responses.ResponseInputItemParamOfMessage(text, responses.EasyInputMessageRoleAssistant))
			}
			for _, tc := range m.ToolCalls {
				input = append(input, responses.ResponseInputItemParamOfFunctionCall(tc.Arguments, tc.ID, tc.Name))
			}
		case RoleTool:
			input = append(input, responses.ResponseInputItemParamOfFunctionCallOutput(m.ToolCallID, m.Text()))
		}
	}
	return input, nil
}

func (p *OpenAIResponsesProvider) buildInputContent(ctx context.Context, m Message) (responses.ResponseInputMessageContentListParam, error) {
	blocks := make(responses.ResponseInputMessageContentListParam, 0, len(m.Content))
	for _, c := range m.Content {
		switch c.Type {
		case ContentText:
			if c.Text != "" {
				blocks = append(blocks, responses.ResponseInputContentParamOfInputText(c.Text))
			}
		case ContentImage:
			imageURL, err := p.openAIImageURL(ctx, c)
			if err != nil {
				return nil, err
			}
			if imageURL != "" {
				img := responses.ResponseInputContentParamOfInputImage(responses.ResponseInputImageDetailAuto)
				img.OfInputImage.ImageURL = openai.String(imageURL)
				blocks = append(blocks, img)
			}
		}
	}
	if len(blocks) == 0 && m.TextContent != "" {
		blocks = append(blocks, responses.ResponseInputContentParamOfInputText(m.TextContent))
	}
	return blocks, nil
}

func (p *OpenAIResponsesProvider) openAIImageURL(ctx context.Context, block ContentBlock) (string, error) {
	if block.Media == nil || p.media == nil {
		return "", fmt.Errorf("image media resolver is unavailable")
	}
	resolved, err := p.media.Resolve(ctx, *block.Media, ResolveAsBase64)
	if err != nil {
		return "", err
	}
	if resolved.URL != "" {
		return resolved.URL, nil
	}
	if resolved.Base64 == "" {
		return "", fmt.Errorf("resolved image is empty")
	}
	mimeType := resolved.MimeType
	if mimeType == "" {
		mimeType = "image/png"
	}
	return "data:" + mimeType + ";base64," + resolved.Base64, nil
}

func (p *OpenAIResponsesProvider) buildTools(tools []ToolDef) []responses.ToolUnionParam {
	if len(tools) == 0 {
		return nil
	}
	result := make([]responses.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		result = append(result, responses.ToolParamOfFunction(t.Name, t.Parameters, false))
		if result[len(result)-1].OfFunction != nil {
			result[len(result)-1].OfFunction.Description = openai.String(t.Description)
		}
	}
	return result
}

type responseToolCall struct {
	ID, Name  string
	Arguments strings.Builder
}

func mergeResponseToolCall(event responses.ResponseStreamEventUnion, calls map[string]*responseToolCall, order *[]string) {
	if event.Type == "response.output_item.added" || event.Type == "response.output_item.done" {
		if event.Item.Type == "function_call" {
			upsertResponseToolCall(calls, order, event.Item.ID, event.Item.CallID, event.Item.Name, event.Item.Arguments.OfString, false)
		}
	}
	if event.Type == "response.function_call_arguments.delta" && event.Delta != "" {
		upsertResponseToolCall(calls, order, event.ItemID, "", "", event.Delta, true)
	}
	if event.Type == "response.function_call_arguments.done" {
		upsertResponseToolCall(calls, order, event.ItemID, "", event.Name, event.Arguments, false)
	}
}

func collectResponseOutputToolCalls(items []responses.ResponseOutputItemUnion, calls map[string]*responseToolCall, order *[]string) {
	for _, item := range items {
		if item.Type == "function_call" {
			upsertResponseToolCall(calls, order, item.ID, item.CallID, item.Name, item.Arguments.OfString, false)
		}
	}
}

func orderedResponseToolCalls(calls map[string]*responseToolCall, order []string) []ToolCall {
	toolCalls := make([]ToolCall, 0, len(order))
	for _, id := range order {
		if tc := calls[id]; tc != nil && tc.Name != "" {
			toolCalls = append(toolCalls, ToolCall{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments.String()})
		}
	}
	return toolCalls
}

func upsertResponseToolCall(calls map[string]*responseToolCall, order *[]string, itemID, callID, name, args string, appendArgs bool) {
	key := callID
	if key == "" {
		key = itemID
	}
	if key == "" {
		key = fmt.Sprintf("call_%d", len(*order))
	}
	if callID != "" && itemID != "" && callID != itemID {
		if existing := calls[itemID]; existing != nil {
			delete(calls, itemID)
			calls[callID] = existing
			for i, id := range *order {
				if id == itemID {
					(*order)[i] = callID
					break
				}
			}
		}
	}
	tc, ok := calls[key]
	if !ok {
		calls[key] = &responseToolCall{ID: key}
		*order = append(*order, key)
		tc = calls[key]
	}
	if name != "" {
		tc.Name = name
	}
	if callID != "" {
		tc.ID = callID
	}
	if appendArgs {
		tc.Arguments.WriteString(args)
	} else if args != "" {
		tc.Arguments.Reset()
		tc.Arguments.WriteString(args)
	}
}
