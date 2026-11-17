package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/prompt"
)

func TestMixedPlanGeneratesOnceAndPersistsBeforeApplyingAtAdoptWatermark(t *testing.T) {
	var mu sync.Mutex
	requests := 0
	var payload struct {
		Summary string           `json:"previous_summary"`
		History []memory.Message `json:"history"`
		Groups  []struct {
			ID       int              `json:"id"`
			Messages []memory.Message `json:"messages"`
		} `json:"groups"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Messages) != 1 {
			http.Error(w, "invalid request", 400)
			return
		}
		parts := strings.SplitN(strings.ReplaceAll(request.Messages[0].Content, "\r\n", "\n"), "\nInput:\n", 2)
		if len(parts) != 2 {
			http.Error(w, "missing input", 400)
			return
		}
		mu.Lock()
		requests++
		err := json.Unmarshal([]byte(parts[1]), &payload)
		mu.Unlock()
		if err != nil {
			http.Error(w, "invalid input", 400)
			return
		}
		content, _ := json.Marshal(`{"summary":"Old requirement retained; earlier work completed.","observations":[{"id":0,"text":"Read evidence; validation pending."}]}`)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"id\":\"offline\",\"object\":\"chat.completion.chunk\",\"model\":\"offline\",\"choices\":[{\"index\":0,\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", content)
	}))
	defer server.Close()
	binding := regressionBind(t, regressionRouter(t, server.URL))
	loader, err := prompt.New()
	if err != nil {
		t.Fatal(err)
	}
	compressor := memory.NewCompressor()
	compressor.SetPrompts(loader)
	working := memory.NewWorkingMemory()
	consumed := func(role model.Role, text string) memory.Message {
		m := memory.NewTextMessage(role, text)
		m.Consumed = true
		if role == model.RoleUser {
			m.Origin = memory.OriginUser
		}
		return m
	}
	messages := []memory.Message{
		consumed(model.RoleUser, "old requirement"), consumed(model.RoleAssistant, strings.Repeat("h", 4*10000)),
		consumed(model.RoleUser, "inspect evidence"),
		{Role: model.RoleAssistant, TextContent: "preserved explanation", Consumed: true, ToolCalls: []memory.ToolCall{{ID: "call", Name: "readfile", Arguments: `{}`}}},
		{Role: model.RoleTool, ToolCallID: "call", TextContent: strings.Repeat("e", 4*4000), Consumed: true},
		consumed(model.RoleAssistant, "evidence explained"), consumed(model.RoleUser, strings.Repeat("t", 4*27500)), consumed(model.RoleAssistant, "recent answer"),
	}
	working.SetMessages(messages)
	state := NewContextMemory("old summary")
	q := CompactRequest{Binding: binding, Working: working, Request: model.CompletionRequest{MaxTokens: 32000}, Coefficient: 1, Calibrated: true}
	before := q.input(messages, state.Summary())
	budget := q.budget()
	if before < budget*60/100 || before >= budget*75/100 {
		t.Fatalf("before=%d budget=%d", before, budget)
	}
	ready := make(chan struct{}, 1)
	state.Ready = func() { ready <- struct{}{} }
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := state.Maintain(ctx, compressor, q, false); err != nil {
		t.Fatal(err)
	}
	awaitCompact(t, ready)
	if state.Snapshot().Kind != memory.CompactMixed {
		t.Fatalf("state=%+v", state.Snapshot())
	}
	if err := state.ApplyReady(ctx, compressor); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(working.Messages(), messages) || state.Snapshot().State != "ready" {
		t.Fatal("ready candidate applied below adoption threshold")
	}
	mu.Lock()
	count := requests
	head := len(payload.History)
	groups := len(payload.Groups)
	old := payload.Summary
	mu.Unlock()
	if count != 1 || head != 2 || groups != 1 || old != "old summary" {
		t.Fatalf("requests=%d head=%d groups=%d summary=%q", count, head, groups, old)
	}
	// 候选生成后追加的正式输入不进入旧选区；消费后才允许普通采用。
	suffix := consumed(model.RoleUser, strings.Repeat("n", 4*10000))
	working.AddMessage(suffix)
	commits := 0
	q.Commit = func(_ context.Context, summary string, replaced []memory.Message) error {
		commits++
		if state.Summary() != "old summary" || len(working.Messages()) != len(messages)+1 || len(working.Messages()[3].ToolCalls) != 1 {
			t.Fatal("live memory changed before durable commit")
		}
		if summary == "old summary" || !reflect.DeepEqual(replaced[len(replaced)-1], suffix) {
			t.Fatal("invalid replacement")
		}
		return nil
	}
	if err := state.Maintain(ctx, compressor, q, false); err != nil {
		t.Fatal(err)
	}
	got := working.Messages()
	snapshot := state.Snapshot()
	if commits != 1 || snapshot.State != "applied" || snapshot.Kind != memory.CompactMixed || len(got) != len(messages)+1-head-1 || got[1].Observation == nil {
		t.Fatalf("commits=%d state=%+v remaining=%d", commits, snapshot, len(got))
	}
	mu.Lock()
	count = requests
	mu.Unlock()
	if count != 1 {
		t.Fatalf("requests=%d want 1", count)
	}
	if !reflect.DeepEqual(got[2:], append(append([]memory.Message(nil), messages[5:]...), suffix)) {
		t.Fatal("unselected suffix was changed")
	}
}

func TestSourceNoGainIsNotRepaidWhenOnlyUnrelatedSuffixChanges(t *testing.T) {
	release := make(chan struct{})
	state, compressor, q, _, requests := newBackgroundMemoryTest(t, release)
	defer close(release)
	messages := q.Working.Messages()
	plan, err := state.planCandidate(q, messages, "", q.input(messages, ""), false)
	if err != nil || plan == nil {
		t.Fatalf("plan=%v err=%v", plan, err)
	}
	c := &compactCandidate{plan: plan, cancel: func() {}, state: CompactState{ID: "same-source", State: "ready"}}
	state.candidate, state.current = c, c.state
	state.finish(c, "skipped", "no_gain", nil)
	q.Working.AddMessage(memory.NewTextMessage(model.RoleUser, "unrelated pending suffix"))
	if err := state.Maintain(context.Background(), compressor, q, false); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 || state.candidate != nil {
		t.Fatal("no-gain source was repaid")
	}
}

func TestSuccessfulCompressionKeepsFixedPreparationAndPrunesObsoleteSources(t *testing.T) {
	state := NewContextMemory("")
	q := CompactRequest{Binding: testContextBinding(t), Working: memory.NewWorkingMemory(), Request: model.CompletionRequest{MaxTokens: 32000}, Coefficient: 1, Calibrated: true}
	state.excludedTools = map[string]bool{"removed-old-source": true}
	candidate := &compactCandidate{cancel: func() {}, state: CompactState{ID: "adopted", State: "ready"}}
	state.candidate, state.current = candidate, candidate.state
	if err := state.commitCandidate(context.Background(), q, candidate, 0, memory.CompactApplied{Kind: memory.CompactHistory, Summary: "updated facts"}, 50000, 20000); err != nil {
		t.Fatal(err)
	}
	if len(state.excludedTools) != 0 || !state.retryAfter.IsZero() {
		t.Fatalf("keys=%v retry=%v", state.excludedTools, state.retryAfter)
	}
}

func TestManualRejectsTinyGainAndCommitFailureNeverChangesWorkingMemory(t *testing.T) {
	for _, tt := range []struct {
		name             string
		tiny, failCommit bool
	}{
		{name: "tiny manual result", tiny: true},
		{name: "durable save failure", failCommit: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			release := make(chan struct{})
			state, compressor, q, _, requests := newBackgroundMemoryTest(t, release)
			defer close(release)
			messages := q.Working.Messages()
			plan, err := memory.PlanCompact(messages, "", memory.CompactOptions{Budget: q.budget(), TargetGain: 1000, MinimumGain: 1, InputLimit: 1000000, OutputLimit: 32000, Coefficient: 1})
			if err != nil || plan == nil {
				t.Fatalf("plan=%v err=%v", plan, err)
			}
			text := "exact evidence"
			if tt.tiny {
				text = strings.Repeat("x", 4*44999)
			}
			candidate := &compactCandidate{plan: plan, output: memory.CompactOutput{Observations: []memory.ToolObservation{{ID: plan.Groups[0].Index, Text: text}}}, done: make(chan struct{}), cancel: func() {}, state: CompactState{ID: "ready", Kind: plan.Kind, State: "ready"}}
			close(candidate.done)
			state.candidate, state.current = candidate, candidate.state
			commits := 0
			q.Commit = func(context.Context, string, []memory.Message) error {
				commits++
				if tt.failCommit {
					return fmt.Errorf("durable write rejected")
				}
				return nil
			}
			err = state.Maintain(context.Background(), compressor, q, true)
			if !reflect.DeepEqual(q.Working.Messages(), messages) || state.Summary() != "" || requests.Load() != 0 {
				t.Fatal("failed candidate changed or regenerated history")
			}
			if tt.tiny && (err != nil || commits != 0 || state.Snapshot().Reason != "no_gain") {
				t.Fatalf("err=%v commits=%d state=%+v", err, commits, state.Snapshot())
			}
			if tt.failCommit && (err == nil || commits != 1 || state.Snapshot().State != "failed") {
				t.Fatalf("err=%v commits=%d state=%+v", err, commits, state.Snapshot())
			}
		})
	}
}

func TestGenerationFailureBackoffAvoidsPerBoundaryRepayment(t *testing.T) {
	release := make(chan struct{})
	closed := false
	defer func() {
		if !closed {
			close(release)
		}
	}()
	state, compressor, q, _, requests := newBackgroundMemoryTest(t, release)
	candidate := &compactCandidate{cancel: func() {}, state: CompactState{ID: "failed", State: "preparing"}}
	state.candidate, state.current = candidate, candidate.state
	state.finish(candidate, "failed", "", fmt.Errorf("temporary failure"))
	for i := 0; i < 3; i++ {
		if err := state.Maintain(context.Background(), compressor, q, false); err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 0 || state.candidate != nil {
		t.Fatal("background failure retried before backoff")
	}
	// 模拟后续 turn 已经过退避期，不靠真实等待让测试变慢；同源失败仍可重新规划。
	state.mu.Lock()
	state.retryAfter = time.Now().Add(-time.Second)
	state.mu.Unlock()
	ready := make(chan struct{}, 1)
	state.Ready = func() { ready <- struct{}{} }
	original := q.Working.Messages()
	close(release)
	closed = true
	if err := state.Maintain(context.Background(), compressor, q, false); err != nil {
		t.Fatal(err)
	}
	awaitCompact(t, ready)
	if requests.Load() != 1 || state.Snapshot().State != "ready" || !reflect.DeepEqual(q.Working.Messages(), original) {
		t.Fatalf("requests=%d state=%+v", requests.Load(), state.Snapshot())
	}
	if err := state.Maintain(context.Background(), compressor, q, false); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 || state.Snapshot().State != "applied" {
		t.Fatalf("requests=%d state=%+v", requests.Load(), state.Snapshot())
	}
}

func TestRunnerContinuesChatAfterOneProgramSelectedHistorySummary(t *testing.T) {
	var mu sync.Mutex
	compressRequests, chatRequests := 0, 0
	committed := false
	var wireIssue string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		isCompact := len(request.Messages) == 1 && strings.Contains(request.Messages[0].Content, "\nInput:")
		text := "complete"
		mu.Lock()
		if isCompact {
			compressRequests++
			text = `{"summary":"Earlier constraint retained; old work completed.","keep":[null,999]}`
		} else {
			chatRequests++
			if !committed {
				wireIssue = "chat started before summary persistence"
			}
			joined := []string{}
			for _, m := range request.Messages {
				joined = append(joined, m.Content)
			}
			body := strings.Join(joined, "\n")
			if !strings.Contains(body, "Earlier constraint retained") || !strings.Contains(body, "pending requirement") {
				wireIssue = "chat missing summary or pending suffix"
			}
		}
		mu.Unlock()
		content, _ := json.Marshal(text)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"id\":\"offline\",\"object\":\"chat.completion.chunk\",\"model\":\"offline\",\"choices\":[{\"index\":0,\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", content)
	}))
	defer server.Close()
	binding := regressionBind(t, regressionRouter(t, server.URL))
	working := memory.NewWorkingMemory()
	var original []memory.Message
	for i := 0; i < 4; i++ {
		user := memory.NewTextMessage(model.RoleUser, fmt.Sprintf("requirement-%d", i))
		user.Origin, user.Consumed = memory.OriginUser, true
		answer := memory.NewTextMessage(model.RoleAssistant, strings.Repeat("h", 4*20000))
		answer.Consumed = true
		original = append(original, user, answer)
	}
	pending := memory.NewTextMessage(model.RoleUser, "pending requirement")
	pending.Origin = memory.OriginUser
	original = append(original, pending)
	working.SetMessages(original)
	state := NewContextMemory("old summary")
	loader, err := prompt.New()
	if err != nil {
		t.Fatal(err)
	}
	compressor := memory.NewCompressor()
	compressor.SetPrompts(loader)
	runner := Runner{Compressor: compressor, Hooks: Hooks{CommitMemory: func(_ context.Context, summary string, messages []memory.Message) error {
		if state.Summary() != "old summary" || !reflect.DeepEqual(working.Messages(), original) {
			t.Fatal("live history changed before commit")
		}
		if summary == "old summary" || !reflect.DeepEqual(messages[len(messages)-1], pending) {
			t.Fatal("invalid candidate")
		}
		mu.Lock()
		committed = true
		mu.Unlock()
		return nil
	}}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := runner.Run(ctx, Request{Binding: binding, Working: working, Memory: state, MaxTokens: 32000, AutoCompress: true, MaxTurns: 1})
	if err != nil || result.FinalText != "complete" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	mu.Lock()
	count, chat, issue := compressRequests, chatRequests, wireIssue
	mu.Unlock()
	if count != 1 || chat != 1 || issue != "" {
		t.Fatalf("compress=%d chat=%d issue=%q", count, chat, issue)
	}
	if state.Snapshot().Kind != memory.CompactHistory || state.Snapshot().State != "applied" {
		t.Fatalf("state=%+v", state.Snapshot())
	}
}

func TestCancelDuringDurableCommitWaitsForAtomicAdoption(t *testing.T) {
	state := NewContextMemory("old summary")
	working := memory.NewWorkingMemory()
	original := []memory.Message{memory.NewTextMessage(model.RoleUser, "original")}
	working.SetMessages(original)
	candidate := &compactCandidate{cancel: func() {}, state: CompactState{ID: "committing", State: "ready"}}
	state.candidate, state.current = candidate, candidate.state
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	replaced := []memory.Message{memory.NewTextMessage(model.RoleUser, "retained suffix")}
	q := CompactRequest{Binding: testContextBinding(t), Working: working, Commit: func(context.Context, string, []memory.Message) error {
		close(entered)
		<-release
		return nil
	}}
	applied := memory.CompactApplied{Kind: memory.CompactHistory, Summary: "new summary", Messages: replaced}
	committed := make(chan error, 1)
	go func() {
		committed <- state.commitCandidate(context.Background(), q, candidate, 0, applied, 10000, 5000)
	}()
	awaitCompact(t, entered)
	cancelled := make(chan struct{})
	go func() { state.Cancel(); close(cancelled) }()
	select {
	case <-cancelled:
		t.Fatal("cancel bypassed an in-progress durable commit")
	default:
	}
	if !reflect.DeepEqual(working.Messages(), original) || state.Summary() != "old summary" {
		t.Fatal("live memory changed before save completed")
	}
	unblock()
	select {
	case err := <-committed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("commit did not finish")
	}
	awaitCompact(t, cancelled)
	if !reflect.DeepEqual(working.Messages(), replaced) || state.Summary() != "new summary" || state.Snapshot().State != "applied" {
		t.Fatal("cancel rolled back durable adoption or left memory inconsistent")
	}
}
