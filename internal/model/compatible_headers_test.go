package model

import (
	"net/http"
	"testing"

	"github.com/alanchenchen/suna/internal/version"
)

func TestNormalizeCompatibleHeadersUsesBuildVersionAndPreservesProtocolHeaders(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://api.example.com/v1/messages", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("User-Agent", "SDK/test")
	req.Header.Set("X-Stainless-Lang", "go")
	req.Header.Set("X-Stainless-Package-Version", "test")
	preserved := map[string]string{
		"Authorization":     "Bearer <TOKEN>",
		"X-Api-Key":         "test-api-key",
		"Content-Type":      "application/json",
		"Anthropic-Version": "2023-06-01",
	}
	for key, value := range preserved {
		req.Header.Set(key, value)
	}

	normalizeCompatibleHeaders(req)
	if got, want := req.UserAgent(), "Suna/"+version.Current(); got != want {
		t.Fatalf("User-Agent = %q, want %q", got, want)
	}
	for _, key := range []string{"X-Stainless-Lang", "X-Stainless-Package-Version"} {
		if got := req.Header.Get(key); got != "" {
			t.Fatalf("%s = %q, want empty", key, got)
		}
	}
	for key, want := range preserved {
		if got := req.Header.Get(key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
}
