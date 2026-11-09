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
func toolHistory() []Message {
	return []Message{consumedMessage(model.RoleUser, "inspect"), {Role: model.RoleAssistant, TextContent: "original prose", Consumed: true, ToolCalls: []ToolCall{{ID: "call-a", Name: "readfile", Arguments: `{"path":"a"}`}, {ID: "call-b", Name: "exec", Arguments: `{"command":"test"}`}}}, {Role: model.RoleTool, ToolCallID: "call-a", TextContent: strings.Repeat("source", 1200), ToolStatus: "completed", Consumed: true}, {Role: model.RoleTool, ToolCallID: "call-b", TextContent: "exact failure at a:12", ToolStatus: "failed", Consumed: true}, consumedMessage(model.RoleAssistant, "results explained"), consumedMessage(model.RoleUser, strings.Repeat("recent tail ", 1000)), consumedMessage(model.RoleAssistant, "tail reply")}
}
func TestToolPlanReplacesCompleteGroupAtOriginalPosition(t *testing.T) {
	messages := toolHistory()
	plan, err := PlanCompact(messages, "old summary", CompactTools, 10000, 100000, 32000, 1, true)
	if err != nil || plan == nil {
		t.Fatalf("plan = %v, error = %v", plan, err)
	}
	if len(plan.Groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(plan.Groups))
	}
	messages = append(messages, NewTextMessage(model.RoleUser, "new correction"))
	result, summary, err := plan.Apply(messages, "old summary", CompactOutput{Observations: []ToolObservation{{ID: 1, Text: "read a; exec failed exactly at a:12"}}})
	if err != nil {
		t.Fatal(err)
	}
	if summary != "old summary" || len(result) != len(messages)-2 {
		t.Fatalf("summary=%q messages=%d", summary, len(result))
	}
	if !strings.HasPrefix(result[1].ModelMessage().Text(), "original prose\n\n[Historical tool observation") || len(result[1].ToolCalls) != 0 {
		t.Fatalf("got %#v", result[1])
	}
	if result[len(result)-1].Text() != "new correction" {
		t.Fatal("new suffix was not preserved")
	}
	messages[2].TextContent = "changed source"
	if _, _, err = plan.Apply(messages, "old summary", CompactOutput{Observations: []ToolObservation{{ID: 1, Text: "observation"}}}); err == nil {
		t.Fatal("changed source accepted")
	}
}
func TestToolPlanProtectsUnconsumedAndIncompleteGroups(t *testing.T) {
	for _, name := range []string{"unconsumed", "unconsumed-result", "incomplete"} {
		t.Run(name, func(t *testing.T) {
			messages := toolHistory()
			if name == "unconsumed" {
				messages[1].Consumed = false
			} else if name == "unconsumed-result" {
				messages[3].Consumed = false
			} else {
				messages = append(messages[:3], messages[4:]...)
			}
			plan, err := PlanCompact(messages, "", CompactTools, 10000, 100000, 32000, 1, true)
			if err != nil {
				t.Fatal(err)
			}
			if plan != nil {
				t.Fatal("unsafe group selected")
			}
		})
	}
}
func TestHistoryCopiesWholeUserOriginalsAndEnforcesBothQuotas(t *testing.T) {
	messages := []Message{consumedMessage(model.RoleUser, strings.Repeat("long original ", 100)), consumedMessage(model.RoleAssistant, "answer"), consumedMessage(model.RoleUser, "short original"), consumedMessage(model.RoleAssistant, "answer"), NewTextMessage(model.RoleUser, "pending")}
	p, err := PlanCompact(messages, "old", CompactHistory, 10000, 100000, 32000, 1, true)
	if err != nil || p == nil {
		t.Fatalf("plan=%v err=%v", p, err)
	}
	if _, _, err = p.Apply(messages, "old", CompactOutput{Summary: "new", Keep: []int{2}}); err == nil {
		t.Fatal("token quota violation accepted")
	}
	result, summary, err := p.Apply(messages, "old", CompactOutput{Summary: "new", Keep: []int{0}})
	if err != nil {
		t.Fatal(err)
	}
	if summary != "new" || result[0].Text() != messages[0].Text() || result[len(result)-1].Text() != "pending" {
		t.Fatal("original or suffix changed")
	}
}
func TestToolOutputLimitRemainsBounded(t *testing.T) {
	value := strings.Repeat("line\n", 20000)
	got := TruncateToolOutputForContext(value)
	if len(got) >= len(value) || !strings.Contains(got, "truncated") {
		t.Fatal("output was not bounded")
	}
}

