package model

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/sashabaranov/go-openai"
)

type OpenAIProvider struct {
	client        *openai.Client
	model         string
	contextWindow int
	baseURL       string
	apiKey        string
	supportsEmbed bool
	embedChecked  bool
	embedModel    string
	httpClient    *http.Client
}

func NewOpenAIProvider(apiKey, baseURL, model string, contextWindow int) *OpenAIProvider {
	cfg := openai.DefaultConfig(apiKey)
	if baseURL != "" {
		cfg.BaseURL = baseURL
	}

	httpClient := &http.Client{}
	httpClient.Transport = &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	cfg.HTTPClient = httpClient

	client := openai.NewClientWithConfig(cfg)
	return &OpenAIProvider{
		client:        client,
		model:         model,
		contextWindow: contextWindow,
		baseURL:       baseURL,
		apiKey:        apiKey,
		httpClient:    httpClient,
	}
}

func (p *OpenAIProvider) Complete(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error) {
	ch := make(chan Chunk, 64)

	messages := p.buildMessages(req)
	tools := p.buildTools(req.Tools)

	go func() {
		defer close(ch)

		stream, err := p.client.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
			Model:         p.resolveModel(req.Model),
			Messages:      messages,
			Tools:         tools,
			MaxTokens:     p.resolveMaxTokens(req.MaxTokens),
			Temperature:   float32(p.resolveTemperature(req.Temperature)),
			Stream:        true,
			StreamOptions: &openai.StreamOptions{IncludeUsage: true},
		})
		if err != nil {
			ch <- Chunk{Done: true, Error: err.Error()}
			return
		}
		defer stream.Close()

		var toolCallsAcc map[int]*openai.ToolCall
		var usage Usage
		var sawStop bool

		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				if len(toolCallsAcc) > 0 {
					ch <- Chunk{
						ToolCalls: p.accumulateToolCalls(toolCallsAcc),
						Done:      false,
					}
				}
				if usage.InputTokens > 0 || usage.OutputTokens > 0 {
					ch <- Chunk{Done: true, Usage: &usage}
				} else {
					ch <- Chunk{Done: true}
				}
				return
			}
			if err != nil {
				ch <- Chunk{Done: true, Error: fmt.Sprintf("stream error: %v", err)}
				return
			}

			if resp.Usage != nil {
				usage.InputTokens = resp.Usage.PromptTokens
				usage.OutputTokens = resp.Usage.CompletionTokens
				usage.TotalTokens = resp.Usage.TotalTokens
				if resp.Usage.PromptTokensDetails != nil {
					usage.CachedTokens = resp.Usage.PromptTokensDetails.CachedTokens
				}
			}

			if len(resp.Choices) == 0 {
				if sawStop && (usage.InputTokens > 0 || usage.OutputTokens > 0 || usage.TotalTokens > 0) {
					ch <- Chunk{Done: true, Usage: &usage}
					return
				}
				continue
			}
			choice := resp.Choices[0]

			if choice.Delta.Content != "" {
				ch <- Chunk{Content: choice.Delta.Content, Done: false}
			}

			if choice.Delta.ReasoningContent != "" {
				ch <- Chunk{ReasoningContent: choice.Delta.ReasoningContent, Done: false}
			}

			for _, tc := range choice.Delta.ToolCalls {
				if toolCallsAcc == nil {
					toolCallsAcc = make(map[int]*openai.ToolCall)
				}
				idx := 0
				if tc.Index != nil {
					idx = *tc.Index
				}
				existing, ok := toolCallsAcc[idx]
				if !ok {
					toolCallsAcc[idx] = &openai.ToolCall{
						ID:   tc.ID,
						Type: tc.Type,
						Function: openai.FunctionCall{
							Name:      tc.Function.Name,
							Arguments: tc.Function.Arguments,
						},
					}
				} else {
					if tc.ID != "" {
						existing.ID = tc.ID
					}
					if tc.Function.Name != "" {
						existing.Function.Name = tc.Function.Name
					}
					if tc.Function.Arguments != "" {
						existing.Function.Arguments += tc.Function.Arguments
					}
				}
			}

			if choice.FinishReason == "tool_calls" || choice.FinishReason == "stop" {
				if len(toolCallsAcc) > 0 {
					ch <- Chunk{
						ToolCalls: p.accumulateToolCalls(toolCallsAcc),
						Done:      false,
					}
					toolCallsAcc = nil
				}
				if choice.FinishReason == "stop" {
					sawStop = true
					if usage.InputTokens > 0 || usage.OutputTokens > 0 || usage.TotalTokens > 0 {
						ch <- Chunk{Done: true, Usage: &usage}
						return
					}
				}
			}
		}
	}()

	return ch, nil
}

func (p *OpenAIProvider) EstimateTokens(text string) int {
	return len(text) / 4
}

func (p *OpenAIProvider) ContextWindow() int {
	if p.contextWindow > 0 {
		return p.contextWindow
	}
	return 128000
}

