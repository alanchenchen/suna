package daemon

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/logging"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/protocol"
)

// modelDiscoveryTimeout 是模型列表拉取的超时；有界阻塞在独立 goroutine，
// 不阻塞 ServeConn 的连接循环。
const modelDiscoveryTimeout = 15 * time.Second

// handleConfigDiscoverModels 是 config.discoverModels 的同步入口：校验凭据后
// 立即返回，实际拉取在独立 goroutine 执行，结果通过 config.models_result 通知
// 回传发起者，避免 15s 网络请求阻塞连接循环（compact 异步化的同一模式）。
func (s *service) handleConfigDiscoverModels(ctx context.Context, req protocol.Request, sink protocol.EventSink) (any, error) {
	var params protocol.ConfigDiscoverModelsParams
	if err := decodeParams(req.Params, &params); err != nil {
		return nil, protocol.InvalidRequest(err.Error())
	}
	provider := strings.TrimSpace(params.Provider)
	if provider == "" {
		return nil, protocol.InvalidRequest("provider is required")
	}
	mc, ok := s.modelConfigByProvider(provider)
	if !ok {
		return nil, protocol.InvalidRequest("provider not found")
	}
	if _, err := mc.ResolveAPIKey(); err != nil {
		return nil, protocol.InvalidRequest(err.Error())
	}
	go s.runModelDiscovery(ctx, provider, mc, sink)
	return protocol.ConfigDiscoverModelsResult{Status: "processing"}, nil
}

// runModelDiscovery 在独立 goroutine 执行模型列表拉取，完成后通知发起者。
// 错误信息脱敏（SDK 错误体可能回显 API Key），只返回模型 ID。
func (s *service) runModelDiscovery(ctx context.Context, provider string, mc config.ModelConfig, sink protocol.EventSink) {
	// goroutine 内 panic 不能带崩 daemon；与 compact 同模式：记日志并通知错误，
	// 避免 TUI 浮层卡在加载态。
	defer func() {
		if r := recover(); r != nil {
			logging.Error("model", "discover_models_panic", fmt.Errorf("%v", r), logging.Event{"provider": provider})
			notifyCtx := context.WithoutCancel(ctx)
			emit(notifyCtx, sink, protocol.NotifyConfigModelsResult, protocol.ConfigModelsResultParams{
				Provider:     provider,
				ErrorMessage: "model discovery failed unexpectedly",
			})
		}
	}()
	notify := func(models []string, errMsg string) {
		// 通知用无取消 ctx：连接可能已断开，但发起者仍应收到结果或错误。
		notifyCtx := context.WithoutCancel(ctx)
		emit(notifyCtx, sink, protocol.NotifyConfigModelsResult, protocol.ConfigModelsResultParams{
			Provider:     provider,
			Models:       models,
			ErrorMessage: errMsg,
		})
	}
	discoveryCtx, cancel := context.WithTimeout(ctx, modelDiscoveryTimeout)
	defer cancel()
	models, err := model.ListModels(discoveryCtx, model.AdapterSpec{
		Protocol:        mc.ProtocolOrDefault(),
		AuthMode:        mc.AuthMode,
		BaseURL:         mc.BaseURL,
		APIKey:          mc.APIKey,
		ContextWindow:   mc.ContextWindow,
		MaxOutputTokens: mc.MaxOutputTokens,
	})
	if err != nil {
		logging.Error("model", "discover_models_failed", err, logging.Event{"provider": provider})
		notify(nil, redactModelDiscoveryError(err, mc.APIKey))
		return
	}
	notify(models, "")
}

// modelConfigByProvider 返回指定 provider 的第一个模型条目。
// 同 provider 的 key/base_url/protocol 一致（不一致时模型请求必然失败），
// 取第一个条目即可定位凭据。
func (s *service) modelConfigByProvider(provider string) (config.ModelConfig, bool) {
	cfg := s.daemon.agent.Config()
	if cfg == nil {
		return config.ModelConfig{}, false
	}
	for _, mc := range cfg.Models {
		if mc.Provider == provider {
			return mc, true
		}
	}
	return config.ModelConfig{}, false
}

// redactModelDiscoveryError 替换错误信息中的 API Key，避免 SDK 错误体回显凭据。
// 错误信息是协议层输出（第三方客户端消费），必须保持英文。
func redactModelDiscoveryError(err error, apiKey string) string {
	text := strings.TrimSpace(err.Error())
	key := strings.TrimSpace(apiKey)
	if key != "" {
		text = strings.ReplaceAll(text, key, "[REDACTED]")
		text = strings.ReplaceAll(text, "Bearer "+key, "Bearer [REDACTED]")
	}
	if text == "" {
		return "failed to fetch model list from endpoint"
	}
	return text
}