func BenchmarkPlanToolHistory(b *testing.B) {
	messages := toolHistory()
	for i := 0; i < 30; i++ {
		messages = append(messages, toolHistory()...)
	}
	b.ReportAllocs()
	for b.Loop() {
		PlanCompact(messages, "", CompactTools, 1000000, 1000000, 128000, 1, false)
	}
}

func BenchmarkLargeSessionMemory(b *testing.B) {
	for _, tokens := range []int{500000, 1000000} {
		b.Run(fmt.Sprintf("%dk", tokens/1000), func(b *testing.B) {
			messages := make([]Message, 0, 300)
			for i := 0; i < 100; i++ {
				messages = append(messages, consumedMessage(model.RoleUser, "inspect numbered evidence"))
				declaration := consumedMessage(model.RoleAssistant, "original reasoning")
				declaration.ToolCalls = []ToolCall{{ID: fmt.Sprintf("call-%d", i), Name: "readfile", Arguments: `{"path":"example.go"}`}}
				result := consumedMessage(model.RoleTool, strings.Repeat("data", tokens/100))
				result.ToolCallID = declaration.ToolCalls[0].ID
				result.ToolStatus = "completed"
				messages = append(messages, declaration, result, consumedMessage(model.RoleAssistant, "complete explanation"))
			}
			b.Run("plan", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					PlanCompact(messages, "", CompactTools, tokens, tokens, 128000, 1, false)
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
func TestHistoryRetainsLegacyOriginalsWithoutCountingThemTowardQuota(t *testing.T) {
	legacy := consumedMessage(model.RoleUser, "legacy content")
	legacy.Origin = OriginLegacy
	messages := []Message{legacy, consumedMessage(model.RoleAssistant, "old reply"), consumedMessage(model.RoleUser, "known user"), consumedMessage(model.RoleAssistant, "new reply"), NewTextMessage(model.RoleUser, "pending")}
	p, err := PlanCompact(messages, "", CompactHistory, 10000, 100000, 32000, 1, true)
	if err != nil || p == nil {
		t.Fatalf("plan=%v err=%v", p, err)
	}
	if _, _, err = p.Apply(messages, "", CompactOutput{Summary: "memory", Keep: []int{0}}); err == nil {
		t.Fatal("legacy original satisfied known user quota")
	}
	got, _, err := p.Apply(messages, "", CompactOutput{Summary: "memory", Keep: []int{2}})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Text() != legacy.Text() || got[1].Text() != "known user" {
		t.Fatal("full originals or order were lost")
	}
}

// 按明确 token 规模构造来源，使容量回归不依赖具体分词器。
func compactSizedText(tokens int) string {
	if tokens == 0 {
		return ""
	}
	return strings.Repeat("x", max(1, (tokens-1)*4))
}

func compactToolBatch(groups, resultTokens, tailTokens int) []Message {
	messages := []Message{consumedMessage(model.RoleUser, "inspect all evidence")}
	for i := 0; i < groups; i++ {
		declaration := consumedMessage(model.RoleAssistant, fmt.Sprintf("original reasoning %d", i))
		declaration.ToolCalls = []ToolCall{{ID: fmt.Sprintf("batch-%d", i), Name: "readfile", Arguments: `{"path":"evidence"}`}}
		result := consumedMessage(model.RoleTool, compactSizedText(resultTokens))
		result.ToolCallID = declaration.ToolCalls[0].ID
		result.ToolStatus = "completed"
		messages = append(messages, declaration, result)
	}
	return append(messages, consumedMessage(model.RoleAssistant, "complete explanation"), consumedMessage(model.RoleUser, compactSizedText(tailTokens)), consumedMessage(model.RoleAssistant, "tail reply"))
}

func compactRequestTokens(t *testing.T, p *CompactPlan) int {
	t.Helper()
	payload, err := json.Marshal(struct {
		Summary    string      `json:"previous_summary"`
		Groups     []ToolGroup `json:"groups,omitempty"`
		History    []Message   `json:"history,omitempty"`
		Background []Message   `json:"readonly_background"`
	}{p.Summary, p.Groups, p.Prefix, p.Background})
	if err != nil {
		t.Fatal(err)
	}
	loader, err := prompt.New()
	if err != nil {
		t.Fatal(err)
	}
	name := "compress"
	if p.Kind == CompactTools {
		name = "compress_tools"
	}
	text, err := loader.Render(name, map[string]any{"Content": string(payload)})
	if err != nil {
		t.Fatal(err)
	}
	return model.EstimateTokens(text)
}

func TestCompactMillionWindowWith8kBindingOutput(t *testing.T) {
	messages := compactToolBatch(6, 50000, 100100)
	for _, outputLimit := range []int{8000, 128000} {
		p, err := PlanCompact(messages, "previous memory", CompactTools, 1000000, 1000000, outputLimit, 1, false)
		if err != nil || p == nil {
			t.Fatalf("output limit %d: plan=%v err=%v", outputLimit, p, err)
		}
		if len(p.Groups) != 6 || p.SourceTokens < 250000 {
			t.Fatalf("output limit %d: selected %d groups / %d source tokens", outputLimit, len(p.Groups), p.SourceTokens)
		}
		if p.OutputBudget > outputLimit || outputLimit == 8000 && p.OutputBudget != outputLimit || outputLimit > 8000 && p.OutputBudget <= 8000 {
			t.Fatalf("output limit %d: budget=%d", outputLimit, p.OutputBudget)
		}
		if compactRequestTokens(t, p)+p.OutputBudget > 1000000 {
			t.Fatal("complete request does not fit")
		}
	}

	// 历史来源也应能超过 24k；真实 8k 输出上限不能卡死包含完整原话的选区。
	history := []Message{consumedMessage(model.RoleUser, compactSizedText(40000)), consumedMessage(model.RoleAssistant, compactSizedText(180000)), NewTextMessage(model.RoleUser, "pending")}
	p, err := PlanCompact(history, "", CompactHistory, 1000000, 1000000, 8000, 1, false)
	if err != nil || p == nil || len(p.Prefix) != 2 || p.OutputBudget != 8000 {
		t.Fatalf("history plan=%v err=%v", p, err)
	}
}

func TestCompactGenerationBudgetIncludesReasoningAndRejectsTruncation(t *testing.T) {
	for _, tc := range []struct {
		name, finish string
		limited      bool
		wantError    bool
	}{
		{name: "binding output capacity", finish: "stop"},
		{name: "remaining window capacity", finish: "stop", limited: true},
		{name: "complete JSON but truncated response", finish: "length", wantError: true},
		{name: "missing finish", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			messages := []Message{consumedMessage(model.RoleUser, "keep this requirement"), consumedMessage(model.RoleAssistant, "completed earlier work")}
			original := ProjectMessages(messages)
			plan, err := PlanCompact(messages, "", CompactHistory, 100000, 180000, 128000, 1, true)
			if err != nil || plan == nil {
				t.Fatalf("plan = %v, error = %v", plan, err)
			}
			if plan.OutputBudget != 1024 {
				t.Fatalf("estimated text budget = %d, want 1024", plan.OutputBudget)
			}
			wantBudget := 128000
			if tc.limited {
				wantBudget = 4096
				plan.inputLimit = compactRequestTokens(t, plan) + wantBudget
			}
			var requests, sentBudget atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				var req struct {
					MaxTokens int `json:"max_tokens"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, "invalid request", http.StatusBadRequest)
					return
				}
				sentBudget.Store(int32(req.MaxTokens))
				finish, content := tc.finish, `{"summary":"facts","keep":[0]}`
				// 复现真实失败：总额度被误设为正文的 1024 时，reasoning 用完额度而没有正文。
				if req.MaxTokens <= 1024 {
					finish, content = "length", ""
				}
				data, _ := json.Marshal(map[string]any{
					"id": "offline", "object": "chat.completion.chunk", "model": "test",
					"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"reasoning_content": strings.Repeat("r ", 1500), "content": content}, "finish_reason": finish}},
					"usage":   map[string]any{"prompt_tokens": 5000, "completion_tokens": 1600, "total_tokens": 6600},
				})
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", data)
			}))
			defer server.Close()
			router, err := model.NewRouter(&config.Config{Models: []config.ModelConfig{{Provider: "custom", Protocol: config.ModelProtocolOpenAIChat, Model: "test", BaseURL: server.URL, APIKey: "test-api-key", ContextWindow: 200000, MaxOutputTokens: 128000}}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			binding, err := router.Bind("custom/test")
			if err != nil {
				t.Fatal(err)
			}
			loader, err := prompt.New()
			if err != nil {
				t.Fatal(err)
			}
			compressor := NewCompressor()
			compressor.SetPrompts(loader)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := compressor.Acquire(ctx); err != nil {
				t.Fatal(err)
			}
			output, usage, err := compressor.Generate(ctx, binding, model.Invocation{}, plan)
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v, wantError = %v", err, tc.wantError)
			}
			if sentBudget.Load() != int32(wantBudget) || requests.Load() != 1 {
				t.Fatalf("budget = %d, requests = %d, want %d and 1", sentBudget.Load(), requests.Load(), wantBudget)
			}
			if usage == nil || usage.OutputTokens != 1600 {
				t.Fatalf("usage = %+v, want received output usage", usage)
			}
			if tc.wantError {
				if output.Summary != "" || len(output.Keep) != 0 {
					t.Fatalf("rejected output = %+v, want empty", output)
				}
			} else if output.Summary != "facts" || !reflect.DeepEqual(output.Keep, []int{0}) {
				t.Fatalf("output = %+v, want complete JSON", output)
			}
			if !reflect.DeepEqual(ProjectMessages(messages), original) {
				t.Fatal("generation changed original messages")
			}
		})
	}
}

func TestHistoryWholeLongOriginalDoesNotInventSavings(t *testing.T) {
	messages := []Message{consumedMessage(model.RoleUser, compactSizedText(40000)), consumedMessage(model.RoleAssistant, "short reply"), NewTextMessage(model.RoleUser, "pending")}
	p, err := PlanCompact(messages, "", CompactHistory, 100000, 1000000, 8000, 1, false)
	if err != nil || p != nil {
		t.Fatalf("unreleasable original selected: plan=%v err=%v", p, err)
	}
	p, err = PlanCompact(messages, "", CompactHistory, 100000, 1000000, 8000, 1, true)
	if err != nil || p == nil {
		t.Fatalf("forced plan=%v err=%v", p, err)
	}
	if _, _, err = p.Apply(messages, "", CompactOutput{Summary: "memory"}); err == nil {
		t.Fatal("single whole original was allowed to disappear")
	}
	got, _, err := p.Apply(messages, "", CompactOutput{Summary: "memory", Keep: []int{0}})
	if err != nil || !reflect.DeepEqual(got[0], messages[0]) {
		t.Fatalf("whole original changed: err=%v", err)
	}

	// 不能把完整 40k 原话估成 20k 而停在首段；只扩大到有收益的交流边界。
	messages = []Message{messages[0], consumedMessage(model.RoleAssistant, compactSizedText(5000)), consumedMessage(model.RoleUser, "second request"), consumedMessage(model.RoleAssistant, compactSizedText(30000)), consumedMessage(model.RoleUser, "later discussion"), consumedMessage(model.RoleAssistant, compactSizedText(30000)), NewTextMessage(model.RoleUser, "pending")}
	p, err = PlanCompact(messages, "", CompactHistory, 100000, 1000000, 8000, 1, false)
	if err != nil || p == nil || len(p.Prefix) != 4 {
		t.Fatalf("minimum useful prefix: plan=%v err=%v", p, err)
	}
}

func TestRetainedOriginalCostsUseWholeFeasibleSets(t *testing.T) {
	for _, tc := range []struct {
		name  string
		sizes []int
		want  []int
	}{
		{"single long", []int{40000}, []int{0, 40000}},
		{"empty originals still count", []int{40000, 0, 0}, []int{0, 40000, 40000, 40000}},
		{"long plus shorts", []int{40000, 1, 1}, []int{0, 40000, 40000, 40001}},
		{"avoid long outlier", []int{50, 10, 10, 10, 10, 10, 10}, []int{0, 50, 50, 60, 60, 70, 50, 60}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var messages []Message
			for _, size := range tc.sizes {
				messages = append(messages, consumedMessage(model.RoleUser, compactSizedText(size)))
			}
			if got := retainedOriginalCosts(messages); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("retained=%v want=%v", got, tc.want)
			}
		})
	}
	legacy := consumedMessage(model.RoleUser, compactSizedText(7))
	legacy.Origin = OriginLegacy
	injected := consumedMessage(model.RoleUser, compactSizedText(100))
	injected.Origin = OriginTool
	messages := []Message{legacy, injected, consumedMessage(model.RoleUser, compactSizedText(40))}
	if got := retainedOriginalCosts(messages); !reflect.DeepEqual(got, []int{0, 7, 7, 47}) {
		t.Fatalf("legacy/tool quota accounting=%v", got)
	}
}

func TestToolCompactFitsWholeBatchesWithoutSourceTruncation(t *testing.T) {
	remaining := compactToolBatch(7, 12000, 100100)
	const inputLimit = 155000
	for batch := 0; ; batch++ {
		p, err := PlanCompact(remaining, "", CompactTools, 1000000, inputLimit, 8000, 1.1, true)
		if err != nil {
			t.Fatal(err)
		}
		if p == nil {
			if batch != 4 {
				t.Fatalf("got %d batches, want 4", batch)
			}
			for _, m := range remaining {
				if len(m.ToolCalls) > 0 || m.Role == model.RoleTool {
					t.Fatal("complete eligible source was left behind")
				}
			}
			break
		}
		wantGroups := 2
		if batch == 3 {
			wantGroups = 1
		}
		if batch > 3 || len(p.Groups) != wantGroups {
			t.Fatalf("batch %d: got %d groups", batch, len(p.Groups))
		}
		if model.ApplyCoefficient(compactRequestTokens(t, p), 1.1)+p.OutputBudget > inputLimit || model.ApplyCoefficient(p.inputTokens, 1.1)+p.OutputBudget > inputLimit {
			t.Fatal("complete payload and output exceed input capacity")
		}
		observations := make([]ToolObservation, 0, len(p.Groups))
		for _, group := range p.Groups {
			if len(group.Messages) != 2 || group.Messages[1].Text() != compactSizedText(12000) {
				t.Fatal("source was split or truncated")
			}
			observations = append(observations, ToolObservation{ID: group.Index, Text: "exact evidence retained"})
		}
		remaining, _, err = p.Apply(remaining, "", CompactOutput{Observations: observations})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestToolBackgroundFindsCompleteExplanationAndCountsOnce(t *testing.T) {
	messages := compactToolBatch(2, 4000, 100100)
	// 新工具声明和未消费的助手内容不能冒充完整解释。
	messages = append(messages[:5], append([]Message{NewTextMessage(model.RoleAssistant, compactSizedText(30000))}, messages[5:]...)...)
	p, err := PlanCompact(messages, "", CompactTools, 1000000, 135000, 8000, 1, true)
	if err != nil || p == nil || len(p.Groups) != 2 {
		t.Fatalf("plan=%v err=%v", p, err)
	}
	want := []string{compactSizedText(100100), "inspect all evidence", "complete explanation"}
	if len(p.Background) != len(want) {
		t.Fatalf("background count=%d want=%d", len(p.Background), len(want))
	}
	for i, m := range p.Background {
		if m.Text() != want[i] || len(m.ToolCalls) != 0 {
			t.Fatalf("background %d is not the positioned complete explanation/user", i)
		}
	}
	summary, _ := json.Marshal(p.Summary)
	input := model.EstimateTokens(string(summary)) + 2048
	for _, m := range p.Background {
		data, _ := json.Marshal(m)
		input += model.EstimateTokens(string(data)) + 2
	}
	for _, group := range p.Groups {
		input += 32
		for _, m := range group.Messages {
			data, _ := json.Marshal(m)
			input += model.EstimateTokens(string(data)) + 2
		}
	}
	if p.inputTokens != input {
		t.Fatalf("background/source counted more than once: input=%d want=%d", p.inputTokens, input)
	}
}

func TestHistoryLatestBackgroundRemovedBeforeFit(t *testing.T) {
	messages := []Message{consumedMessage(model.RoleUser, compactSizedText(20000)), consumedMessage(model.RoleAssistant, compactSizedText(1000))}
	p, err := PlanCompact(messages, "", CompactHistory, 1000000, 32000, 8000, 1, true)
	if err != nil || p == nil || len(p.Prefix) != len(messages) || len(p.Background) != 0 {
		t.Fatalf("selected latest user should not cost twice: plan=%v err=%v", p, err)
	}
	summary, _ := json.Marshal("")
	input := model.EstimateTokens(string(summary)) + 2048
	for _, m := range p.Prefix {
		data, _ := json.Marshal(m)
		input += model.EstimateTokens(string(data)) + 2
	}
	if p.inputTokens != input || compactRequestTokens(t, p)+p.OutputBudget > 32000 {
		t.Fatal("removed background retained an input charge")
	}
}

func TestHistoryNeverIncludesPartlyConsumedGroupOrExchange(t *testing.T) {
	for _, incomplete := range []bool{false, true} {
		messages := []Message{consumedMessage(model.RoleUser, "earlier"), consumedMessage(model.RoleAssistant, "complete earlier exchange")}
		messages = append(messages, toolHistory()...)
		if incomplete {
			messages = append(messages[:5], messages[6:]...)
		} else {
			messages[5].Consumed = false
		}
		p, err := PlanCompact(messages, "", CompactHistory, 1000000, 1000000, 8000, 1, true)
		if err != nil || p == nil || len(p.Prefix) != 2 {
			t.Fatalf("partial group/exchange included: incomplete=%v plan=%v err=%v", incomplete, p, err)
		}
	}
}

func TestToolOutputBudgetAllowsStructuralKeepForEveryGroup(t *testing.T) {
	p := &CompactPlan{Kind: CompactTools, coefficient: 1, inputTokens: 1000}
	for i := 0; i < 600; i++ {
		p.Groups = append(p.Groups, ToolGroup{Index: 1000000000 + i})
	}
	if planFits(p, 1000000, 8000) {
		t.Fatal("accepted more group entries than output can structurally represent")
	}
	p.Groups = p.Groups[:400]
	if !planFits(p, 1000000, 8000) {
		t.Fatal("3:1 or expected observation size became an unnecessary source cap")
	}
	observations := make([]ToolObservation, len(p.Groups))
	for i, group := range p.Groups {
		observations[i] = ToolObservation{ID: group.Index, Keep: true}
	}
	data, _ := json.Marshal(CompactOutput{Observations: observations})
	if model.EstimateTokens(string(data)) > p.OutputBudget {
		t.Fatal("budget cannot return keep for all groups")
	}
}

func TestToolBackgroundKeepsDistinctPositionsAndRollsBackFailedBatch(t *testing.T) {
	messages := compactToolBatch(2, 6000, 100100)
	messages[0] = consumedMessage(model.RoleUser, "same words, different facts")
	messages[len(messages)-2] = consumedMessage(model.RoleUser, "same words, different facts")
	// 大块近期尾部与最新用户消息分开，验证位置去重而非文本去重。
	messages = append(messages, consumedMessage(model.RoleAssistant, compactSizedText(100100)))
	messages = append(messages[:3], append([]Message{consumedMessage(model.RoleAssistant, "first explanation")}, messages[3:]...)...)
	p, err := PlanCompact(messages, "", CompactTools, 1000000, 14000, 8000, 1, true)
	if err != nil || p == nil || len(p.Groups) != 1 {
		t.Fatalf("complete-prefix rollback: plan=%v err=%v", p, err)
	}
	if len(p.Background) != 3 || p.Background[0].Text() != messages[0].Text() || p.Background[1].Text() != messages[0].Text() || p.Background[2].Text() != "first explanation" {
		t.Fatal("position deduplication lost a distinct fact or leaked failed group's background")
	}
	if compactRequestTokens(t, p)+p.OutputBudget > 14000 {
		t.Fatal("failed batch retained input cost")
	}
}

func TestToolApplyPreservesEveryOriginalTextBlockAndKeptGroup(t *testing.T) {
	messages := compactToolBatch(2, 4000, 100100)
	messages[1].TextContent = ""
	messages[1].Content = []model.ContentBlock{{Type: model.ContentText, Text: "first block"}, {Type: model.ContentText, Text: "second block"}}
	p, err := PlanCompact(messages, "", CompactTools, 1000000, 1000000, 8000, 1, true)
	if err != nil || p == nil || len(p.Groups) != 2 {
		t.Fatalf("plan=%v err=%v", p, err)
	}
	got, _, err := p.Apply(messages, "", CompactOutput{Observations: []ToolObservation{{ID: 1, Text: "evidence"}, {ID: 3, Keep: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got[1].ModelMessage().Text(), "first block\nsecond block\n\n[Historical tool observation") || !reflect.DeepEqual(got[2:4], messages[3:5]) {
		t.Fatal("declaring prose or the whole kept native group changed")
	}
}

func TestLaterToolBatchKeepsOriginalUserBackground(t *testing.T) {
	messages := compactToolBatch(3, 4000, 100100)
	plan, err := PlanCompact(messages, "", CompactTools, 1000000, 1000000, 8000, 1, true, 3)
	if err != nil || plan == nil || len(plan.Groups) != 2 || plan.Groups[0].Index != 3 {
		t.Fatalf("plan = %+v, error = %v", plan, err)
	}
	found := false
	for _, background := range plan.Background {
		found = found || background.Text() == messages[0].Text()
	}
	if !found {
		t.Fatal("later batch lost the original user requirement")
	}
}

func TestHistorySmallOutputFitsRequiredOriginalIndices(t *testing.T) {
	var messages []Message
	for i := 0; i < 1400; i++ {
		messages = append(messages, consumedMessage(model.RoleUser, "requirement"), consumedMessage(model.RoleAssistant, "answer"))
	}
	plan, err := PlanCompact(messages, "", CompactHistory, 1000000, 1000000, 128, 1, true)
	if err != nil || plan == nil || len(plan.Prefix) >= len(messages) || len(plan.Prefix)%2 != 0 {
		t.Fatalf("plan = %+v, error = %v", plan, err)
	}
	keep := make([]int, 0)
	for i := 0; i < len(plan.Prefix); i += 4 {
		keep = append(keep, i)
	}
	encoded, err := json.Marshal(CompactOutput{Summary: "facts", Keep: keep})
	if err != nil || model.EstimateTokens(string(encoded)) > plan.OutputBudget {
		t.Fatalf("minimum complete output cannot fit: %d/%d, error = %v", model.EstimateTokens(string(encoded)), plan.OutputBudget, err)
	}
}

func TestHistoryCanUseConsumedToolsWithoutWaitingForNextUser(t *testing.T) {
	messages := toolHistory()[:5]
	plan, err := PlanCompact(messages, "", CompactHistory, 10000, 100000, 8000, 1, true)
	if err != nil || plan == nil || len(plan.Prefix) < 4 {
		t.Fatalf("closed consumed coding history was frozen: plan = %+v, error = %v", plan, err)
	}
}
