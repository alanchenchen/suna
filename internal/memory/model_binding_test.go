package memory

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/model"
)

func newTestModelBinding(t *testing.T, text string, capture func([]byte)) *model.ModelBinding {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			return
		}
		if capture != nil {
			capture(body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"id":"test","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"content":` + quoteJSONString(text) + `}}]}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"id":"test","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	cfg := &config.Config{Models: []config.ModelConfig{{
		Provider:        "test",
		Protocol:        config.ModelProtocolOpenAIChat,
		Model:           "model",
		BaseURL:         server.URL,
		ContextWindow:   128000,
		MaxOutputTokens: 8192,
		APIKey:          "test-key",
	}}}
	router, err := model.NewRouter(cfg, nil)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	binding, err := router.Bind("test/model")
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	return binding
}

func quoteJSONString(value string) string {
	out := make([]byte, 0, len(value)+2)
	out = append(out, '"')
	for _, b := range []byte(value) {
		switch b {
		case '\\', '"':
			out = append(out, '\\', b)
		case '\n':
			out = append(out, '\\', 'n')
		case '\r':
			out = append(out, '\\', 'r')
		case '\t':
			out = append(out, '\\', 't')
		default:
			out = append(out, b)
		}
	}
	out = append(out, '"')
	return string(out)
}
