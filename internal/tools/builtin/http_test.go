package builtin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alanchenchen/suna/internal/version"
)

func TestHTTPDefaultsToGETAndReturnsMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		fmt.Fprint(w, "ok")
	}))
	defer server.Close()

	res := HTTP{}.Execute(context.Background(), map[string]any{"url": server.URL})
	if res.IsError {
		t.Fatalf("HTTP.Execute() error = %s", res.Error)
	}
	if got := res.Metadata["kind"]; got != "http_response" {
		t.Fatalf("metadata kind = %#v, want http_response", got)
	}
	if got := res.Metadata["method"]; got != "GET" {
		t.Fatalf("metadata method = %#v, want GET", got)
	}
	if got := res.Metadata["status"]; got != 200 {
		t.Fatalf("metadata status = %#v, want 200", got)
	}
}

func TestHTTPUsesBuildVersionUnlessUserAgentIsProvided(t *testing.T) {
	for _, tt := range []struct {
		name    string
		headers map[string]any
		want    string
	}{
		{name: "default", want: "Suna/" + version.Current()},
		{name: "custom", headers: map[string]any{"user-agent": "custom-client/test"}, want: "custom-client/test"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			agents := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				agents <- r.UserAgent()
				fmt.Fprint(w, "ok")
			}))
			defer server.Close()

			res := HTTP{}.Execute(context.Background(), map[string]any{"url": server.URL, "headers": tt.headers})
			if res.IsError {
				t.Fatalf("HTTP.Execute() error = %s", res.Error)
			}
			if got := <-agents; got != tt.want {
				t.Fatalf("User-Agent = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHTTPRejectsUnsupportedMethod(t *testing.T) {
	res := HTTP{}.Execute(context.Background(), map[string]any{"url": "http://example.com", "method": "TRACE"})
	if !res.IsError {
		t.Fatalf("HTTP.Execute().IsError = false, want true")
	}
}
