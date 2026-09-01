package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/agent"
	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/protocol"
)

// newDiscoveryTestDaemon 构造带模型配置的 daemon，供 discoverModels 测试使用。
func newDiscoveryTestDaemon(t *testing.T, models []config.ModelConfig) *Daemon {
	t.Helper()
	cfg := &config.Config{
		DataDir:     t.TempDir(),
		ActiveModel: "test/model",
		Models:      models,
	}
	if err := cfg.EnsureDataDirs(); err != nil {
		t.Fatalf("EnsureDataDirs error = %v", err)
	}
	ag, err := agent.NewAgent(cfg)
	if err != nil {
		t.Fatalf("NewAgent error = %v", err)
	}
	t.Cleanup(func() { ag.Close() })
	return &Daemon{state: protocol.DaemonRuntimeReady, agent: ag, sinks: map[string]protocol.EventSink{}}
}

// waitForModelsResult 轮询 sink 直到收到 config.models_result 通知。
func waitForModelsResult(t *testing.T, sink *captureEventSink) protocol.ConfigModelsResultParams {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, event := range sink.Events() {
			if event.Method != protocol.NotifyConfigModelsResult {
				continue
			}
			params, ok := event.Params.(protocol.ConfigModelsResultParams)
			if !ok {
				t.Fatalf("models_result params = %T, want ConfigModelsResultParams", event.Params)
			}
			return params
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for config.models_result notification")
	return protocol.ConfigModelsResultParams{}
}

func TestDiscoverModelsOpenAIEndpoint(t *testing.T) {
	var gotAuth, gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-5.6-sol"},{"id":"gpt-5.2"}],"object":"list"}`))
	}))
	defer upstream.Close()

	d := newDiscoveryTestDaemon(t, []config.ModelConfig{{
		Provider: "test", Protocol: config.ModelProtocolOpenAIChat, Model: "gpt-5.2",
		BaseURL: upstream.URL + "/v1", APIKey: "test-api-key",
	}})
	svc := newService(d)
	sink := &captureEventSink{}
	result, err := svc.handleConfigDiscoverModels(context.Background(), protocol.Request{Params: protocol.ConfigDiscoverModelsParams{Provider: "test"}}, sink)
	if err != nil {
		t.Fatalf("handleConfigDiscoverModels error = %v", err)
	}
	if got := result.(protocol.ConfigDiscoverModelsResult).Status; got != "processing" {
		t.Fatalf("status = %q, want processing", got)
	}
	params := waitForModelsResult(t, sink)
	if params.ErrorMessage != "" {
		t.Fatalf("error_message = %q, want empty", params.ErrorMessage)
	}
	if got, want := strings.Join(params.Models, ","), "gpt-5.2,gpt-5.6-sol"; got != want {
		t.Fatalf("models = %q, want %q", got, want)
	}
	if gotPath != "/v1/models" {
		t.Fatalf("path = %q, want /v1/models", gotPath)
	}
	if gotAuth != "Bearer test-api-key" {
		t.Fatalf("Authorization = %q, want Bearer test-api-key", gotAuth)
	}
}

func TestDiscoverModelsAnthropicEndpoint(t *testing.T) {
	var gotAuth, gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("X-Api-Key")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-sonnet-4"}],"has_more":false}`))
	}))
	defer upstream.Close()

	d := newDiscoveryTestDaemon(t, []config.ModelConfig{{
		Provider: "test", Protocol: config.ModelProtocolAnthropic, Model: "claude-sonnet-4",
		BaseURL: upstream.URL, APIKey: "test-api-key",
	}})
	svc := newService(d)
	sink := &captureEventSink{}
	if _, err := svc.handleConfigDiscoverModels(context.Background(), protocol.Request{Params: protocol.ConfigDiscoverModelsParams{Provider: "test"}}, sink); err != nil {
		t.Fatalf("handleConfigDiscoverModels error = %v", err)
	}
	params := waitForModelsResult(t, sink)
	if params.ErrorMessage != "" {
		t.Fatalf("error_message = %q, want empty", params.ErrorMessage)
	}
	if got, want := strings.Join(params.Models, ","), "claude-sonnet-4"; got != want {
		t.Fatalf("models = %q, want %q", got, want)
	}
	if gotPath != "/v1/models" {
		t.Fatalf("path = %q, want /v1/models", gotPath)
	}
	if gotAuth != "test-api-key" {
		t.Fatalf("X-Api-Key = %q, want test-api-key", gotAuth)
	}
}

// runModelDiscovery 在 goroutine 内出错（如协议不支持、网络失败）时必须回传
// 错误通知，否则 TUI 浮层会永久卡在加载态。
func TestDiscoverModelsGoroutineErrorNotifies(t *testing.T) {
	d := newDiscoveryTestDaemon(t, []config.ModelConfig{{
		Provider: "test", Protocol: config.ModelProtocolOpenAIChat, Model: "gpt-5.2",
		BaseURL: "https://api.example.com/v1", APIKey: "test-api-key",
	}})
	svc := newService(d)
	sink := &captureEventSink{}
	// 不支持的协议让 ListModels 同步返回错误，验证 goroutine 错误路径的通知回传。
	svc.runModelDiscovery(context.Background(), "test", config.ModelConfig{
		Provider: "test", Protocol: config.ModelProtocol("custom"),
	}, sink)
	params := waitForModelsResult(t, sink)
	if params.ErrorMessage == "" {
		t.Fatal("error_message = empty, want error notification")
	}
	if len(params.Models) != 0 {
		t.Fatalf("models = %v, want empty on error", params.Models)
	}
}

func TestDiscoverModelsProviderNotFound(t *testing.T) {
	d := newDiscoveryTestDaemon(t, []config.ModelConfig{{
		Provider: "test", Protocol: config.ModelProtocolOpenAIChat, Model: "gpt-5.2",
		BaseURL: "https://api.example.com/v1", APIKey: "test-api-key",
	}})
	svc := newService(d)
	_, err := svc.handleConfigDiscoverModels(context.Background(), protocol.Request{Params: protocol.ConfigDiscoverModelsParams{Provider: "missing"}}, &captureEventSink{})
	if err == nil || !strings.Contains(err.Error(), "provider not found") {
		t.Fatalf("error = %v, want provider not found", err)
	}
}

func TestDiscoverModelsErrorRedaction(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key test-api-key"}}`))
	}))
	defer upstream.Close()

	d := newDiscoveryTestDaemon(t, []config.ModelConfig{{
		Provider: "test", Protocol: config.ModelProtocolOpenAIChat, Model: "gpt-5.2",
		BaseURL: upstream.URL + "/v1", APIKey: "test-api-key",
	}})
	svc := newService(d)
	sink := &captureEventSink{}
	if _, err := svc.handleConfigDiscoverModels(context.Background(), protocol.Request{Params: protocol.ConfigDiscoverModelsParams{Provider: "test"}}, sink); err != nil {
		t.Fatalf("handleConfigDiscoverModels error = %v", err)
	}
	params := waitForModelsResult(t, sink)
	if params.ErrorMessage == "" {
		t.Fatal("error_message = empty, want redacted error")
	}
	if strings.Contains(params.ErrorMessage, "test-api-key") {
		t.Fatalf("error_message leaks api key: %q", params.ErrorMessage)
	}
	if !strings.Contains(params.ErrorMessage, "[REDACTED]") {
		t.Fatalf("error_message = %q, want [REDACTED] marker", params.ErrorMessage)
	}
}
