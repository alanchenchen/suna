package model

import "net/http"

type compatibleHeaderRoundTripper struct{ next http.RoundTripper }

func (t compatibleHeaderRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	normalizeCompatibleHeaders(req)
	return t.next.RoundTrip(req)
}

func normalizeCompatibleHeaders(req *http.Request) {
	// 一些 OpenAI/Anthropic compatible 中转网关会在业务层之前拦截 SDK 识别头，
	// 导致请求变成 Cloudflare/网关 502，且供应商控制台看不到记录；这里仅清理
	// Stainless SDK 追踪头和 User-Agent，不移除 Authorization、Content-Type 或协议必需头。
	req.Header.Set("User-Agent", "Suna/1.0")
	req.Header.Set("Accept", "*/*")
	for _, key := range []string{
		"X-Stainless-Lang",
		"X-Stainless-Package-Version",
		"X-Stainless-OS",
		"X-Stainless-Arch",
		"X-Stainless-Runtime",
		"X-Stainless-Runtime-Version",
		"X-Stainless-Retry-Count",
		"X-Stainless-Timeout",
	} {
		req.Header.Del(key)
	}
}

func compatibleHTTPClient(transport http.RoundTripper) *http.Client {
	return &http.Client{Transport: compatibleHeaderRoundTripper{next: transport}}
}
