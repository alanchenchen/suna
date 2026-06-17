package model

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

type OpenAIChatProvider struct {
	client          openai.Client
	model           string
	contextWindow   int
	maxOutputTokens int
	media           MediaResolver
}

func NewOpenAIChatProvider(apiKey, baseURL, model string, contextWindow, maxOutputTokens int, mediaResolver MediaResolver) *OpenAIChatProvider {
	httpClient := compatibleHTTPClient(&http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}})
	// 关闭 SDK 隐式重试，避免一次 Suna Complete 在上游产生多次不可见请求；
	// 未来如需重试应由 Suna 自己实现并记录日志。
	opts := []option.RequestOption{option.WithAPIKey(apiKey), option.WithHTTPClient(httpClient), option.WithMaxRetries(0)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	return &OpenAIChatProvider{client: openai.NewClient(opts...), model: model, contextWindow: contextWindow, maxOutputTokens: maxOutputTokens, media: mediaResolver}
}

func (p *OpenAIChatProvider) Complete(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error) {
	if p.maxOutputTokens <= 0 {
		return nil, fmt.Errorf("max_output_tokens is required for model %q", p.model)
	}
	maxTokens := p.resolveMaxTokens(req.MaxTokens)
	req.MaxTokens = maxTokens
	messages, err := p.buildMessages(ctx, req)
	if err != nil {
		return nil, err
	}
	params := openai.ChatCompletionNewParams{
		Model:       openai.ChatModel(p.resolveModel(req.Model)),
		Messages:    messages,
		MaxTokens:   openai.Int(int64(maxTokens)),
		Temperature: openai.Float(p.resolveTemperature(req.Temperature)),
		StreamOptions: openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: openai.Bool(true),
		},
	}
	if tools := p.buildTools(req.Tools); len(tools) > 0 {
		params.Tools = tools
		params.ParallelToolCalls = openai.Bool(true)
	}
	opts, err := reasoningRequestOptions(req.Reasoning, chatGeneratedKeys())
	if err != nil {
		return nil, err
	}

	ch := make(chan Chunk, providerChunkBuffer)
	go func() {
		defer close(ch)
		started := time.Now()
		stream := p.client.Chat.Completions.NewStreaming(ctx, params, opts...)
		defer stream.Close()

		var usage *Usage
		var toolCallsAcc map[int]*chatToolCallAccum
		chunkCount := 0
		assistantBytes := 0
		reasoningBytes := 0
		usageReceived := false
		lastChunkAt := started
		for stream.Next() {
			chunkCount++
			lastChunkAt = time.Now()
			chunk := stream.Current()
			if chunk.JSON.Usage.Valid() {
				u := chunk.Usage
				usage = &Usage{InputTokens: int(u.PromptTokens), OutputTokens: int(u.CompletionTokens), TotalTokens: int(u.TotalTokens), CachedTokens: int(u.PromptTokensDetails.CachedTokens)}
				usageReceived = true
			}
			if len(chunk.Choices) == 0 {
				continue
			}
			choice := chunk.Choices[0]
			if choice.Delta.Content != "" {
				assistantBytes += len(choice.Delta.Content)
				ch <- Chunk{Content: choice.Delta.Content, Done: false}
			}
			if reasoning := chatReasoningContent(choice.Delta); reasoning != "" {
				reasoningBytes += len(reasoning)
				ch <- Chunk{ReasoningContent: reasoning, Done: false}
			}
			mergeChatToolDeltas(choice.Delta.ToolCalls, &toolCallsAcc)
		}
		if err := stream.Err(); err != nil {
			fields := loggingFields(started, usage)
			fields["chunk_count"] = chunkCount
			fields["assistant_bytes"] = assistantBytes
			fields["reasoning_bytes"] = reasoningBytes
			fields["usage_received"] = usageReceived
			fields["last_chunk_age_ms"] = time.Since(lastChunkAt).Milliseconds()
			logLLMFailure(req, err, fields)
			ch <- Chunk{Done: true, Error: err.Error()}
			return
		}
		toolCalls := accumulateChatToolCalls(toolCallsAcc)
		fields := loggingFields(started, usage)
		fields["tool_calls"] = len(toolCalls)
		logLLMSuccess(req, fields)
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

func chatGeneratedKeys() map[string]bool {
	return map[string]bool{"model": true, "messages": true, "max_tokens": true, "temperature": true, "stream": true, "stream_options": true, "tools": true}
}

func chatReasoningContent(delta openai.ChatCompletionChunkChoiceDelta) string {
	if reasoning := chatReasoningTextField(delta.JSON.ExtraFields["reasoning_content"]); reasoning != "" {
		return reasoning
	}
	return chatReasoningDetails(delta.JSON.ExtraFields["reasoning_details"])
}

func chatReasoningTextField(field interface{ Raw() string }) string {
	if field == nil || field.Raw() == "" || field.Raw() == "null" {
		return ""
	}
	var value string
	if err := json.Unmarshal([]byte(field.Raw()), &value); err != nil {
		return ""
	}
	return value
}

func chatReasoningDetails(field interface{ Raw() string }) string {
	if field == nil || field.Raw() == "" || field.Raw() == "null" {
		return ""
	}
	var details []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(field.Raw()), &details); err != nil {
		return ""
	}
	var parts []string
	for _, detail := range details {
		if detail.Text == "" {
			continue
		}
		if detail.Type != "" && detail.Type != "reasoning.text" {
			continue
		}
		parts = append(parts, detail.Text)
	}
	return strings.Join(parts, "")
}

