package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/prompt"
)

func consumedMessage(role model.Role, text string) Message {
	m := NewTextMessage(role, text)
	m.Consumed = true
	if role == model.RoleUser {
		m.Origin = OriginUser
	}
	return m
}
func compactSizedText(tokens int) string {
	if tokens == 0 {
		return ""
	}
	return strings.Repeat("x", max(1, (tokens-1)*4))
}
func toolHistory() []Message {
	return []Message{consumedMessage(model.RoleUser, "inspect"), {Role: model.RoleAssistant, TextContent: "original prose", Consumed: true, ToolCalls: []ToolCall{{ID: "call-a", Name: "readfile", Arguments: `{"path":"a"}`}, {ID: "call-b", Name: "exec", Arguments: `{"command":"test"}`}}}, {Role: model.RoleTool, ToolCallID: "call-a", TextContent: strings.Repeat("source", 1200), ToolStatus: "completed", Consumed: true}, {Role: model.RoleTool, ToolCallID: "call-b", TextContent: "exact failure at a:12", ToolStatus: "failed", Consumed: true}, consumedMessage(model.RoleAssistant, "results explained"), consumedMessage(model.RoleUser, strings.Repeat("recent tail ", 1000)), consumedMessage(model.RoleAssistant, "tail reply")}
}
func compactToolBatch(groups, resultTokens, tailTokens int) []Message {
	messages := []Message{consumedMessage(model.RoleUser, "inspect all evidence")}
	for i := 0; i < groups; i++ {
		declaration := consumedMessage(model.RoleAssistant, fmt.Sprintf("original reasoning %d", i))
		declaration.ToolCalls = []ToolCall{{ID: fmt.Sprintf("batch-%d", i), Name: "readfile", Arguments: `{"path":"evidence"}`}}
		result := consumedMessage(model.RoleTool, compactSizedText(resultTokens))
		result.ToolCallID, result.ToolStatus = declaration.ToolCalls[0].ID, "completed"
		messages = append(messages, declaration, result)
	}
	return append(messages, consumedMessage(model.RoleAssistant, "complete explanation"), consumedMessage(model.RoleUser, compactSizedText(tailTokens)), consumedMessage(model.RoleAssistant, "tail reply"))
}
func testCompactOptions(target int) CompactOptions {
	return CompactOptions{Budget: 10000, TargetGain: target, MinimumGain: 1, InputLimit: 1000000, OutputLimit: 32000, Coefficient: 1}
}
func compactRequestTokens(t *testing.T, p *CompactPlan) int {
	t.Helper()
	payload, err := compactPayload(p)
	if err != nil {
		t.Fatal(err)
	}
	loader, err := prompt.New()
	if err != nil {
		t.Fatal(err)
	}
	text, err := loader.Render("compress", map[string]any{"Content": string(payload)})
	if err != nil {
		t.Fatal(err)
	}
	return model.EstimateTokens(text)
}
func compactTestBinding(t *testing.T, url string, output int) *model.ModelBinding {
	t.Helper()
	router, err := model.NewRouter(&config.Config{MaxModelRPS: 100, Models: []config.ModelConfig{{Provider: "custom", Protocol: config.ModelProtocolOpenAIChat, Model: "test", BaseURL: url, APIKey: "test-api-key", ContextWindow: 200000, MaxOutputTokens: output}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := router.Bind("custom/test")
	if err != nil {
		t.Fatal(err)
	}
	return binding
}
func testCompressor(t *testing.T) *Compressor {
	t.Helper()
	loader, err := prompt.New()
	if err != nil {
		t.Fatal(err)
	}
	c := NewCompressor()
	c.SetPrompts(loader)
	return c
}
func sendCompactResponse(w http.ResponseWriter, text, finish string) {
	content, _ := json.Marshal(text)
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprintf(w, "data: {\"id\":\"offline\",\"object\":\"chat.completion.chunk\",\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":%s},\"finish_reason\":%q}],\"usage\":{\"prompt_tokens\":5000,\"completion_tokens\":1600,\"total_tokens\":6600}}\n\ndata: [DONE]\n\n", content, finish)
}

func TestCompactGenerationUsesReasoningBudgetWithoutRetryingTruncation(t *testing.T) {
	for _, tt := range []struct {
		name                   string
		limited, truncateFirst bool
	}{
		{name: "binding output capacity"}, {name: "remaining window capacity", limited: true}, {name: "truncated complete JSON rejected once", truncateFirst: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			messages := []Message{consumedMessage(model.RoleUser, "requirement"), consumedMessage(model.RoleAssistant, compactSizedText(2000)), consumedMessage(model.RoleAssistant, compactSizedText(2000))}
			plan, err := PlanCompact(messages, "", testCompactOptions(1))
			if err != nil || plan == nil || plan.Kind != CompactHistory {
				t.Fatalf("plan=%v err=%v", plan, err)
			}
			wantBudget := 128000
			if tt.limited {
				wantBudget = 4096
				plan.inputLimit = compactRequestTokens(t, plan) + wantBudget
			}
			var requests, sentBudget atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					MaxTokens int `json:"max_tokens"`
				}
				if json.NewDecoder(r.Body).Decode(&request) != nil {
					http.Error(w, "bad request", 400)
					return
				}
				sentBudget.Store(int32(request.MaxTokens))
				n := requests.Add(1)
				finish := "stop"
				if tt.truncateFirst && n == 1 {
					finish = "length"
				}
				sendCompactResponse(w, `{"summary":"facts"}`, finish)
			}))
			defer server.Close()
			c := testCompressor(t)
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			if err := c.Acquire(ctx); err != nil {
				t.Fatal(err)
			}
			out, usage, err := c.Generate(ctx, compactTestBinding(t, server.URL, 128000), model.Invocation{}, plan)
			if (err != nil) != tt.truncateFirst || (!tt.truncateFirst && out.Summary != "facts") || (tt.truncateFirst && out.Summary != "") || sentBudget.Load() != int32(wantBudget) || requests.Load() != 1 {
				t.Fatalf("output=%+v err=%v budget=%d requests=%d", out, err, sentBudget.Load(), requests.Load())
			}
			if usage == nil || usage.OutputTokens != 1600 {
				t.Fatalf("usage=%+v", usage)
			}
		})
	}
}

