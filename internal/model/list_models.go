package model

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"sort"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"github.com/alanchenchen/suna/internal/config"
)

// ListModels 使用模型配置的凭据拉取 provider 的可用模型列表。
// 只返回模型 ID；API Key 只在 daemon 侧使用，不暴露给调用方。
// SDK 的 ModelService.List 按协议自动拼接路径（OpenAI: {base}/models，
// Anthropic: {base}/v1/models），与模型请求的 base_url 语义一致。
func ListModels(ctx context.Context, spec AdapterSpec) ([]string, error) {
	httpClient := compatibleHTTPClient(&http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}})
	switch spec.Protocol {
	case config.ModelProtocolOpenAIChat, config.ModelProtocolOpenAIResponses:
		opts := []option.RequestOption{option.WithAPIKey(spec.APIKey), option.WithHTTPClient(httpClient), option.WithMaxRetries(0)}
		if spec.BaseURL != "" {
			opts = append(opts, option.WithBaseURL(spec.BaseURL))
		}
		client := openai.NewClient(opts...)
		page, err := client.Models.List(ctx)
		if err != nil {
			return nil, err
		}
		return modelIDs(page.Data, func(m openai.Model) string { return m.ID }), nil
	case config.ModelProtocolAnthropic:
		opts := []anthropicoption.RequestOption{anthropicoption.WithHTTPClient(httpClient), anthropicoption.WithMaxRetries(0)}
		// 与 AnthropicAdapter 的凭据头语义保持一致：默认 X-Api-Key，
		// Bearer 与双头模式由用户显式选择。
		switch spec.AuthMode {
		case config.AuthModeBearer:
			opts = append(opts, anthropicoption.WithAuthToken(spec.APIKey))
		case config.AuthModeBoth:
			opts = append(opts, anthropicoption.WithAPIKey(spec.APIKey), anthropicoption.WithAuthToken(spec.APIKey))
		default:
			opts = append(opts, anthropicoption.WithAPIKey(spec.APIKey))
		}
		if spec.BaseURL != "" {
			opts = append(opts, anthropicoption.WithBaseURL(spec.BaseURL))
		}
		client := anthropic.NewClient(opts...)
		page, err := client.Models.List(ctx, anthropic.ModelListParams{})
		if err != nil {
			return nil, err
		}
		return modelIDs(page.Data, func(m anthropic.ModelInfo) string { return m.ID }), nil
	default:
		return nil, fmt.Errorf("protocol %q does not support model listing", spec.Protocol)
	}
}

// modelIDs 提取模型 ID 并排序去重，保证返回列表稳定。
func modelIDs[T any](items []T, id func(T) string) []string {
	seen := make(map[string]bool, len(items))
	models := make([]string, 0, len(items))
	for _, item := range items {
		id := id(item)
		if id != "" && !seen[id] {
			seen[id] = true
			models = append(models, id)
		}
	}
	sort.Strings(models)
	return models
}
