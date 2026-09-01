package model

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/config"
)

func TestListModelsOpenAI(t *testing.T) {
	var gotAuth, gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-5.6-sol"},{"id":"gpt-5.2"}],"object":"list"}`))
	}))
	defer upstream.Close()

	models, err := ListModels(context.Background(), AdapterSpec{
		Protocol: config.ModelProtocolOpenAIChat,
		BaseURL:  upstream.URL + "/v1",
		APIKey:   "test-api-key",
	})
	if err != nil {
		t.Fatalf("ListModels error = %v", err)
	}
	if got, want := strings.Join(models, ","), "gpt-5.2,gpt-5.6-sol"; got != want {
		t.Fatalf("models = %q, want %q", got, want)
	}
	if gotPath != "/v1/models" {
		t.Fatalf("path = %q, want /v1/models", gotPath)
	}
	if gotAuth != "Bearer test-api-key" {
		t.Fatalf("Authorization = %q, want Bearer test-api-key", gotAuth)
	}
}

func TestListModelsAnthropic(t *testing.T) {
	var gotAuth, gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("X-Api-Key")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-sonnet-4"}],"has_more":false}`))
	}))
	defer upstream.Close()

	models, err := ListModels(context.Background(), AdapterSpec{
		Protocol: config.ModelProtocolAnthropic,
		BaseURL:  upstream.URL,
		APIKey:   "test-api-key",
	})
	if err != nil {
		t.Fatalf("ListModels error = %v", err)
	}
	if got, want := strings.Join(models, ","), "claude-sonnet-4"; got != want {
		t.Fatalf("models = %q, want %q", got, want)
	}
	if gotPath != "/v1/models" {
		t.Fatalf("path = %q, want /v1/models", gotPath)
	}
	if gotAuth != "test-api-key" {
		t.Fatalf("X-Api-Key = %q, want test-api-key", gotAuth)
	}
}

func TestListModelsUnsupportedProtocol(t *testing.T) {
	_, err := ListModels(context.Background(), AdapterSpec{
		Protocol: config.ModelProtocol("custom"),
		BaseURL:  "https://api.example.com/v1",
		APIKey:   "test-api-key",
	})
	if err == nil || !strings.Contains(err.Error(), "does not support model listing") {
		t.Fatalf("error = %v, want unsupported protocol", err)
	}
}

func TestListModelsDeduplicatesAndSorts(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"b-model"},{"id":"a-model"},{"id":"b-model"}],"object":"list"}`))
	}))
	defer upstream.Close()

	models, err := ListModels(context.Background(), AdapterSpec{
		Protocol: config.ModelProtocolOpenAIChat,
		BaseURL:  upstream.URL + "/v1",
		APIKey:   "test-api-key",
	})
	if err != nil {
		t.Fatalf("ListModels error = %v", err)
	}
	if got, want := strings.Join(models, ","), "a-model,b-model"; got != want {
		t.Fatalf("models = %q, want %q", got, want)
	}
}
