package builtin

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/alanchenchen/suna/internal/tools"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	maxHTTPBodySize = 100 * 1024
	httpTimeout     = 30 * time.Second
	maxRedirects    = 5
)

type ReadHTTP struct{}

func (ReadHTTP) Spec() tools.Spec {
	return builtinSpec("readhttp", "Send an HTTP GET request and return response status, headers, and body.", tools.Perceive, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url":     map[string]any{"type": "string", "description": "Request URL"},
			"headers": map[string]any{"type": "object", "description": "Request headers"},
			"timeout": map[string]any{"type": "integer", "description": "Timeout in seconds, default 30"},
		},
		"required": []string{"url"},
	})
}

func (ReadHTTP) Execute(ctx context.Context, params map[string]any) tools.Result {
	url, _ := params["url"].(string)
	if url == "" {
		return tools.ErrorResult("url is required")
	}

	timeout := httpTimeout
	if t, ok := params["timeout"].(float64); ok && int(t) > 0 {
		timeout = time.Duration(int(t)) * time.Second
	}

	headers := make(map[string]string)
	if h, ok := params["headers"].(map[string]any); ok {
		for k, v := range h {
			if s, ok := v.(string); ok {
				headers[k] = s
			}
		}
	}

	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("too many redirects (%d)", maxRedirects)
			}
			return nil
		},
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
		},
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("create request: %s", err))
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "Suna/1.0")
	}

	resp, err := client.Do(req)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("request failed: %s", err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHTTPBodySize+1))
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("read body: %s", err))
	}

	truncated := len(body) > maxHTTPBodySize
	if truncated {
		body = body[:maxHTTPBodySize]
	}

	var headerStrs []string
	for k, v := range resp.Header {
		headerStrs = append(headerStrs, fmt.Sprintf("%s: %s", k, strings.Join(v, ", ")))
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Status: %d %s\n", resp.StatusCode, resp.Status))
	sb.WriteString(fmt.Sprintf("Headers:\n  %s\n", strings.Join(headerStrs, "\n  ")))
	sb.WriteString(fmt.Sprintf("Body:\n%s", string(body)))
	if truncated {
		sb.WriteString("\n... (truncated at 100KB)")
	}

	return tools.Result{Content: sb.String(), Truncated: truncated}
}
