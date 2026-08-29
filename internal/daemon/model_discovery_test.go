package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/config"
)

func TestDiscoverConfiguredModelsUsesSavedKey(t *testing.T) {
	t.Parallel()

	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %q, want /v1/models", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-5.6-sol"},{"id":"gpt-5.2"}],"object":"list"}`))
	}))
	defer upstream.Close()

	models, message := discoverConfiguredModels(context.Background(), &config.Config{
		Models: []config.ModelConfig{{
			Provider: "oiocode",
			Protocol: config.ModelProtocolOpenAIChat,
			Model:    "gpt-5.6-sol",
			BaseURL:  upstream.URL + "/v1",
			APIKey:   "test-token",
		}},
	}, "oiocode/gpt-5.6-sol")

	if message != "" {
		t.Fatalf("message = %q, want empty", message)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if got, want := strings.Join(models, ","), "gpt-5.2,gpt-5.6-sol"; got != want {
		t.Fatalf("models = %q, want %q", got, want)
	}
}

func TestDiscoverConfiguredModelsTriesCompatFallback(t *testing.T) {
	t.Parallel()

	var paths []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"fallback-model"}]}`))
	}))
	defer upstream.Close()

	models, message := discoverConfiguredModels(context.Background(), &config.Config{
		Models: []config.ModelConfig{{
			Provider: "deepseek",
			Protocol: config.ModelProtocolOpenAIChat,
			Model:    "deepseek-chat",
			BaseURL:  upstream.URL + "/anthropic",
			APIKey:   "test-token",
		}},
	}, "deepseek/deepseek-chat")

	if message != "" {
		t.Fatalf("message = %q, want empty", message)
	}
	if got, want := strings.Join(paths, ","), "/anthropic/v1/models,/v1/models"; got != want {
		t.Fatalf("paths = %q, want %q", got, want)
	}
	if got, want := strings.Join(models, ","), "fallback-model"; got != want {
		t.Fatalf("models = %q, want %q", got, want)
	}
}

func TestDiscoverConfiguredModelsRedactsSavedKeyOnFailure(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad token test-token", http.StatusUnauthorized)
	}))
	defer upstream.Close()

	_, message := discoverConfiguredModels(context.Background(), &config.Config{
		Models: []config.ModelConfig{{
			Provider: "oiocode",
			Protocol: config.ModelProtocolOpenAIChat,
			Model:    "gpt",
			BaseURL:  upstream.URL + "/v1",
			APIKey:   "test-token",
		}},
	}, "oiocode/gpt")

	if message == "" {
		t.Fatal("message is empty")
	}
	if strings.Contains(message, "test-token") {
		t.Fatalf("message leaked API key: %q", message)
	}
	if !strings.Contains(message, "HTTP Unauthorized") {
		t.Fatalf("message = %q, want HTTP Unauthorized", message)
	}
}

func TestModelDiscoveryEndpointsMatchCCSwitchCandidates(t *testing.T) {
	t.Parallel()

	tests := map[string][]string{
		"https://open.bigmodel.cn/api/coding/paas/v4": {
			"https://open.bigmodel.cn/api/coding/paas/v4/models",
			"https://open.bigmodel.cn/api/coding/paas/v4/v1/models",
		},
		"https://api.deepseek.com/anthropic": {
			"https://api.deepseek.com/anthropic/v1/models",
			"https://api.deepseek.com/v1/models",
			"https://api.deepseek.com/models",
		},
	}
	for input, want := range tests {
		got, err := modelDiscoveryEndpoints(input)
		if err != nil {
			t.Fatalf("modelDiscoveryEndpoints(%q) error = %v", input, err)
		}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("modelDiscoveryEndpoints(%q) = %#v, want %#v", input, got, want)
		}
	}
}

func TestConfigToParamsIncludesOnlyAPIKeyHint(t *testing.T) {
	t.Parallel()

	out := configToParams(&config.Config{
		Models: []config.ModelConfig{{
			Provider: "oiocode",
			Protocol: config.ModelProtocolOpenAIChat,
			Model:    "gpt",
			BaseURL:  "https://www.oiocode.com/v1",
			APIKey:   "sk-test-secret-3456",
		}},
	})

	if len(out.Models) != 1 {
		t.Fatalf("models len = %d, want 1", len(out.Models))
	}
	if !out.Models[0].HasAPIKey {
		t.Fatal("HasAPIKey = false, want true")
	}
	if got, want := out.Models[0].APIKeyHint, "sk-...3456"; got != want {
		t.Fatalf("APIKeyHint = %q, want %q", got, want)
	}
}