func TestCompactGenerationReturnsFailuresWithoutAnotherPaidRequest(t *testing.T) {
	for _, tt := range []struct {
		name         string
		status       int
		body         string
		wantRequests int
		wantError    bool
	}{
		{name: "overload", status: 503, wantRequests: 1, wantError: true},
		{name: "malformed output", body: `{"summary":`, wantRequests: 1, wantError: true},
		{name: "empty output", body: `{}`, wantRequests: 1, wantError: true},
		{name: "bad request", status: 400, wantRequests: 1, wantError: true},
		{name: "authentication", status: 401, wantRequests: 1, wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			plan := &CompactPlan{Prefix: []Message{consumedMessage(model.RoleAssistant, "facts")}, OutputBudget: 1024, inputLimit: 180000, coefficient: 1}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if requests.Add(1) == 1 {
					if tt.status > 0 {
						http.Error(w, "temporary or rejected", tt.status)
						return
					}
					sendCompactResponse(w, tt.body, "stop")
					return
				}
				sendCompactResponse(w, `{"summary":"recovered facts"}`, "stop")
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			c := testCompressor(t)
			if err := c.Acquire(ctx); err != nil {
				t.Fatal(err)
			}
			out, _, err := c.Generate(ctx, compactTestBinding(t, server.URL, 32000), model.Invocation{}, plan)
			if (err != nil) != tt.wantError || requests.Load() != int32(tt.wantRequests) {
				t.Fatalf("out=%+v err=%v requests=%d", out, err, requests.Load())
			}
		})
	}
}

