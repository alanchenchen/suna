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
	for k, v := range params {
		lowerKey := strings.ToLower(k)
		switch val := v.(type) {
		case string:
			if isAuditContentField(lowerKey) {
				clean[k] = auditRedactedSummary(lowerKey, val)
			} else {
				clean[k] = MaskSensitiveContent(val)
			}
		case map[string]any:
			if lowerKey == "env" {
				clean[k] = scrubAuditEnv(val)
			} else {
				clean[k] = scrubAuditParams(val)
			}
		default:
			clean[k] = v
		}
	}
	return clean
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
