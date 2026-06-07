package builtin

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"github.com/alanchenchen/suna/internal/tools"
	"io"
	"net/http"
	"strings"
	"time"
)

type WriteHTTP struct{}

func (WriteHTTP) Spec() tools.Spec {
	return builtinSpec("writehttp", "Send an HTTP POST, PUT, DELETE, or PATCH request.", tools.Act, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"method":  map[string]any{"type": "string", "description": "HTTP method", "enum": []string{"POST", "PUT", "DELETE", "PATCH"}},
			"url":     map[string]any{"type": "string", "description": "Request URL"},
			"headers": map[string]any{"type": "object", "description": "Request headers"},
			"body":    map[string]any{"type": "string", "description": "Request body"},
			"timeout": map[string]any{"type": "integer", "description": "Timeout in seconds, default 30"},
		},
		"required": []string{"method", "url"},
	})
}

func (WriteHTTP) Execute(ctx context.Context, params map[string]any) tools.Result {
	method, _ := params["method"].(string)
	url, _ := params["url"].(string)
	if method == "" {
		return tools.ErrorResult("method is required")
	}
	if url == "" {
		return tools.ErrorResult("url is required")
	}
	method = strings.ToUpper(method)

	timeout := 30 * time.Second
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

	body, _ := params["body"].(string)
	var bodyReader io.Reader
	if body != "" {
		bodyReader = bytes.NewReader([]byte(body))
	}

	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
		},
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

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxHTTPBodySize+1))
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("read body: %s", err))
	}

	truncated := len(respBody) > maxHTTPBodySize
	if truncated {
		respBody = respBody[:maxHTTPBodySize]
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Status: %d %s\n", resp.StatusCode, resp.Status))
	sb.WriteString(fmt.Sprintf("Body:\n%s", string(respBody)))
	if truncated {
		sb.WriteString("\n... (truncated at 100KB)")
	}

	return tools.Result{Content: sb.String(), Truncated: truncated}
}