func TestCompactGenerationCancellationDoesNotRetryOrLeakSlot(t *testing.T) {
	started := make(chan struct{})
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request any
		_ = json.NewDecoder(r.Body).Decode(&request)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		requests.Add(1)
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := testCompressor(t)
	if err := c.Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	plan := &CompactPlan{OutputBudget: 1024, inputLimit: 180000, coefficient: 1}
	done := make(chan error, 1)
	binding := compactTestBinding(t, server.URL, 32000)
	go func() {
		_, _, err := c.Generate(ctx, binding, model.Invocation{}, plan)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request not started")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancel returned nil")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel blocked")
	}
	acquireCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	if err := c.Acquire(acquireCtx); err != nil {
		t.Fatal(err)
	}
	c.ReleaseBackground()
	if requests.Load() != 1 {
		t.Fatalf("requests=%d want 1", requests.Load())
	}
}

func TestDecodeCompactOutputRepairsIndependentRegions(t *testing.T) {
	plan := &CompactPlan{Prefix: []Message{consumedMessage(model.RoleAssistant, "old")}, Groups: []ToolGroup{{Index: 3}, {Index: 7}}}
	for _, tt := range []struct {
		name, raw string
		wantError bool
		wantKeep  bool
	}{
		{name: "fences and extra fields", raw: "```json\n{\"summary\":\"new\",\"extra\":true,\"observations\":[{\"id\":0,\"text\":\"evidence\"}]}\n```"},
		{name: "null id cannot become zero", raw: `{"summary":"new","observations":[{"id":null,"text":"stray"}]}`, wantKeep: true},
		{name: "duplicate ambiguous", raw: `{"summary":"new","observations":[{"id":0,"text":"first"},{"id":0,"text":"second"}]}`, wantKeep: true},
		{name: "invalid item independent", raw: `{"summary":"new","observations":[null,{"id":0,"text":"evidence"},{"id":1,"text":4}]}`},
		{name: "usable tool without history", raw: `{"summary":42,"observations":[{"id":0,"text":"evidence"}]}`},
		{name: "incomplete", raw: `{"summary":"new"`, wantError: true},
		{name: "trailing content", raw: `{"summary":"new"} garbage`, wantError: true},
		{name: "extra value", raw: `{"summary":"new"}{}`, wantError: true},
		{name: "empty", raw: `{}`, wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out, err := decodeCompactOutput(tt.raw, plan)
			if (err != nil) != tt.wantError {
				t.Fatalf("out=%+v err=%v", out, err)
			}
			if err == nil && (len(out.Observations) != 2 || out.Observations[0].ID != 3 || out.Observations[0].Keep != tt.wantKeep || !out.Observations[1].Keep) {
				t.Fatalf("out=%+v", out)
			}
		})
	}
}

