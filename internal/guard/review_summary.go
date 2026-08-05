package guard

import (
	"encoding/json"
	"sort"
	"unicode/utf8"
)

const reviewParamsLimit = 6000

// marshalReviewParams 保留脱敏后的完整参数结构，使 reviewer 能判断实际目标和影响范围。
// 仅当安全参数本身超过预算时才降级为合法 JSON 的明确摘要，绝不截断 JSON 文本。
func marshalReviewParams(params map[string]any) (string, bool) {
	clean := scrubAuditParams(params)
	encoded, err := json.Marshal(clean)
	if err != nil {
		return `{"summary_truncated":true,"reason":"parameter summary could not be encoded"}`, true
	}
	if utf8.RuneCountInString(string(encoded)) <= reviewParamsLimit {
		return string(encoded), false
	}
	return marshalReviewParamFallback(clean), true
}

// marshalReviewParamFallback 在超预算时保留决定目标、范围和影响的字段。
// 正文和低优先级字段会被省略，但 reviewer 仍可判断调用是否为正常的任务续作。
func marshalReviewParamFallback(params map[string]any) string {
	const fallbackStringLimit = 1200
	priorityKeys := []string{
		"action", "path", "destination", "command", "url", "method", "cwd", "shell",
		"recursive", "overwrite", "force", "parents", "create_dirs", "mode", "timeout",
		"allow_missing", "expected_kind", "max_body_bytes",
	}
	result := map[string]any{
		"summary_truncated": true,
		"reason":            "sanitized parameters exceed review budget",
	}
	included := make(map[string]bool, len(priorityKeys))
	for _, key := range priorityKeys {
		value, ok := params[key]
		if !ok {
			continue
		}
		included[key] = true
		result[key] = boundedReviewValue(value, fallbackStringLimit)
	}
	keys := make([]string, 0, len(params))
	for key := range params {
		if !included[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		result["omitted_keys"] = keys
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return `{"summary_truncated":true,"reason":"parameter summary could not be encoded"}`
	}
	return string(encoded)
}

func boundedReviewValue(value any, stringLimit int) any {
	text, ok := value.(string)
	if !ok || utf8.RuneCountInString(text) <= stringLimit {
		return value
	}
	chars := []rune(text)
	return string(chars[:stringLimit]) + "...[omitted]"
}

// SafeTarget 返回适合 Smart Review 的目标标识。参数本身已通过 marshalReviewParams 脱敏，
// target 仍应保留完整的风险语义，避免 reviewer 因丢失目标或范围而无意义确认。
func SafeTarget(tool string, params map[string]any) string {
	switch tool {
	case "exec":
		target, _ := params["command"].(string)
		return MaskSensitiveContent(target)
	case "writefile", "editfile", "readfile", "listdir", "search":
		target, _ := params["path"].(string)
		return MaskSensitiveContent(target)
	case "filesystem":
		action, _ := params["action"].(string)
		path, _ := params["path"].(string)
		destination, _ := params["destination"].(string)
		if destination != "" {
			return MaskSensitiveContent(action + " " + path + " -> " + destination)
		}
		return MaskSensitiveContent(action + " " + path)
	case "http":
		method, _ := params["method"].(string)
		if method == "" {
			method = "GET"
		}
		target, _ := params["url"].(string)
		return MaskSensitiveContent(method + " " + target)
	default:
		return ""
	}
}

// SafeOperationSummary 返回用于任务回执的安全参数摘要。
func SafeOperationSummary(tool string, params map[string]any) string {
	encoded, _ := marshalReviewParams(params)
	return encoded
}
