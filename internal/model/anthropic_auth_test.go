package model

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alanchenchen/suna/internal/config"
)

func TestAnthropicAuthModeSelectsExpectedHeaders(t *testing.T) {
	tests := []struct {
		name              string
		authMode          config.AuthMode
		wantAPIKey        bool
		wantAuthorization bool
	}{
		{name: "default uses API key", wantAPIKey: true},
		{name: "bearer uses authorization", authMode: config.AuthModeBearer, wantAuthorization: true},
		{name: "both uses both headers", authMode: config.AuthModeBoth, wantAPIKey: true, wantAuthorization: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			var got http.Header
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Clone()
				http.Error(w, "test response", http.StatusUnauthorized)
			}))
			defer server.Close()

			spec := testAdapterSpec("claude-test")
			spec.BaseURL = server.URL
			spec.AuthMode = tt.authMode
			adapter := NewAnthropicAdapter(spec, AdapterDependencies{})
			chunks, err := adapter.Complete(context.Background(), CompletionRequest{
				Messages: []Message{{Role: RoleUser, TextContent: "hello"}},
			})
			if err != nil {
				t.Fatalf("Complete() error = %v", err)
			}
			for range chunks {
			}

			if got == nil {
				t.Fatal("request headers were not captured")
			}
			if has := got.Get("X-Api-Key") != ""; has != tt.wantAPIKey {
				t.Fatalf("X-Api-Key present = %v, want %v", has, tt.wantAPIKey)
			}
			if has := got.Get("Authorization") == "Bearer test-api-key"; has != tt.wantAuthorization {
				t.Fatalf("Authorization bearer present = %v, want %v; header = %q", has, tt.wantAuthorization, got.Get("Authorization"))
			}
		})
	}
}