func TestToolOutputLimitRemainsBounded(t *testing.T) {
	value := strings.Repeat("line\n", 20000)
	got := TruncateToolOutputForContext(value)
	if len(got) >= len(value) || !strings.Contains(got, "truncated") {
		t.Fatal("output was not bounded")
	}
}
func BenchmarkLargeSessionMemory(b *testing.B) {
	for _, tokens := range []int{500000, 1000000} {
		b.Run(fmt.Sprintf("%dk", tokens/1000), func(b *testing.B) {
			messages := compactToolBatch(100, tokens/100, tokens/10)
			b.Run("plan", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if _, err := PlanCompact(messages, "", CompactOptions{Budget: tokens, TargetGain: tokens * 15 / 100, MinimumGain: tokens / 50, InputLimit: tokens, OutputLimit: 128000, Coefficient: 1}); err != nil {
						b.Fatal(err)
					}
				}
			})
			plan, err := PlanCompact(messages, "", CompactOptions{Budget: tokens, TargetGain: tokens * 15 / 100, MinimumGain: tokens / 50, InputLimit: tokens, OutputLimit: 128000, Coefficient: 1})
			if err != nil || plan == nil {
				b.Fatalf("plan=%v err=%v", plan, err)
			}
			output := CompactOutput{}
			for _, group := range plan.Groups {
				output.Observations = append(output.Observations, ToolObservation{ID: group.Index, Text: "Exact tool evidence; validation remains pending."})
			}
			b.Run("source_key", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					plan.SourceKey()
				}
			})
			keys := make(map[string]bool)
			for i := 1; i < len(messages); i++ {
				if len(messages[i].ToolCalls) > 0 {
					keys[(ToolGroup{Messages: messages[i : i+2]}).SourceKey()] = true
				}
			}
			b.Run("prune_sources", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					PruneToolSourceKeys(messages, keys)
				}
			})
			b.Run("apply", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if _, err := plan.Apply(messages, "", output); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("checkpoint_projection", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					json.Marshal(ProjectMessages(messages))
				}
			})
			b.Run("sqlite_save", func(b *testing.B) {
				store, err := NewStore(filepath.Join(b.TempDir(), "memory.db"))
				if err != nil {
					b.Fatal(err)
				}
				defer store.Close()
				if _, err = store.DB().Exec(`INSERT INTO sessions(id,created_at,updated_at) VALUES ('s',datetime('now'),datetime('now'))`); err != nil {
					b.Fatal(err)
				}
				states := NewSessionStateStore(store.DB())
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if err = states.Save(context.Background(), "s", "summary", messages); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

func TestGenerationDoesNotMutateSource(t *testing.T) {
	messages := toolHistory()
	before := ProjectMessages(messages)
	plan, err := PlanCompact(messages, "old", testCompactOptions(1000))
	if err != nil || plan == nil {
		t.Fatalf("plan=%v err=%v", plan, err)
	}
	if _, err = compactPayload(plan); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, ProjectMessages(messages)) {
		t.Fatal("request changed source")
	}
}

func TestCompactFinishRequiresCompleteResponse(t *testing.T) {
	for _, tt := range []struct {
		name   string
		finish *model.FinishInfo
		want   bool
	}{
		{name: "missing"},
		{name: "stop", finish: &model.FinishInfo{Reason: "stop"}, want: true},
		{name: "completed", finish: &model.FinishInfo{Status: "completed"}, want: true},
		{name: "length", finish: &model.FinishInfo{Reason: "length"}},
		{name: "incomplete", finish: &model.FinishInfo{Reason: "stop", IncompleteReason: "output_limit"}},
		{name: "failed", finish: &model.FinishInfo{Reason: "stop", Status: "failed"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := completeCompactFinish(tt.finish); got != tt.want {
				t.Fatalf("got=%v want=%v", got, tt.want)
			}
		})
	}
}

func TestReadCompactStreamRejectsMissingDoneAndRetainsUsage(t *testing.T) {
	for _, tt := range []struct {
		name      string
		chunks    []model.Chunk
		wantError bool
	}{
		{name: "closed without done", chunks: []model.Chunk{{Content: `{"summary":"facts"}`, Finish: &model.FinishInfo{Reason: "stop"}, Usage: &model.Usage{OutputTokens: 123}}}, wantError: true},
		{name: "done without finish", chunks: []model.Chunk{{Content: `{"summary":"facts"}`, Usage: &model.Usage{OutputTokens: 123}, Done: true}}, wantError: true},
		{name: "complete", chunks: []model.Chunk{{Content: `{"summary":"facts"}`, Finish: &model.FinishInfo{Reason: "stop"}, Usage: &model.Usage{OutputTokens: 123}, Done: true}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			chunks := make(chan model.Chunk, len(tt.chunks))
			for _, chunk := range tt.chunks {
				chunks <- chunk
			}
			close(chunks)
			text, usage, err := readCompactStream(context.Background(), chunks, 1024)
			if (err != nil) != tt.wantError || usage == nil || usage.OutputTokens != 123 {
				t.Fatalf("text=%q usage=%+v err=%v", text, usage, err)
			}
			if tt.wantError && text != "" {
				t.Fatal("incomplete output returned usable text")
			}
		})
	}
}
