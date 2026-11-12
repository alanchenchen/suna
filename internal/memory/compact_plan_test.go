package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/prompt"
)

func TestNormalizeToolObservationsRepairsModelOutput(t *testing.T) {
	groups := []ToolGroup{{Index: 0}, {Index: 3}, {Index: 7}}
	cases := []struct {
		name string
		out  []ToolObservation
		want []ToolObservation
	}{
		{
			name: "missing groups keep originals",
			out:  []ToolObservation{{ID: 0, Text: "evidence"}},
			want: []ToolObservation{{ID: 0, Text: "evidence"}, {ID: 3, Keep: true}, {ID: 7, Keep: true}},
		},
		{
			name: "unknown ids dropped",
			out:  []ToolObservation{{ID: 0, Text: "evidence"}, {ID: 3, Keep: true}, {ID: 7, Keep: true}, {ID: 99, Text: "stray"}},
			want: []ToolObservation{{ID: 0, Text: "evidence"}, {ID: 3, Keep: true}, {ID: 7, Keep: true}},
		},
		{
			name: "duplicate keeps first",
			out:  []ToolObservation{{ID: 0, Text: "first"}, {ID: 0, Text: "second"}, {ID: 3, Keep: true}, {ID: 7, Keep: true}},
			want: []ToolObservation{{ID: 0, Text: "first"}, {ID: 3, Keep: true}, {ID: 7, Keep: true}},
		},
		{
			name: "empty text falls back to keep",
			out:  []ToolObservation{{ID: 0, Text: "  "}, {ID: 3, Keep: true}, {ID: 7, Keep: true}},
			want: []ToolObservation{{ID: 0, Keep: true}, {ID: 3, Keep: true}, {ID: 7, Keep: true}},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeToolObservations(groups, CompactOutput{Observations: tt.out})
			if !reflect.DeepEqual(got.Observations, tt.want) {
				t.Fatalf("got %#v, want %#v", got.Observations, tt.want)
			}
		})
	}
}

func TestToolApplyAcceptsPartialModelOutput(t *testing.T) {
	messages := compactToolBatch(2, 4000, 100100)
	p, err := PlanCompact(messages, "", CompactTools, 1000000, 1000000, 8000, 1, true)
	if err != nil || p == nil || len(p.Groups) != 2 {
		t.Fatalf("plan=%v err=%v", p, err)
	}
	// 只返回第一组的证据，第二组漏写：必须降级为保留原文而不是整批失败。
	got, _, err := p.Apply(messages, "", CompactOutput{Observations: []ToolObservation{{ID: p.Groups[0].Index, Text: "evidence"}}})
	if err != nil {
		t.Fatalf("partial output rejected: %v", err)
	}
	if len(got) <= 0 {
		t.Fatal("replacement produced empty history")
	}
	if !reflect.DeepEqual(got[len(got)-len(messages)+p.Groups[1].Index:], messages[p.Groups[1].Index:]) {
		t.Fatal("missing group was not preserved verbatim")
	}
}

func TestToolGroupWireUsesDenseIDs(t *testing.T) {
	groups := []ToolGroup{{Index: 0}, {Index: 3}, {Index: 7}}
	wire := compactToolGroups(groups)
	for i, g := range wire {
		if g.ID != i {
			t.Fatalf("wire id %d, want %d", g.ID, i)
		}
	}
	restored := restoreToolGroupIndexes(groups, []ToolObservation{{ID: 0, Text: "a"}, {ID: 1, Text: "b"}, {ID: 2, Text: "c"}})
	want := []int{0, 3, 7}
	for i, v := range restored {
		if v.ID != want[i] {
			t.Fatalf("restored id %d, want %d", v.ID, want[i])
		}
	}
	// 越界编号必须归一成 -1：真实位置可能恰好等于越界稠密编号，
	// 原样透传会把证据套到另一组上。
	out := restoreToolGroupIndexes(groups, []ToolObservation{{ID: 9, Text: "stray"}})
	if out[0].ID != -1 {
		t.Fatalf("out-of-range id rewritten to %d, want -1", out[0].ID)
	}
}

func TestRestoreOutOfRangeIDsNeverMatchRealIndexes(t *testing.T) {
	// 真实位置 2 和 5，合法稠密编号只有 0、1。
	groups := []ToolGroup{{Index: 2}, {Index: 5}}
	got := restoreToolGroupIndexes(groups, []ToolObservation{{ID: 5, Text: "model text"}})
	out := normalizeToolObservations(groups, CompactOutput{Observations: got})
	for _, v := range out.Observations {
		if v.Text == "model text" {
			t.Fatalf("out-of-range dense id 5 matched real index 5: %#v", v)
		}
	}
	if len(out.Observations) != len(groups) {
		t.Fatalf("got %d observations, want %d", len(out.Observations), len(groups))
	}
}

func TestGenerateSendsDenseGroupIDsAndRestoresPlanIndexes(t *testing.T) {
	messages := compactToolBatch(2, 4000, 100100)
	plan, err := PlanCompact(messages, "", CompactTools, 1000000, 1000000, 8000, 1, true)
	if err != nil || plan == nil || len(plan.Groups) != 2 {
		t.Fatalf("plan=%v err=%v", plan, err)
	}
	// 真实位置必须与稠密编号不同，否则测试无法区分 wire id 与 plan index。
	if plan.Groups[0].Index == 0 && plan.Groups[1].Index == 1 {
		t.Fatalf("fixture indexes %d,%d are already dense", plan.Groups[0].Index, plan.Groups[1].Index)
	}
	var sentIDs []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(body, &req)
		if len(req.Messages) > 0 {
			// prompt 形如 "...\nInput:\n{json}"，从第一个 { 起截取完整 JSON 对象。
			content := req.Messages[0].Content
			start := strings.Index(content, `{"previous_summary"`)
			if start >= 0 {
				var payload struct {
					Groups []struct {
						ID int `json:"id"`
					} `json:"groups"`
				}
				if err := json.Unmarshal([]byte(content[start:]), &payload); err == nil {
					for _, g := range payload.Groups {
						sentIDs = append(sentIDs, g.ID)
					}
				}
			}
		}
		// 回显稠密编号 0、1，验证还原成真实位置。
		content, _ := json.Marshal(`{"observations":[{"id":0,"text":"first"},{"id":1,"text":"second"}]}`)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"model\":\"t\",\"choices\":[{\"index\":0,\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", content)
	}))
	defer server.Close()
	router, err := model.NewRouter(&config.Config{Models: []config.ModelConfig{{Provider: "custom", Protocol: config.ModelProtocolOpenAIChat, Model: "t", BaseURL: server.URL, APIKey: "test-api-key", ContextWindow: 200000, MaxOutputTokens: 32000}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := router.Bind("custom/t")
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
	out, _, err := compressor.Generate(ctx, binding, model.Invocation{}, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(sentIDs) != 2 || sentIDs[0] != 0 || sentIDs[1] != 1 {
		t.Fatalf("sent ids = %v, want [0 1]", sentIDs)
	}
	wantIDs := []int{plan.Groups[0].Index, plan.Groups[1].Index}
	for i, v := range out.Observations {
		if v.ID != wantIDs[i] {
			t.Fatalf("restored id %d, want %d", v.ID, wantIDs[i])
		}
	}
}
