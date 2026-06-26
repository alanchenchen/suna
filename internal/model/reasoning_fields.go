package model

import (
	"fmt"
	"sort"
)

// mergeReasoningFields 将配置里的 models.reasoning 平铺进最终请求体。
// models.reasoning 是 Suna 对各协议“思考/推理强度相关参数”的统一抽象入口。
// 公共层不理解任何模型 preset，只保护 core 已经生成的字段不被覆盖。
func mergeReasoningFields(body map[string]any, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	for _, key := range sortedReasoningFieldKeys(fields) {
		value := fields[key]
		if _, exists := body[key]; exists {
			return reasoningFieldConflictError(key)
		}
		body[key] = value
	}
	return nil
}

func validateReasoningFields(fields map[string]any, generated map[string]bool) error {
	for _, key := range sortedReasoningFieldKeys(fields) {
		if generated[key] {
			return reasoningFieldConflictError(key)
		}
	}
	return nil
}

func reasoningFieldConflictError(key string) error {
	return fmt.Errorf("reasoning field %q conflicts with generated request body", key)
}

func sortedReasoningFieldKeys(fields map[string]any) []string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
