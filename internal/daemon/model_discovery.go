package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/alanchenchen/suna/internal/config"
)

const modelDiscoveryErrorBodyLimit = 512

var knownModelDiscoveryCompatSuffixes = []string{
	"/api/claudecode",
	"/api/anthropic",
	"/apps/anthropic",
	"/api/coding",
	"/claudecode",
	"/anthropic",
	"/step_plan",
	"/coding",
	"/claude",
}

func discoverConfiguredModels(ctx context.Context, cfg *config.Config, modelRef string) ([]string, string) {
	if cfg == nil {
		return nil, "配置尚未加载。"
	}
	mc, ok := cfg.ModelByRef(strings.TrimSpace(modelRef))
	if !ok {
		return nil, "模型配置不存在。"
	}
	key, err := mc.ResolveAPIKey()
	if err != nil {
		return nil, "该模型还没有保存 API Key。"
	}
	endpoints, err := modelDiscoveryEndpoints(mc.BaseURL)
	if err != nil {
		return nil, "Endpoint 必须是有效的 http(s) URL。"
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var lastErr error
	for _, endpoint := range endpoints {
		models, err := fetchDiscoveredModels(ctx, endpoint, key, mc.ProtocolOrDefault(), mc.AuthMode)
		if err == nil {
			return models, ""
		}
		lastErr = err
		if !errors.Is(err, errModelDiscoveryEndpointNotFound) {
			break
		}
	}
	message := "无法从模型服务拉取列表。已尝试：" + strings.Join(endpoints, "、") + "。"
	if lastErr != nil {
		message += " 最后错误：" + lastErr.Error()
	}
	return nil, message
}

func modelDiscoveryEndpoints(raw string) ([]string, error) {
	base, err := cleanModelDiscoveryURL(raw)
	if err != nil {
		return nil, err
	}
	parsed, _ := url.Parse(base)
	path := strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(path, "/models") {
		parsed.Path = path
		return []string{parsed.String()}, nil
	}

	var endpoints []string
	if endsWithModelDiscoveryVersionSegment(path) {
		endpoints = append(endpoints, withModelDiscoveryPath(parsed, path+"/models"))
		if !strings.HasSuffix(path, "/v1") {
			endpoints = append(endpoints, withModelDiscoveryPath(parsed, path+"/v1/models"))
		}
	} else {
		nextPath := "/v1/models"
		if path != "" {
			nextPath = path + "/v1/models"
		}
		endpoints = append(endpoints, withModelDiscoveryPath(parsed, nextPath))
	}
	if stripped, ok := stripModelDiscoveryCompatSuffix(path); ok {
		root := strings.TrimRight(stripped, "/")
		endpoints = append(endpoints, withModelDiscoveryPath(parsed, root+"/v1/models"))
		endpoints = append(endpoints, withModelDiscoveryPath(parsed, root+"/models"))
	}
	return uniqueModelDiscoveryStrings(endpoints), nil
}

func cleanModelDiscoveryURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil {
		return "", errors.New("invalid model endpoint")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("invalid model endpoint")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func withModelDiscoveryPath(parsed *url.URL, path string) string {
	next := *parsed
	next.Path = path
	return next.String()
}

func endsWithModelDiscoveryVersionSegment(path string) bool {
	last := path
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		last = path[idx+1:]
	}
	digits := strings.TrimPrefix(last, "v")
	return last != digits && digits != "" && strings.IndexFunc(digits, func(r rune) bool {
		return r < '0' || r > '9'
	}) == -1
}

func stripModelDiscoveryCompatSuffix(path string) (string, bool) {
	for _, suffix := range knownModelDiscoveryCompatSuffixes {
		if strings.HasSuffix(path, suffix) {
			return strings.TrimSuffix(path, suffix), true
		}
	}
	return "", false
}

func uniqueModelDiscoveryStrings(values []string) []string {
	unique := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			unique = append(unique, value)
		}
	}
	return unique
}

var errModelDiscoveryEndpointNotFound = errors.New("models endpoint not found")

func fetchDiscoveredModels(ctx context.Context, endpoint, apiKey string, protocol config.ModelProtocol, authMode config.AuthMode) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if protocol == config.ModelProtocolAnthropic && authMode != config.AuthModeBearer {
		req.Header.Set("X-Api-Key", apiKey)
		req.Header.Set("Anthropic-Version", "2023-06-01")
		if authMode == config.AuthModeBoth {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
	} else {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := modelDiscoveryHTTPError(resp.StatusCode, resp.Body, apiKey)
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			return nil, errors.Join(errModelDiscoveryEndpointNotFound, errors.New(message))
		}
		return nil, errors.New(message)
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var models []string
	for _, item := range payload.Data {
		id := strings.TrimSpace(item.ID)
		if id != "" && !seen[id] {
			seen[id] = true
			models = append(models, id)
		}
	}
	for _, item := range payload.Models {
		name := strings.TrimSpace(item.Name)
		if name != "" && !seen[name] {
			seen[name] = true
			models = append(models, name)
		}
	}
	if len(models) == 0 {
		return nil, errors.New("empty model list")
	}
	sort.Strings(models)
	return models, nil
}

func modelDiscoveryHTTPError(status int, body io.Reader, apiKey string) string {
	raw, _ := io.ReadAll(io.LimitReader(body, modelDiscoveryErrorBodyLimit+1))
	text := strings.TrimSpace(string(raw))
	if key := strings.TrimSpace(apiKey); key != "" {
		text = strings.ReplaceAll(text, key, "[REDACTED]")
		text = strings.ReplaceAll(text, "Bearer "+key, "Bearer [REDACTED]")
	}
	if len(text) > modelDiscoveryErrorBodyLimit {
		text = text[:modelDiscoveryErrorBodyLimit] + "..."
	}
	if text == "" {
		return "HTTP " + http.StatusText(status)
	}
	return "HTTP " + http.StatusText(status) + ": " + text
}
