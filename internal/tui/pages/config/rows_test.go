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
		Models:   []ModelConfig{{Provider: "openai", Model: "gpt-4o", BaseURL: "https://example.test", ContextWindow: 128000, MaxOutputTokens: 8192, HasAPIKey: true}},
		IsActive: func(ref string) bool { return ref == "openai/gpt-4o" },
		ModelSummary: func(ModelConfig) string {
			return "openai · gpt-4o"
		},
	})

	var modelRow Row
	for _, row := range rows {
		if row.Kind == "model" {
			modelRow = row
			break
		}
	}
	if modelRow.Kind == "" {
		t.Fatalf("ModelRows returned no model row: %#v", rows)
	}
	label := modelRow.Label
	value := modelRow.Value
	if !strings.HasPrefix(label, "◉ ") {
		t.Fatalf("active model label = %q, want active marker prefix", label)
	}
	if strings.Contains(label, "Activated") || strings.Contains(value, "Activated") {
		t.Fatalf("active model row = %q / %q, should not repeat active text", label, value)
	}
}

func TestModelSummaryKeepsCapabilitiesBriefAndPrioritizesStrengths(t *testing.T) {
	mc := ModelConfig{
		Provider:        "DF",
		Model:           "MiniMax-M3",
		BaseURL:         "https://example.test",
		ContextWindow:   1000000,
		MaxOutputTokens: 128000,
		Strengths:       []string{"多模态", "1M长上下文", "快速代码辅助"},
		HasAPIKey:       true,
	}

	got := ModelSummary(mc, true, func(n int) string {
		switch n {
		case 1000000:
			return "1.0M"
		case 128000:
			return "128.0k"
		default:
			return "?"
		}
	})
	want := "ctx 1.0M · out 128.0k · 多模态, 1M长上下文, 快速代码辅助"
	if got != want {
		t.Fatalf("ModelSummary() = %q, want %q", got, want)
	}
	for _, unexpected := range []string{"DF", "MiniMax-M3", "endpoint_configured", "active"} {
		if strings.Contains(got, unexpected) {
			t.Fatalf("ModelSummary() = %q, should not contain %q", got, unexpected)
		}
	}
}

func TestModelSummaryOmitsSubtaskFor(t *testing.T) {
	mc := ModelConfig{Provider: "DF", Model: "MiniMax-M3", ContextWindow: 1000000, MaxOutputTokens: 8192, SubtaskFor: []string{"openai/**"}, HasAPIKey: true}
	got := ModelSummary(mc, false, func(int) string { return "x" })
	if strings.Contains(got, "openai") || strings.Contains(got, "subtask") {
		t.Fatalf("ModelSummary() = %q, should omit subtask_for", got)
	}
}

func TestDetailRowsShowsSubtaskFor(t *testing.T) {
	m := &Model{Page: "detail", DetailRef: "DF/MiniMax-M3"}
	rows := m.DetailRows(RowsDeps{
		Tr: func(key string) string {
			switch key {
			case "tui.config.provider.subtask_for":
				return "Subtask for"
			case "tui.config.subtask_for_all":
				return "all main models"
			default:
				return key
			}
		},
		Models:           []ModelConfig{{Provider: "DF", Model: "MiniMax-M3", ContextWindow: 1000000, MaxOutputTokens: 8192, SubtaskFor: []string{"openai/**", "anthropic/**"}}},
		DisplayEndpoint:  func(string) string { return "" },
		ContextDisplay:   func(ModelConfig) string { return "" },
		MaxOutputDisplay: func(ModelConfig) string { return "" },
		ReasoningDisplay: func(ModelConfig) string { return "" },
	})
	for _, row := range rows {
		if row.Label == "Subtask for" {
			if row.Value != "openai/**, anthropic/**" {
				t.Fatalf("Subtask for row value = %q", row.Value)
			}
			return
		}
	}
	t.Fatalf("Subtask for row not found in %#v", rows)
}

func TestModelRowsGroupsModelsByProvider(t *testing.T) {
	m := &Model{Page: "models"}
	rows := m.ModelRows(RowsDeps{
		Tr: func(key string) string { return key },
		Models: []ModelConfig{
			{Provider: "Oio", Model: "claude", BaseURL: "https://oio.example", ContextWindow: 1000, MaxOutputTokens: 100, HasAPIKey: true},
			{Provider: "DF", Model: "glm", BaseURL: "https://df.example", ContextWindow: 1000, MaxOutputTokens: 100, HasAPIKey: true},
		},
		IsActive:     func(string) bool { return false },
		ModelSummary: func(ModelConfig) string { return "summary" },
	})

	var headers, providerAdds, finalAdds int
	for _, row := range rows {
		switch row.Kind {
		case "provider_header":
			headers++
		case "provider_add_model":
			providerAdds++
		case "add_provider_model":
			finalAdds++
		}
	}
	if headers != 2 || providerAdds != 2 || finalAdds != 1 {
		t.Fatalf("group rows headers=%d providerAdds=%d finalAdds=%d rows=%#v", headers, providerAdds, finalAdds, rows)
	}
}
