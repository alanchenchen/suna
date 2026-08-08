package agent

func schemaPropertyKeys(schema map[string]any) map[string]bool {
	keys := make(map[string]bool)
	collectSchemaPropertyKeys(schema, keys)
	if len(keys) == 0 {
		return nil
	}
	return keys
}

func collectSchemaPropertyKeys(schema map[string]any, keys map[string]bool) {
	if props, ok := schema["properties"].(map[string]any); ok {
		for key := range props {
			keys[key] = true
		}
	}
	// 参数清洗必须认识组合 schema 的全部合法分支，不能只读取顶层 properties。
	for _, keyword := range []string{"oneOf", "anyOf"} {
		branches, _ := schema[keyword].([]any)
		for _, branch := range branches {
			if child, ok := branch.(map[string]any); ok {
				collectSchemaPropertyKeys(child, keys)
			}
		}
	}
}
