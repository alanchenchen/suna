package tool

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type WriteHTTP struct{}

func (WriteHTTP) Name() string { return "writehttp" }
func (WriteHTTP) Description() string {
	return "发送 HTTP POST/PUT/DELETE/PATCH 请求。"
}
func (WriteHTTP) Category() Category { return Act }
func (WriteHTTP) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"method":  map[string]any{"type": "string", "description": "HTTP 方法", "enum": []string{"POST", "PUT", "DELETE", "PATCH"}},
			"url":     map[string]any{"type": "string", "description": "请求 URL"},
			"headers": map[string]any{"type": "object", "description": "请求头"},
			"body":    map[string]any{"type": "string", "description": "请求体"},
			"timeout": map[string]any{"type": "integer", "description": "超时秒数（默认30）"},
		},
		"required": []string{"method", "url"},
	}
}

func (WriteHTTP) Execute(ctx context.Context, params map[string]any) Result {
	method, _ := params["method"].(string)
	url, _ := params["url"].(string)
	if method == "" {
		return ErrorResult("method is required")
	}
	if url == "" {
		return ErrorResult("url is required")
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
		return ErrorResult(fmt.Sprintf("create request: %s", err))
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "Suna/1.0")
	}

	resp, err := client.Do(req)
	if err != nil {
		return ErrorResult(fmt.Sprintf("request failed: %s", err))
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxHTTPBodySize+1))
	if err != nil {
		return ErrorResult(fmt.Sprintf("read body: %s", err))
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

	return Result{Content: sb.String(), Truncated: truncated}
}
