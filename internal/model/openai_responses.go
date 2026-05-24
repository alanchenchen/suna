package model

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const openAIResponsesURL = "https://api.openai.com/v1/responses"

type OpenAIResponsesProvider struct {
	apiKey        string
	model         string
	contextWindow int
	httpClient    *http.Client
}

func NewOpenAIResponsesProvider(apiKey, model string, contextWindow int) *OpenAIResponsesProvider {
	return &OpenAIResponsesProvider{
		apiKey:        apiKey,
		model:         model,
		contextWindow: contextWindow,
		httpClient: &http.Client{Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		}},
	}
}

func (p *OpenAIResponsesProvider) Complete(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error) {
	ch := make(chan Chunk, 64)
	body, err := p.buildRequest(req)
	if err != nil {
		return nil, err
	}

	go func() {
		defer close(ch)
		started := time.Now()

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, openAIResponsesURL, bytes.NewReader(body))
		if err != nil {
			logLLMFailure(req, err, loggingFields(started, nil))
			ch <- Chunk{Done: true, Error: err.Error()}
			return
		}
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := p.httpClient.Do(httpReq)
		if err != nil {
			logLLMFailure(req, err, loggingFields(started, nil))
			ch <- Chunk{Done: true, Error: err.Error()}
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			err := responseStatusError(resp)
			logLLMFailure(req, err, loggingFields(started, nil))
			ch <- Chunk{Done: true, Error: err.Error()}
			return
		}

		usage, toolCalls, err := p.readResponseStream(resp.Body, ch)
		if err != nil {
			logLLMFailure(req, err, loggingFields(started, usage))
			ch <- Chunk{Done: true, Error: err.Error()}
			return
		}

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

func (p *OpenAIResponsesProvider) EstimateTokens(text string) int {
	return len(text) / 4
}

func (p *OpenAIResponsesProvider) ContextWindow() int {
	if p.contextWindow > 0 {
		return p.contextWindow
	}
	return 128000
}

func (p *OpenAIResponsesProvider) buildRequest(req *CompletionRequest) ([]byte, error) {
	payload := map[string]any{
		"model":               p.resolveModel(req.Model),
		"input":               p.buildInput(req),
		"max_output_tokens":   p.resolveMaxTokens(req.MaxTokens),
		"temperature":         p.resolveTemperature(req.Temperature),
		"stream":              true,
		"parallel_tool_calls": true,
	}
	if req.System != "" {
		payload["instructions"] = req.System
	}
	if tools := p.buildTools(req.Tools); len(tools) > 0 {
		payload["tools"] = tools
	}
	return json.Marshal(payload)
}

func (p *OpenAIResponsesProvider) resolveModel(m string) string {
	if m != "" {
		return m
	}
	return p.model
}

func (p *OpenAIResponsesProvider) resolveMaxTokens(m int) int {
	if m > 0 {
		return m
	}
	return 4096
}

func (p *OpenAIResponsesProvider) resolveTemperature(t float64) float64 {
	if t > 0 {
		return t
	}
	return 0.7
}

func (p *OpenAIResponsesProvider) buildInput(req *CompletionRequest) []map[string]any {
	input := make([]map[string]any, 0, len(req.Messages)+len(req.Messages))
	for _, m := range req.Messages {
		switch m.Role {
		case RoleUser:
			input = append(input, map[string]any{"role": "user", "content": p.buildInputContent(m)})
		case RoleAssistant:
			if len(m.ToolCalls) == 0 {
				input = append(input, map[string]any{"role": "assistant", "content": p.buildOutputContent(m)})
				continue
			}
			// Responses API 中 function_call 是独立 input item，不挂在 assistant message 字段上。
			if text := m.Text(); text != "" {
				input = append(input, map[string]any{"role": "assistant", "content": p.buildOutputContent(m)})
			}
			for _, tc := range m.ToolCalls {
				input = append(input, map[string]any{
					"type":      "function_call",
					"call_id":   tc.ID,
					"name":      tc.Name,
					"arguments": tc.Arguments,
				})
			}
		case RoleTool:
			input = append(input, map[string]any{
				"type":    "function_call_output",
				"call_id": m.ToolCallID,
				"output":  m.Text(),
			})
		}
	}
	return input
}

func (p *OpenAIResponsesProvider) buildInputContent(m Message) []map[string]any {
	blocks := make([]map[string]any, 0, len(m.Content))
	for _, c := range m.Content {
		switch c.Type {
		case ContentText:
			if c.Text != "" {
				blocks = append(blocks, map[string]any{"type": "input_text", "text": c.Text})
			}
		case ContentImage:
			if imageURL := openAIImageURL(c); imageURL != "" {
				// Responses API 的图片统一使用 input_image.image_url；base64 由 openAIImageURL 转为 data URL。
				blocks = append(blocks, map[string]any{"type": "input_image", "image_url": imageURL})
			}
		}
	}
	if len(blocks) == 0 && m.TextContent != "" {
		blocks = append(blocks, map[string]any{"type": "input_text", "text": m.TextContent})
	}
	return blocks
}

func (p *OpenAIResponsesProvider) buildOutputContent(m Message) []map[string]any {
	text := m.Text()
	if text == "" {
		return nil
	}
	return []map[string]any{{"type": "output_text", "text": text}}
}

func (p *OpenAIResponsesProvider) buildTools(tools []ToolDef) []map[string]any {
	if len(tools) == 0 {
		return nil
	}
	result := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		result = append(result, map[string]any{
			"type":        "function",
			"name":        t.Name,
			"description": t.Description,
			"parameters":  t.Parameters,
		})
	}
	return result
}

