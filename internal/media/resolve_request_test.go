package media_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/media"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/tools"
)

func TestDeletedImagesAreOmittedBeforeSendingEachProtocol(t *testing.T) {
	for _, protocol := range []config.ModelProtocol{config.ModelProtocolOpenAIChat, config.ModelProtocolOpenAIResponses, config.ModelProtocolAnthropic} {
		t.Run(string(protocol), func(t *testing.T) {
			root := t.TempDir()
			if resolved, err := filepath.EvalSymlinks(root); err == nil {
				root = resolved
			}
			path := filepath.Join(root, "temporary.png")
			if err := os.WriteFile(path, []byte("temporary image"), 0600); err != nil {
				t.Fatal(err)
			}
			stable := filepath.Join(root, "stable.png")
			if err := os.WriteFile(stable, []byte("stable image"), 0600); err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			var bodies []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				mu.Lock()
				bodies = append(bodies, string(body))
				mu.Unlock()
				w.Header().Set("Content-Type", "text/event-stream")
				switch protocol {
				case config.ModelProtocolOpenAIChat:
					fmt.Fprint(w, "data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				case config.ModelProtocolOpenAIResponses:
					fmt.Fprint(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"test\",\"status\":\"completed\",\"output\":[]}}\n\n")
				case config.ModelProtocolAnthropic:
					fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"test\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"example-model\",\"stop_reason\":null,\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
				}
			}))
			defer server.Close()
			router, err := model.NewRouter(&config.Config{Models: []config.ModelConfig{{Provider: "example-provider", Model: "example-model", Protocol: protocol, BaseURL: server.URL, APIKey: "test-api-key", ContextWindow: 100000, MaxOutputTokens: 1000}}}, media.NewContextResolver(filepath.Join(root, "unused")))
			if err != nil {
				t.Fatal(err)
			}
			binding, err := router.Bind("example-provider/example-model")
			if err != nil {
				t.Fatal(err)
			}
			ref := model.MediaRef{Kind: model.MediaAttachment, Path: path, Name: "temporary.png", MimeType: "image/png"}
			image := model.ContentBlock{Type: model.ContentImage, Media: &ref}
			stableRef := model.MediaRef{Kind: model.MediaPath, Path: stable, Name: "stable.png", MimeType: "image/png"}
			req := model.CompletionRequest{Model: "example-model", MaxTokens: 1000, SessionState: "old facts", Messages: []model.Message{
				model.NewTextMessage(model.RoleUser, "inspect and finish"),
				{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "read", Name: "read_image", Arguments: `{}`}}},
				{Role: model.RoleTool, ToolCallID: "read", Content: []model.ContentBlock{{Type: model.ContentText, Text: "image loaded; source retained"}}},
				{Role: model.RoleUser, Content: []model.ContentBlock{image}},
				{Role: model.RoleUser, Content: []model.ContentBlock{{Type: model.ContentText, Text: "keep surrounding prose"}, image, {Type: model.ContentImage, Media: &stableRef}}},
			}}
			original, err := json.Marshal(req)
			if err != nil {
				t.Fatal(err)
			}
			ctx := tools.WithExecutionContext(context.Background(), tools.ExecutionContext{AttachmentDir: root})
			for attempt := 0; attempt < 3; attempt++ {
				if attempt == 1 {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				}
				if attempt == 2 {
					if err := os.WriteFile(path, []byte("restored image"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				stream, err := binding.Complete(ctx, req)
				if err != nil {
					t.Fatalf("request %d error = %v", attempt, err)
				}
				for chunk := range stream {
					if chunk.Error != nil {
						t.Fatal(chunk.Error)
					}
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if len(bodies) != 3 {
				t.Fatalf("HTTP requests = %d, want 3", len(bodies))
			}
			for i, body := range bodies {
				for _, text := range []string{"old facts", "inspect and finish", "keep surrounding prose", "read_image", "image loaded; source retained", base64.StdEncoding.EncodeToString([]byte("stable image"))} {
					if !strings.Contains(body, text) {
						t.Fatalf("request %d missing %q", i, text)
					}
				}
				if i == 1 {
					if strings.Contains(body, base64.StdEncoding.EncodeToString([]byte("temporary image"))) || strings.Contains(body, `"content":[]`) {
						t.Fatal("deleted image or empty image message reached model service")
					}
				} else {
					text := "temporary image"
					if i == 2 {
						text = "restored image"
					}
					if !strings.Contains(body, base64.StdEncoding.EncodeToString([]byte(text))) {
						t.Fatalf("request %d missing live image", i)
					}
				}
			}
			after, _ := json.Marshal(req)
			if !reflect.DeepEqual(original, after) {
				t.Fatal("request filtering changed original history")
			}
		})
	}
}