func (p *OpenAIProvider) SupportsEmbedding() bool {
	if p.embedChecked {
		return p.supportsEmbed
	}
	if p.baseURL == "" {
		p.supportsEmbed = true
		p.embedChecked = true
		return true
	}
	p.detectEmbedding()
	p.embedChecked = true
	return p.supportsEmbed
}

func (p *OpenAIProvider) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	if !p.SupportsEmbedding() {
		return nil, fmt.Errorf("embedding not supported by this provider")
	}
	embedModel := p.embedModel
	if embedModel == "" {
		embedModel = "text-embedding-3-small"
	}
	resp, err := p.client.CreateEmbeddings(ctx, openai.EmbeddingRequestStrings{
		Input: texts,
		Model: openai.EmbeddingModel(embedModel),
	})
	if err != nil {
		return nil, fmt.Errorf("embedding API: %w", err)
	}
	result := make([][]float64, len(resp.Data))
	for i, d := range resp.Data {
		result[i] = make([]float64, len(d.Embedding))
		for j, v := range d.Embedding {
			result[i][j] = float64(v)
		}
	}
	return result, nil
}

func (p *OpenAIProvider) detectEmbedding() {
	p.embedModel = resolveEmbedModel(p.baseURL)

	reqBody := fmt.Sprintf(`{"input":"hi","model":"%s"}`, p.embedModel)
	endpoint := strings.TrimRight(p.baseURL, "/") + "/embeddings"
	if p.baseURL == "" {
		endpoint = "https://api.openai.com/v1/embeddings"
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, strings.NewReader(reqBody))
	if err != nil {
		p.supportsEmbed = false
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	client := p.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		p.supportsEmbed = false
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		p.supportsEmbed = true
		return
	}
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		p.supportsEmbed = false
		return
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusBadRequest {
		p.supportsEmbed = true
		return
	}
	p.supportsEmbed = false
}

func resolveEmbedModel(baseURL string) string {
	switch {
	case strings.Contains(baseURL, "open.bigmodel.cn"):
		return "embedding-3"
	case strings.Contains(baseURL, "openai.com"):
		return "text-embedding-3-small"
	case strings.Contains(baseURL, "dashscope.aliyuncs.com"):
		return "text-embedding-v3"
	case strings.Contains(baseURL, "deepseek.com"):
		return "deepseek-chat"
	default:
		return "text-embedding-3-small"
	}
}

func (p *OpenAIProvider) resolveModel(m string) string {
	if m != "" {
		return m
	}
	return p.model
}

func (p *OpenAIProvider) resolveMaxTokens(m int) int {
	if m > 0 {
		return m
	}
	return 4096
}

func (p *OpenAIProvider) resolveTemperature(t float64) float64 {
	if t > 0 {
		return t
	}
	return 0.7
}

func (p *OpenAIProvider) buildMessages(req *CompletionRequest) []openai.ChatCompletionMessage {
	msgs := make([]openai.ChatCompletionMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: req.System,
		})
	}
	for _, m := range req.Messages {
		msg := openai.ChatCompletionMessage{
			Role: string(m.Role),
		}
		if len(m.ToolCalls) > 0 {
			msg.ToolCalls = make([]openai.ToolCall, len(m.ToolCalls))
			for i, tc := range m.ToolCalls {
				msg.ToolCalls[i] = openai.ToolCall{
					ID:   tc.ID,
					Type: openai.ToolTypeFunction,
					Function: openai.FunctionCall{
						Name:      tc.Name,
						Arguments: tc.Arguments,
					},
				}
			}
		}
		if m.ToolCallID != "" {
			msg.ToolCallID = m.ToolCallID
		}
		if len(m.Content) > 0 {
			var textParts []string
			for _, c := range m.Content {
				if c.Type == ContentText {
					textParts = append(textParts, c.Text)
				}
			}
			msg.Content = joinStrings(textParts, "\n")
		} else if m.TextContent != "" {
			msg.Content = m.TextContent
		}
		msgs = append(msgs, msg)
	}
	return msgs
}

func (p *OpenAIProvider) buildTools(tools []ToolDef) []openai.Tool {
	if len(tools) == 0 {
		return nil
	}
	result := make([]openai.Tool, len(tools))
	for i, t := range tools {
		params, _ := json.Marshal(t.Parameters)
		result[i] = openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  json.RawMessage(params),
			},
		}
	}
	return result
}

func (p *OpenAIProvider) accumulateToolCalls(acc map[int]*openai.ToolCall) []ToolCall {
	indexes := make([]int, 0, len(acc))
	for idx := range acc {
		indexes = append(indexes, idx)
	}
	sort.Ints(indexes)
	result := make([]ToolCall, 0, len(acc))
	for _, idx := range indexes {
		tc := acc[idx]
		result = append(result, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}
	return result
}

func joinStrings(parts []string, sep string) string {
	result := ""
	for i, s := range parts {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}