func (p *OpenAIResponsesProvider) readResponseStream(body io.Reader, ch chan<- Chunk) (*Usage, []ToolCall, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 1024), 1024*1024*16)

	var usage *Usage
	toolCallsByID := map[string]*ToolCall{}
	var toolCallOrder []string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}

		var event responseStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return usage, nil, fmt.Errorf("decode responses stream: %w", err)
		}
		if event.Error != nil {
			return usage, nil, fmt.Errorf("responses error: %s", event.Error.Message)
		}
		if event.Type == "response.output_text.delta" && event.Delta != "" {
			ch <- Chunk{Content: event.Delta, Done: false}
		}
		if strings.Contains(event.Type, "function_call") {
			mergeResponseToolCall(event, toolCallsByID, &toolCallOrder)
		}
		if event.Response != nil && event.Response.Usage != nil {
			usage = event.Response.Usage.toUsage()
			collectResponseOutputToolCalls(event.Response.Output, toolCallsByID, &toolCallOrder)
		}
	}
	if err := scanner.Err(); err != nil {
		return usage, nil, fmt.Errorf("read responses stream: %w", err)
	}

	toolCalls := make([]ToolCall, 0, len(toolCallOrder))
	for _, id := range toolCallOrder {
		if tc := toolCallsByID[id]; tc != nil && tc.Name != "" {
			toolCalls = append(toolCalls, *tc)
		}
	}
	return usage, toolCalls, nil
}

type responseStreamEvent struct {
	Type        string                    `json:"type"`
	Delta       string                    `json:"delta"`
	ItemID      string                    `json:"item_id"`
	OutputIndex int                       `json:"output_index"`
	Name        string                    `json:"name"`
	Arguments   string                    `json:"arguments"`
	CallID      string                    `json:"call_id"`
	Item        *responseOutputItem       `json:"item"`
	Response    *responseCompletedPayload `json:"response"`
	Error       *responseErrorPayload     `json:"error"`
}

type responseCompletedPayload struct {
	Output []responseOutputItem  `json:"output"`
	Usage  *responseUsagePayload `json:"usage"`
}

type responseOutputItem struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type responseUsagePayload struct {
	InputTokens         int                        `json:"input_tokens"`
	OutputTokens        int                        `json:"output_tokens"`
	TotalTokens         int                        `json:"total_tokens"`
	InputTokensDetails  responseInputTokenDetails  `json:"input_tokens_details"`
	OutputTokensDetails responseOutputTokenDetails `json:"output_tokens_details"`
}

type responseInputTokenDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

type responseOutputTokenDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

type responseErrorPayload struct {
	Message string `json:"message"`
}

func (u responseUsagePayload) toUsage() *Usage {
	return &Usage{
		InputTokens:  u.InputTokens,
		OutputTokens: u.OutputTokens,
		CachedTokens: u.InputTokensDetails.CachedTokens,
		TotalTokens:  u.TotalTokens,
	}
}

func mergeResponseToolCall(event responseStreamEvent, calls map[string]*ToolCall, order *[]string) {
	if event.Item != nil && event.Item.Type == "function_call" {
		upsertResponseToolCall(calls, order, event.Item.ID, event.Item.CallID, event.Item.Name, event.Item.Arguments, false)
	}
	if event.Type == "response.function_call_arguments.delta" && event.Delta != "" {
		upsertResponseToolCall(calls, order, event.ItemID, event.CallID, event.Name, event.Delta, true)
	}
	if event.Type == "response.function_call_arguments.done" {
		upsertResponseToolCall(calls, order, event.ItemID, event.CallID, event.Name, event.Arguments, false)
	}
}

func collectResponseOutputToolCalls(items []responseOutputItem, calls map[string]*ToolCall, order *[]string) {
	for _, item := range items {
		if item.Type == "function_call" {
			upsertResponseToolCall(calls, order, item.ID, item.CallID, item.Name, item.Arguments, false)
		}
	}
}

func upsertResponseToolCall(calls map[string]*ToolCall, order *[]string, itemID, callID, name, args string, appendArgs bool) {
	key := callID
	if key == "" {
		key = itemID
	}
	if key == "" {
		key = fmt.Sprintf("call_%d", len(*order))
	}
	tc, ok := calls[key]
	if !ok {
		calls[key] = &ToolCall{ID: key}
		*order = append(*order, key)
		tc = calls[key]
	}
	if name != "" {
		tc.Name = name
	}
	if appendArgs {
		tc.Arguments += args
	} else if args != "" {
		tc.Arguments = args
	}
}

func responseStatusError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = resp.Status
	}
	return fmt.Errorf("openai responses error: %s", message)
}
