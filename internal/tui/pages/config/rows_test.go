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
	mc := ModelConfig{Provider: "DF", Model: "MiniMax-M3", ContextWindow: 1000000, MaxOutputTokens: 8192, SubtaskFor: []string{"Froghire/**"}, HasAPIKey: true}
	got := ModelSummary(mc, false, func(int) string { return "x" })
	if strings.Contains(got, "Froghire") || strings.Contains(got, "subtask") {
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
		Models:           []ModelConfig{{Provider: "DF", Model: "MiniMax-M3", ContextWindow: 1000000, MaxOutputTokens: 8192, SubtaskFor: []string{"Froghire/**", "Oio/**"}}},
		DisplayEndpoint:  func(string) string { return "" },
		ContextDisplay:   func(ModelConfig) string { return "" },
		MaxOutputDisplay: func(ModelConfig) string { return "" },
		ReasoningDisplay: func(ModelConfig) string { return "" },
	})
	for _, row := range rows {
		if row.Label == "Subtask for" {
			if row.Value != "Froghire/**, Oio/**" {
				t.Fatalf("Subtask for row value = %q", row.Value)
			}
			return
		}
	}
	t.Fatalf("Subtask for row not found in %#v", rows)
}
