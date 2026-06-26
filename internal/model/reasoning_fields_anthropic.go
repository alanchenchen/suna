package model

import anthropicoption "github.com/anthropics/anthropic-sdk-go/option"

func anthropicReasoningFieldOptions(fields map[string]any, generated map[string]bool) ([]anthropicoption.RequestOption, error) {
	if len(fields) == 0 {
		return nil, nil
	}
	if err := validateReasoningFields(fields, generated); err != nil {
		return nil, err
	}
	opts := make([]anthropicoption.RequestOption, 0, len(fields))
	for _, key := range sortedReasoningFieldKeys(fields) {
		opts = append(opts, anthropicoption.WithJSONSet(key, fields[key]))
	}
	return opts, nil
}
