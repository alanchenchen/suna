package model

import (
	"strings"
	"testing"
)

func TestLLMDebugEnabled(t *testing.T) {
	t.Setenv("SUNA_LLM_DEBUG", "")
	if llmDebugEnabled() {
		t.Fatal("llmDebugEnabled() = true, want false")
	}

	t.Setenv("SUNA_LLM_DEBUG", "true")
	if !llmDebugEnabled() {
		t.Fatal("llmDebugEnabled() = false, want true")
	}
}

func TestLLMDebugMessagesSummarizesRequestSafely(t *testing.T) {
	longText := strings.Repeat("x", llmDebugTextLimit+1)
	messages := llmDebugMessages([]Message{
		{
			Role:        RoleUser,
			TextContent: longText,
			Content: []ContentBlock{
				{Type: ContentText, Text: longText},
				{Type: ContentImage, Media: &MediaRef{Kind: MediaAttachment, Name: "shot.png", MimeType: "image/png", Size: 123}},
			},
			ToolCalls: []ToolCall{{ID: "call-1", Name: "readfile", Arguments: longText}},
		},
	})

	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	msg := messages[0]
	if !msg.Truncated || len(msg.Text) <= llmDebugTextLimit {
		t.Fatalf("message text was not marked/truncated: truncated=%v len=%d", msg.Truncated, len(msg.Text))
	}
	if len(msg.Content) != 2 {
		t.Fatalf("len(content) = %d, want 2", len(msg.Content))
	}
	if msg.Content[1].MediaName != "shot.png" || msg.Content[1].MediaMime != "image/png" || msg.Content[1].MediaSize != 123 {
		t.Fatalf("media summary = %#v, want metadata only", msg.Content[1])
	}
	if len(msg.ToolCalls) != 1 || !msg.ToolCalls[0].ArgsTruncated {
		t.Fatalf("tool call summary = %#v, want truncated args", msg.ToolCalls)
	}
}

func TestTruncateLLMDebugTextKeepsUTF8(t *testing.T) {
	got, truncated := truncateLLMDebugText("你好世界", 5)
	if !truncated {
		t.Fatal("truncated = false, want true")
	}
	if !strings.HasPrefix(got, "你") || strings.ContainsRune(got, '\uFFFD') {
		t.Fatalf("truncateLLMDebugText() = %q, want valid UTF-8 prefix", got)
	}
}
