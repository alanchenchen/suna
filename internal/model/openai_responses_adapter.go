package model

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

type OpenAIResponsesAdapter struct {
	client          openai.Client
	model           string
	contextWindow   int
	maxOutputTokens int
	media           MediaResolver
	cacheNamespace  string
}

func NewOpenAIResponsesAdapter(spec AdapterSpec, deps AdapterDependencies) *OpenAIResponsesAdapter {
	httpClient := compatibleHTTPClient(&http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}})
	// 关闭 SDK 隐式重试，避免一次 Suna Complete 在上游产生多次不可见请求；
	// 未来如需重试应由 Suna 自己实现并记录日志。
	client := openai.NewClient(option.WithAPIKey(spec.APIKey), option.WithBaseURL(spec.BaseURL), option.WithHTTPClient(httpClient), option.WithMaxRetries(0))
	cacheNamespace := responseCacheNamespace(spec)
	return &OpenAIResponsesAdapter{client: client, model: spec.ModelID, contextWindow: spec.ContextWindow, maxOutputTokens: spec.MaxOutputTokens, media: deps.MediaResolver, cacheNamespace: cacheNamespace}
}

func (p *OpenAIResponsesAdapter) Complete(ctx context.Context, req CompletionRequest) (<-chan Chunk, error) {
	if p.maxOutputTokens <= 0 {
		return nil, fmt.Errorf("max_output_tokens is required for model %q", p.model)
	}
	maxTokens := p.resolveMaxTokens(req.MaxTokens)

	input, err := p.buildInput(ctx, &req)
	if err != nil {
		return nil, err
	}
	params := responses.ResponseNewParams{
		Model:             responses.ResponsesModel(p.model),
		Input:             responses.ResponseNewParamsInputUnion{OfInputItemList: input},
		MaxOutputTokens:   openai.Int(int64(maxTokens)),
		ParallelToolCalls: openai.Bool(true),
	}
	if cacheKey := p.promptCacheKey(req); cacheKey != "" {
		params.PromptCacheKey = openai.String(cacheKey)
	}
	if req.System != "" {
		params.Instructions = openai.String(req.System)
	}
	if req.Temperature != nil {
		params.Temperature = openai.Float(*req.Temperature)
	}
	if tools := p.buildTools(req.Tools); len(tools) > 0 {
		params.Tools = tools
	}
	opts, err := openAIReasoningFieldOptions(req.Reasoning, responsesGeneratedKeys())
	if err != nil {
		return nil, err
	}

	ch := make(chan Chunk, adapterChunkBuffer)
	go func() {
		defer close(ch)
		stream := p.client.Responses.NewStreaming(ctx, params, opts...)
		defer stream.Close()

		var usage *Usage
		var finish *FinishInfo
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
					usage = &Usage{InputTokens: int(u.InputTokens), OutputTokens: int(u.OutputTokens), CacheReadTokens: int(u.InputTokensDetails.CachedTokens), TotalTokens: int(u.TotalTokens)}
					finish = responseFinishInfo(event.Response.Status, event.Response.IncompleteDetails.Reason)
					collectResponseOutputToolCalls(event.Response.Output, toolCallsByID, &toolCallOrder)
				}
			case "error":
				err := fmt.Errorf("responses error: %s", event.Message)
				ch <- Chunk{Done: true, Error: modelErrorFromProvider(err, "openai", p.model)}
				return
			}
		}
		if err := stream.Err(); err != nil {
			ch <- Chunk{Done: true, Error: modelErrorFromProvider(err, "openai", p.model)}
			return
		}
		toolCalls := orderedResponseToolCalls(toolCallsByID, toolCallOrder)
		if len(toolCalls) > 0 {
			ch <- Chunk{ToolCalls: toolCalls, Done: false}
		}
		if usage != nil {
			ch <- Chunk{Done: true, Usage: usage, Finish: finish}
			return
		}
		ch <- Chunk{Done: true, Finish: finish}
	}()
	return ch, nil
}

func responseCacheNamespace(spec AdapterSpec) string {
	sum := sha256.Sum256([]byte("suna:openai-responses:v1\x00" + spec.BaseURL + "\x00" + spec.ModelID))
	return hex.EncodeToString(sum[:16])
}

func (p *OpenAIResponsesAdapter) promptCacheKey(req CompletionRequest) string {
	if req.Invocation.SessionScope == "" || req.Purpose == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("suna:prompt-cache:v1\x00" + p.cacheNamespace + "\x00" + req.Invocation.SessionScope + "\x00" + req.Purpose))
	return "suna-" + hex.EncodeToString(sum[:16])
}

func responsesGeneratedKeys() map[string]bool {
	return map[string]bool{"model": true, "input": true, "max_output_tokens": true, "temperature": true, "parallel_tool_calls": true, "instructions": true, "tools": true, "stream": true, "prompt_cache_key": true}
}

