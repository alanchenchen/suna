package model

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"reflect"
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

// 适配器测试只模拟解析契约；文件是否存在、权限和校验归媒体包验证。
type adapterMediaResolver struct {
	result ResolvedMedia
	err    error
	calls  int
}

func (r *adapterMediaResolver) Resolve(_ context.Context, ref MediaRef, _ ResolveMode) (ResolvedMedia, error) {
	r.calls++
	if ref.Path == "live.png" {
		return ResolvedMedia{URL: "https://example.com/live.png"}, nil
	}
	return r.result, r.err
}

func adapterMediaRequest() CompletionRequest {
	missing := ContentBlock{Type: ContentImage, Media: &MediaRef{Kind: MediaPath, Path: "missing.png"}}
	live := ContentBlock{Type: ContentImage, Media: &MediaRef{Kind: MediaPath, Path: "live.png"}}
	return CompletionRequest{SessionState: "old facts", Messages: []Message{
		NewTextMessage(RoleUser, "original request"),
		{Role: RoleUser, Content: []ContentBlock{missing}},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call", Name: "read_image", Arguments: `{}`}}},
		{Role: RoleTool, ToolCallID: "call", Content: []ContentBlock{{Type: ContentText, Text: "original tool evidence"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "prefix"}, missing, live, {Type: ContentText, Text: "suffix"}}},
		{Role: RoleUser, TextContent: "cached text", Content: []ContentBlock{missing}},
	}}
}

func checkAdapterMediaInput(t *testing.T, build func(MediaResolver, *CompletionRequest) ([]byte, error)) {
	t.Helper()
	for _, tt := range []struct {
		name   string
		result ResolvedMedia
		err    error
	}{
		{name: "missing", result: ResolvedMedia{Missing: true}},
		{name: "permission", err: fs.ErrPermission},
		{name: "cancelled", err: context.Canceled},
		{name: "deadline", err: context.DeadlineExceeded},
		{name: "missing result with error", result: ResolvedMedia{Missing: true}, err: fs.ErrPermission},
		{name: "ordinary protocol error", err: &ModelError{Kind: ModelErrorHTTP, StatusCode: 400}},
		{name: "empty result"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &adapterMediaResolver{result: tt.result, err: tt.err}
			req := adapterMediaRequest()
			before, err := json.Marshal(req)
			if err != nil {
				t.Fatal(err)
			}
			wire, err := build(resolver, &req)
			if tt.err != nil || !tt.result.Missing {
				if err == nil || resolver.calls != 1 || (tt.err != nil && !errors.Is(err, tt.err)) {
					t.Fatalf("error=%v calls=%d, want original failure and one resolve", err, resolver.calls)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				for _, value := range []string{"old facts", "original request", "call", "read_image", "original tool evidence", "prefix", "suffix", "cached text", "https://example.com/live.png"} {
					if !strings.Contains(string(wire), value) {
						t.Fatalf("wire missing %q", value)
					}
				}
				if strings.Contains(string(wire), `"content":[]`) || strings.Contains(string(wire), "missing.png") {
					t.Fatal("missing image or empty message remains in protocol input")
				}
			}
			after, err := json.Marshal(req)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("protocol mapping modified source request")
			}
		})
	}
}

func TestOpenAIChatBuildMessagesMapsMissingMedia(t *testing.T) {
	checkAdapterMediaInput(t, func(resolver MediaResolver, req *CompletionRequest) ([]byte, error) {
		adapter := NewOpenAIChatAdapter(testAdapterSpec("example-model"), AdapterDependencies{MediaResolver: resolver})
		messages, err := adapter.buildMessages(context.Background(), req)
		if err != nil {
			return nil, err
		}
		return json.Marshal(messages)
	})
}
