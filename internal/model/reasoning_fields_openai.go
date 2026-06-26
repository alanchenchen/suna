package model

import openaioption "github.com/openai/openai-go/v3/option"

func openAIReasoningFieldOptions(fields map[string]any, generated map[string]bool) ([]openaioption.RequestOption, error) {
	if len(fields) == 0 {
		return nil, nil
	}
	if err := validateReasoningFields(fields, generated); err != nil {
		return nil, err
	}
	opts := make([]openaioption.RequestOption, 0, len(fields))
	for _, key := range sortedReasoningFieldKeys(fields) {
		opts = append(opts, openaioption.WithJSONSet(key, fields[key]))
	}
	return opts, nil
}
