package config

import (
	"strings"
	"testing"
)

func TestModelRowsActiveModelUsesMarkerWithoutRepeatedActiveText(t *testing.T) {
	m := &Model{Page: "models"}
	rows := m.ModelRows(RowsDeps{
		Tr: func(key string) string {
			if key == "tui.config.activated_status" {
				return "Activated"
			}
			return key
		},
		Models:   []ModelConfig{{Provider: "openai", Model: "gpt-4o", BaseURL: "https://example.test", HasAPIKey: true}},
		IsActive: func(ref string) bool { return ref == "openai/gpt-4o" },
		ModelSummary: func(ModelConfig) string {
			return "openai · gpt-4o"
		},
	})

	if len(rows) == 0 {
		t.Fatalf("ModelRows returned no rows")
	}
	label := rows[0].Label
	value := rows[0].Value
	if !strings.HasPrefix(label, "◉ ") {
		t.Fatalf("active model label = %q, want active marker prefix", label)
	}
	if strings.Contains(label, "Activated") || strings.Contains(value, "Activated") {
		t.Fatalf("active model row = %q / %q, should not repeat active text", label, value)
	}
}
