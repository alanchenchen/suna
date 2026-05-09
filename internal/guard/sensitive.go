package guard

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// 敏感文件路径匹配规则。
// 如果文件名或路径匹配这些规则，readfile 直接拦截，不返回内容。
var sensitiveFilePatterns = []struct {
	pattern string
	reason  string
}{
	{".credentials", "credential file"},
	{".env", "environment file with secrets"},
	{".pem", "PEM private key"},
	{".key", "private key file"},
	{".p12", "PKCS12 certificate"},
	{".pfx", "PKCS12 certificate"},
	{".jks", "Java keystore"},
	{"id_rsa", "SSH private key"},
	{"id_ed25519", "SSH private key"},
	{"id_ecdsa", "SSH private key"},
	{".ssh/", "SSH directory"},
	{".gnupg/", "GPG directory"},
	{".netrc", "netrc with credentials"},
	{".npmrc", "may contain auth tokens"},
	{".pypirc", "may contain PyPI credentials"},
	{".aws/credentials", "AWS credentials"},
	{".aws/config", "AWS config with secrets"},
	{"credentials.json", "service account credentials"},
	{"service-account", "service account key"},
	{".docker/config.json", "Docker credentials"},
	{".kube/config", "Kubernetes config with tokens"},
	{".suna/credentials.toml", "Suna API credentials"},
	{"credentials.toml", "Suna stored keys"},
}

// IsSensitivePath 检查文件路径是否指向敏感文件。
// 返回 (是否敏感, 拦截原因)。
func IsSensitivePath(path string) (bool, string) {
	expanded := expandPathForCheck(path)
	lower := strings.ToLower(expanded)

	for _, rule := range sensitiveFilePatterns {
		if strings.Contains(lower, strings.ToLower(rule.pattern)) {
			homeDir, _ := os.UserHomeDir()
			// ~/.suna/config.toml 不是敏感文件，允许读取
			if strings.HasPrefix(lower, strings.ToLower(filepath.Join(homeDir, ".suna"))) {
				if strings.HasSuffix(lower, "config.toml") || strings.HasSuffix(lower, "memory.db") {
					continue
				}
			}
			return true, rule.reason
		}
	}

	return false, ""
}

// 敏感内容正则：匹配 API key、token、密码等模式
var sensitivePatterns = []struct {
	re    *regexp.Regexp
	group int
	label string
}{
	// API Key 模式：常见前缀
	{regexp.MustCompile(`(?i)(sk-|sk_live_|sk_test_|key_)[a-zA-Z0-9_\-]{20,}`), 0, "api_key"},
	// Anthropic key
	{regexp.MustCompile(`(?i)sk-ant-api[a-zA-Z0-9_\-]{20,}`), 0, "anthropic_key"},
	// Bearer token
	{regexp.MustCompile(`(?i)Bearer\s+[a-zA-Z0-9_\-\.]{20,}`), 0, "bearer_token"},
	// Generic secret assignment: password=xxx, token=xxx, secret=xxx
	{regexp.MustCompile(`(?i)(password|passwd|secret|token|api_key|apikey|access_key|private_key)\s*[=:]\s*["']?[a-zA-Z0-9_\-\.]{8,}["']?`), 0, "credential"},
	// AWS key pattern
	{regexp.MustCompile(`AKIA[0-9A-Z]{16}`), 0, "aws_access_key"},
	// Generic hex/base64 key (32+ chars after key-like prefix)
	{regexp.MustCompile(`(?i)(key|token|secret|password)\s*[=:]\s*["']([a-zA-Z0-9+/=_\-]{32,})["']`), 2, "long_secret"},
	// Private key block: 完整的 BEGIN...END 块
	{regexp.MustCompile(`(?s)-----BEGIN (?:RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----[\s\S]*?-----END (?:RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----`), 0, "private_key_block"},
	// Private key: 只有 BEGIN 行（截断的情况）
	{regexp.MustCompile(`-----BEGIN (?:RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----[^\n]*(?:\n[^\n]*){0,5}`), 0, "private_key_block"},
	// URL with embedded credentials: https://user:pass@host
	{regexp.MustCompile(`(?i)(https?://[^:\s]+):([^@\s]+)@`), 0, "url_credential"},
}

// MaskSensitiveContent 对文本内容进行脱敏处理。
// 将匹配到的 API key、密码、token 等替换为 ***REDACTED***。
func MaskSensitiveContent(content string) string {
	result := content

	for _, sp := range sensitivePatterns {
		result = sp.re.ReplaceAllStringFunc(result, func(match string) string {
			if sp.group == 0 {
				return "***REDACTED_" + strings.ToUpper(sp.label) + "***"
			}
			// 只替换捕获组部分
			submatches := sp.re.FindStringSubmatch(match)
			if len(submatches) > sp.group {
				return strings.Replace(match, submatches[sp.group], "***REDACTED_"+strings.ToUpper(sp.label)+"***", 1)
			}
			return match
		})
	}

	return result
}

func expandPathForCheck(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}
