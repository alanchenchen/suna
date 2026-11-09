package model

import (
	"strings"
	"testing"
)

func testAdapterSpec(modelID string) AdapterSpec {
	return AdapterSpec{
		ModelID:         modelID,
		BaseURL:         "https://api.example.com/v1",
		APIKey:          "test-api-key",
		ContextWindow:   128000,
		MaxOutputTokens: 8192,
	}
}

func TestMessageTextPreservesCacheAndBlockSemantics(t *testing.T) {
	for _, tt := range []struct {
		name    string
		message Message
		want    string
	}{
		{name: "empty"},
		{name: "cached", message: Message{TextContent: "cached", Content: []ContentBlock{{Type: ContentText, Text: "original"}}}, want: "cached"},
		{name: "single", message: Message{Content: []ContentBlock{{Type: ContentText, Text: "完整正文\nsecond line"}}}, want: "完整正文\nsecond line"},
		{name: "empty text", message: Message{Content: []ContentBlock{{Type: ContentText}}}},
		{name: "image only", message: Message{Content: []ContentBlock{{Type: ContentImage, Text: "not text"}}}},
		{name: "mixed", message: Message{Content: []ContentBlock{{Type: ContentText, Text: "before"}, {Type: ContentImage}, {Type: ContentText, Text: "after"}}}, want: "before\nafter"},
		{name: "empty blocks", message: Message{Content: []ContentBlock{{Type: ContentText}, {Type: ContentText, Text: "body"}, {Type: ContentText}, {Type: ContentText, Text: "tail"}}}, want: "body\n\ntail"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.message.Text(); got != tt.want {
				t.Fatalf("Text() = %q, want %q", got, tt.want)
			}
		})
	}
}

func BenchmarkMessageTextSingleBlock(b *testing.B) {
	message := Message{Content: []ContentBlock{{Type: ContentText, Text: strings.Repeat("source evidence; ", 250000)}}}
	b.ReportAllocs()
	for b.Loop() {
		if message.Text() == "" {
			b.Fatal("missing text")
		}
	}
}
