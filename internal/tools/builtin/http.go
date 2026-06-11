package builtin

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/alanchenchen/suna/internal/tools"
)

type HTTP struct{}

func (HTTP) Spec() tools.Spec {
	return builtinSpec("http", "Send an HTTP request and return response status, headers, and body.", tools.Act, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url":            map[string]any{"type": "string", "description": "Request URL"},
			"method":         map[string]any{"type": "string", "enum": []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"}, "description": "HTTP method, default GET"},
			"headers":        map[string]any{"type": "object", "description": "Request headers"},
			"body":           map[string]any{"type": "string", "description": "Request body"},
			"timeout":        map[string]any{"type": "integer", "description": "Timeout in seconds, default 30"},
			"max_body_bytes": map[string]any{"type": "integer", "description": "Maximum response body bytes, default 100KB"},
		},
		"required": []string{"url"},
	})
}

func (HTTP) Execute(ctx context.Context, params map[string]any) tools.Result {
	url, _ := params["url"].(string)
	if url == "" {
		return tools.ErrorResult("url is required")
	}
	method, _ := params["method"].(string)
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = "GET"
	}
	if !validHTTPMethod(method) {
		return tools.ErrorResult(fmt.Sprintf("unsupported HTTP method: %s", method))
	}

	timeout := httpTimeout
	if t, ok := params["timeout"].(float64); ok && int(t) > 0 {
		timeout = time.Duration(int(t)) * time.Second
	}
	maxBody := maxHTTPBodySize
	if n, ok := params["max_body_bytes"].(float64); ok && int(n) > 0 {
		maxBody = int(n)
	}

	headers := make(map[string]string)
	if h, ok := params["headers"].(map[string]any); ok {
		for k, v := range h {
			if s, ok := v.(string); ok {
				headers[k] = s
			}
		}
	}

	var bodyReader io.Reader
	if body, _ := params["body"].(string); body != "" {
		bodyReader = bytes.NewReader([]byte(body))
	}
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("too many redirects (%d)", maxRedirects)
			}
			return nil
		},
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: false}},
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
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

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBody)+1))
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("read body: %s", err))
	}
	truncated := len(body) > maxBody
	if truncated {
		body = body[:maxBody]
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
		sb.WriteString(fmt.Sprintf("\n... (truncated at %d bytes)", maxBody))
	}
	return tools.Result{Content: sb.String(), Truncated: truncated, Metadata: map[string]any{"kind": "http_response", "method": method, "url": url, "status": resp.StatusCode, "body_bytes": len(body), "truncated": truncated}}
}

func validHTTPMethod(method string) bool {
	switch method {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE":
		return true
	default:
		return false
	}
}
