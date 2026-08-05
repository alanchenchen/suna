package guard

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

func marshalParams(params map[string]any) (string, error) {
	clean := scrubAuditParams(params)
	b, err := json.Marshal(clean)
	if err != nil {
		return "{}", err
	}
	return string(b), nil
}

func scrubAuditParams(params map[string]any) map[string]any {
	if params == nil {
		return map[string]any{}
	}
	clean := make(map[string]any, len(params))
	for key, value := range params {
		clean[key] = scrubAuditValue(strings.ToLower(key), value)
	}
	return clean
}

// scrubAuditValue 递归处理嵌套参数，确保 editfile 等数组中的正文也不会进入审计或 Review。
// 除正文、环境变量值和可识别凭据外，参数结构和值应尽量保留，供 LLM 判断真实影响范围。
func scrubAuditValue(key string, value any) any {
	switch typed := value.(type) {
	case string:
		if isAuditContentField(key) || isSensitiveAuditKey(key) {
			return auditRedactedSummary(key, typed)
		}
		return MaskSensitiveContent(typed)
	case map[string]any:
		if key == "env" {
			return scrubAuditEnv(typed)
		}
		return scrubAuditParams(typed)
	case []any:
		clean := make([]any, len(typed))
		for index, item := range typed {
			clean[index] = scrubAuditValue(key, item)
		}
		return clean
	case []string:
		clean := make([]string, len(typed))
		for index, item := range typed {
			value, _ := scrubAuditValue(key, item).(string)
			clean[index] = value
		}
		return clean
	default:
		return value
	}
}

func isSensitiveAuditKey(key string) bool {
	for _, marker := range []string{"authorization", "cookie", "token", "secret", "password", "api_key", "apikey", "credential"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

func isAuditContentField(key string) bool {
	switch key {
	case "content", "body", "old_string", "new_string", "system":
		return true
	default:
		return false
	}
}

func auditRedactedSummary(key string, value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("***REDACTED_%s len=%d sha256=%x***", strings.ToUpper(key), len(value), sum[:8])
}

func scrubAuditEnv(env map[string]any) map[string]any {
	clean := make(map[string]any, len(env))
	for k, v := range env {
		if s, ok := v.(string); ok {
			clean[k] = auditRedactedSummary("env", s)
		} else {
			clean[k] = "***REDACTED_ENV_VALUE***"
		}
	}
	return clean
}