func (p *OpenAIChatProvider) EstimateTokens(text string) int { return len(text) / 4 }

func (p *OpenAIChatProvider) ContextWindow() int { return p.contextWindow }

func (p *OpenAIChatProvider) MaxOutputTokens() int {
	return p.maxOutputTokens
}

func (p *OpenAIChatProvider) resolveModel(m string) string {
	if m != "" {
		return m
	}
	return p.model
}

func (p *OpenAIChatProvider) resolveMaxTokens(m int) int {
	if m > 0 && m < p.maxOutputTokens {
		return m
	}
	return p.maxOutputTokens
}

func (p *OpenAIChatProvider) resolveTemperature(t float64) float64 {
	if t > 0 {
		return t
	}
	return 0.7
}

func (p *OpenAIChatProvider) buildMessages(ctx context.Context, req *CompletionRequest) ([]openai.ChatCompletionMessageParamUnion, error) {
	msgs := make([]openai.ChatCompletionMessageParamUnion, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, openai.SystemMessage(req.System))
	}
	if state := FormatSessionStateForModel(req.SessionState); state != "" {
		msgs = append(msgs, openai.UserMessage(state))
	}
	for _, m := range req.Messages {
		switch m.Role {
		case RoleUser:
			msg, err := p.buildChatUserMessage(ctx, m)
			if err != nil {
				return nil, err
			}
			msgs = append(msgs, msg)
		case RoleAssistant:
			msgs = append(msgs, buildChatAssistantMessage(m))
		case RoleTool:
			msgs = append(msgs, openai.ToolMessage(m.Text(), m.ToolCallID))
		}
	}
	return msgs, nil
}

func (p *OpenAIChatProvider) buildChatUserMessage(ctx context.Context, m Message) (openai.ChatCompletionMessageParamUnion, error) {
	if !hasImagePart(m.Content) {
		return openai.UserMessage(m.Text()), nil
	}
	parts := make([]openai.ChatCompletionContentPartUnionParam, 0, len(m.Content))
	for _, c := range m.Content {
		switch c.Type {
		case ContentText:
			if c.Text != "" {
				parts = append(parts, openai.TextContentPart(c.Text))
			}
		case ContentImage:
			imageURL, err := p.openAIImageURL(ctx, c)
			if err != nil {
				return openai.ChatCompletionMessageParamUnion{}, err
			}
			if imageURL != "" {
				parts = append(parts, openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{URL: imageURL}))
			}
		}
	}
	if len(parts) == 0 && m.TextContent != "" {
		parts = append(parts, openai.TextContentPart(m.TextContent))
	}
	return openai.UserMessage(parts), nil
}

func buildChatAssistantMessage(m Message) openai.ChatCompletionMessageParamUnion {
	msg := openai.AssistantMessage(m.Text())
	if len(m.ToolCalls) == 0 || msg.OfAssistant == nil {
		return msg
	}
	msg.OfAssistant.ToolCalls = make([]openai.ChatCompletionMessageToolCallUnionParam, 0, len(m.ToolCalls))
	for _, tc := range m.ToolCalls {
		msg.OfAssistant.ToolCalls = append(msg.OfAssistant.ToolCalls, openai.ChatCompletionMessageToolCallUnionParam{OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
			ID: tc.ID,
			Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
				Name:      tc.Name,
				Arguments: tc.Arguments,
			},
		}})
	}
	return msg
}

func hasImagePart(blocks []ContentBlock) bool {
	for _, b := range blocks {
		if b.Type == ContentImage && b.Media != nil {
			return true
		}
	}
	return false
}

func (p *OpenAIChatProvider) openAIImageURL(ctx context.Context, block ContentBlock) (string, error) {
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

func (p *OpenAIChatProvider) buildTools(tools []ToolDef) []openai.ChatCompletionToolUnionParam {
	if len(tools) == 0 {
		return nil
	}
	result := make([]openai.ChatCompletionToolUnionParam, 0, len(tools))
	for _, t := range tools {
		result = append(result, openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{Name: t.Name, Description: openai.String(t.Description), Parameters: openai.FunctionParameters(t.Parameters)}))
	}
	return result
}

type chatToolCallAccum struct{ ID, Name, Arguments string }

func mergeChatToolDeltas(toolCalls []openai.ChatCompletionChunkChoiceDeltaToolCall, acc *map[int]*chatToolCallAccum) {
	for _, tc := range toolCalls {
		if *acc == nil {
			*acc = make(map[int]*chatToolCallAccum)
		}
		idx := int(tc.Index)
		existing, ok := (*acc)[idx]
		if !ok {
			existing = &chatToolCallAccum{}
			(*acc)[idx] = existing
		}
		if tc.ID != "" {
			existing.ID = tc.ID
		}
		if tc.Function.Name != "" {
			existing.Name = tc.Function.Name
		}
		if tc.Function.Arguments != "" {
			existing.Arguments += tc.Function.Arguments
		}
	}
}

func accumulateChatToolCalls(acc map[int]*chatToolCallAccum) []ToolCall {
	indexes := make([]int, 0, len(acc))
	for idx := range acc {
		indexes = append(indexes, idx)
	}
	sort.Ints(indexes)
	result := make([]ToolCall, 0, len(acc))
	for _, idx := range indexes {
		tc := acc[idx]
		if tc.Name == "" {
			continue
		}
		id := tc.ID
		if id == "" {
			id = fmt.Sprintf("call_%d", idx)
		}
		result = append(result, ToolCall{ID: id, Name: tc.Name, Arguments: tc.Arguments})
	}
	return result
}