func responseReasoningContent(event responses.ResponseStreamEventUnion) string {
	switch event.Type {
	case "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
		return event.Delta
	default:
		return ""
	}
}

func responseFinishInfo(status responses.ResponseStatus, incompleteReason string) *FinishInfo {
	finish := &FinishInfo{Status: string(status)}
	if incompleteReason != "" {
		finish.Reason = incompleteReason
		finish.NativeReason = incompleteReason
		finish.IncompleteReason = incompleteReason
	}
	if finish.Status == "" && finish.Reason == "" {
		return nil
	}
	return finish
}

func (p *OpenAIResponsesAdapter) EstimateTokens(text string) int { return len(text) / 4 }

func (p *OpenAIResponsesAdapter) ContextWindow() int { return p.contextWindow }

func (p *OpenAIResponsesAdapter) MaxOutputTokens() int {
	return p.maxOutputTokens
}

func (p *OpenAIResponsesAdapter) resolveMaxTokens(m int) int {
	if m > 0 && m < p.maxOutputTokens {
		return m
	}
	return p.maxOutputTokens
}

func (p *OpenAIResponsesAdapter) buildInput(ctx context.Context, req *CompletionRequest) (responses.ResponseInputParam, error) {
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

func (p *OpenAIResponsesAdapter) buildInputContent(ctx context.Context, m Message) (responses.ResponseInputMessageContentListParam, error) {
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

func (p *OpenAIResponsesAdapter) openAIImageURL(ctx context.Context, block ContentBlock) (string, error) {
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

func (p *OpenAIResponsesAdapter) buildTools(tools []ToolDef) []responses.ToolUnionParam {
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

// Responses 的 arguments 事件只携带 item_id；因此调用状态始终以原生 output item 为键。
func mergeResponseToolCall(event responses.ResponseStreamEventUnion, calls map[string]*responseToolCall, order *[]string) {
	switch event.Type {
	case "response.output_item.added":
		if event.Item.Type == "function_call" {
			supplementResponseToolCall(calls, order, event.Item.ID, event.Item.CallID, event.Item.Name, event.Item.Arguments.OfString)
		}
	case "response.output_item.done":
		if event.Item.Type == "function_call" {
			upsertResponseToolCall(calls, order, event.Item.ID, event.Item.CallID, event.Item.Name, event.Item.Arguments.OfString, false)
		}
	case "response.function_call_arguments.delta":
		if event.Delta != "" {
			upsertResponseToolCall(calls, order, event.ItemID, "", "", event.Delta, true)
		}
	case "response.function_call_arguments.done":
		upsertResponseToolCall(calls, order, event.ItemID, "", event.Name, event.Arguments, false)
	}
}

// completed 中的 output 是流事件的最终快照，只补全缺失字段，不能重新聚合为新的调用。
func collectResponseOutputToolCalls(items []responses.ResponseOutputItemUnion, calls map[string]*responseToolCall, order *[]string) {
	for _, item := range items {
		if item.Type == "function_call" {
			supplementResponseToolCall(calls, order, item.ID, item.CallID, item.Name, item.Arguments.OfString)
		}
	}
}

func orderedResponseToolCalls(calls map[string]*responseToolCall, order []string) []ToolCall {
	toolCalls := make([]ToolCall, 0, len(order))
	seenCallIDs := make(map[string]bool, len(order))
	for _, itemID := range order {
		tc := calls[itemID]
		if tc == nil || tc.ID == "" || tc.Name == "" || seenCallIDs[tc.ID] {
			continue
		}
		seenCallIDs[tc.ID] = true
		toolCalls = append(toolCalls, ToolCall{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments.String()})
	}
	return toolCalls
}

// upsertResponseToolCall 仅以 itemID 定位状态；callID 是最终发送给工具执行器的 ID，不参与流式聚合。
func upsertResponseToolCall(calls map[string]*responseToolCall, order *[]string, itemID, callID, name, args string, appendArgs bool) {
	if itemID == "" {
		return
	}
	tc := calls[itemID]
	if tc == nil {
		tc = &responseToolCall{}
		calls[itemID] = tc
		*order = append(*order, itemID)
	}
	if callID != "" {
		tc.ID = callID
	}
	if name != "" {
		tc.Name = name
	}
	if appendArgs {
		tc.Arguments.WriteString(args)
		return
	}
	tc.Arguments.Reset()
	tc.Arguments.WriteString(args)
}

func supplementResponseToolCall(calls map[string]*responseToolCall, order *[]string, itemID, callID, name, args string) {
	if itemID == "" {
		return
	}
	tc := calls[itemID]
	if tc == nil {
		tc = &responseToolCall{}
		calls[itemID] = tc
		*order = append(*order, itemID)
	}
	if tc.ID == "" {
		tc.ID = callID
	}
	if tc.Name == "" {
		tc.Name = name
	}
	if tc.Arguments.Len() == 0 && args != "" {
		tc.Arguments.WriteString(args)
	}
}
