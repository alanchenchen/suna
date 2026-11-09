package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/prompt"
)

func regressionRouter(t *testing.T, url string) *model.Router {
	t.Helper()
	router, err := model.NewRouter(&config.Config{MaxModelRPS: 100, Models: []config.ModelConfig{{
		Provider: "local", Protocol: config.ModelProtocolOpenAIChat, Model: "offline", BaseURL: url,
		APIKey: "offline", ContextWindow: 100000, MaxOutputTokens: 32000,
	}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return router
}

func regressionBind(t *testing.T, router *model.Router) *model.ModelBinding {
	t.Helper()
	binding, err := router.Bind("local/offline")
	if err != nil {
		t.Fatal(err)
	}
	return binding
}

func TestContextMemoryRebindOnlyCancelsDifferentRoute(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	defer server.Close()
	router := regressionRouter(t, server.URL)
	first, again := regressionBind(t, router), regressionBind(t, router)
	rebuilt := regressionBind(t, regressionRouter(t, server.URL))
	if first == again || !reflect.DeepEqual(first.Config(), rebuilt.Config()) || first.Ref() != rebuilt.Ref() {
		t.Fatal("测试前提不成立：快照须独立，重建路由须同 ref/config")
	}

	state := NewContextMemory("")
	state.Bind(first)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	candidate := &compactCandidate{cancel: cancel, state: CompactState{ID: "candidate", State: "preparing"}, started: time.Now()}
	state.candidate, state.current = candidate, candidate.state
	state.Bind(again)
	select {
	case <-ctx.Done():
		t.Fatal("同一 Router 的新 binding 取消了候选")
	default:
	}
	if state.candidate != candidate || state.Snapshot().State != "preparing" {
		t.Fatal("同一路由不应移除候选")
	}

	state.Bind(rebuilt)
	select {
	case <-ctx.Done():
	default:
		t.Fatal("重建 Router 后未取消旧候选")
	}
	if state.candidate != nil || state.Snapshot().State != "cancelled" || requests.Load() != 0 {
		t.Fatalf("candidate=%v state=%+v requests=%d", state.candidate, state.Snapshot(), requests.Load())
	}
}

func TestReadyOnlyHardAppliesWithoutWaitingOrStartingMore(t *testing.T) {
	release := make(chan struct{})
	state, compressor, q, started, requests := newBackgroundMemoryTest(t, release)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	ready := make(chan struct{}, 1)
	var waits, commits atomic.Int32
	state.Notify = func(s CompactState) {
		if s.Wait != "" {
			waits.Add(1)
		}
	}
	state.Ready = func() {
		select {
		case ready <- struct{}{}:
		default:
		}
	}
	q.Commit = func(context.Context, string, []memory.Message) error { commits.Add(1); return nil }
	if err := state.Maintain(context.Background(), compressor, q, false); err != nil {
		t.Fatal(err)
	}
	awaitCompact(t, started)
	unblock()
	awaitCompact(t, ready)

	// 采用后仍保持硬缺口，验证空闲回调不会串联第二笔请求。
	pending := memory.NewTextMessage(model.RoleUser, strings.Repeat("p", 4*70000))
	pending.Origin = memory.OriginUser
	q.Working.AddMessage(pending)
	if before := q.input(q.Working.Messages(), state.Summary()); before < q.budget() {
		t.Fatalf("before=%d budget=%d，测试未进入硬缺口", before, q.budget())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := state.ApplyReady(ctx, compressor); err != nil {
		t.Fatal(err)
	}
	after := q.input(q.Working.Messages(), state.Summary())
	got := q.Working.Messages()
	if after < q.budget() || state.Snapshot().State != "applied" || commits.Load() != 1 ||
		waits.Load() != 0 || requests.Load() != 1 || len(got[1].ToolCalls) != 0 || got[len(got)-1].Text() != pending.Text() {
		t.Fatalf("after=%d budget=%d state=%+v commits=%d waits=%d requests=%d", after, q.budget(), state.Snapshot(), commits.Load(), waits.Load(), requests.Load())
	}
}

func TestHardLimitAtExactBudgetWaitsForInflight(t *testing.T) {
	release := make(chan struct{})
	state, compressor, q, started, requests := newBackgroundMemoryTest(t, release)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	waiting := make(chan struct{}, 1)
	state.Notify = func(s CompactState) {
		if s.Wait == "hard_limit" {
			select {
			case waiting <- struct{}{}:
			default:
			}
		}
	}
	if err := state.Maintain(context.Background(), compressor, q, false); err != nil {
		t.Fatal(err)
	}
	awaitCompact(t, started)

	// 单字节 ASCII 的估算为 len/4+1，精确填到 B 而不是依赖近似比例。
	missing := q.budget() - q.input(q.Working.Messages(), state.Summary())
	if missing < 2 {
		t.Fatalf("测试前提不成立：剩余预算 %d", missing)
	}
	filler := memory.NewTextMessage(model.RoleUser, strings.Repeat("x", 4*(missing-1)))
	filler.Origin = memory.OriginUser
	// 未消费用户后缀阻止旧的 >B 实现误以为普通 ready 候选可直接采用。
	q.Working.AddMessage(filler)
	if before := q.input(q.Working.Messages(), state.Summary()); before != q.budget() {
		t.Fatalf("before=%d budget=%d，未精确命中边界", before, q.budget())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- state.Maintain(ctx, compressor, q, false) }()
	select {
	case <-waiting:
	case err := <-done:
		t.Fatalf("达到 B 时未等待候选：%v", err)
	case <-ctx.Done():
		t.Fatal("未进入 hard_limit 等待")
	}
	select {
	case err := <-done:
		t.Fatalf("候选未完成时提前返回：%v", err)
	default:
	}
	if requests.Load() != 1 {
		t.Fatalf("等待期间请求数=%d，期望复用唯一候选", requests.Load())
	}
	unblock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("释放候选后仍未完成")
	}
	if requests.Load() != 1 || state.Snapshot().State != "applied" || q.input(q.Working.Messages(), state.Summary()) >= q.budget() {
		t.Fatalf("requests=%d state=%+v", requests.Load(), state.Snapshot())
	}
}

func TestNormalMaintenanceIgnoresFinishedBackgroundFailure(t *testing.T) {
	release := make(chan struct{})
	state, _, q, _, requests := newBackgroundMemoryTest(t, release)
	t.Cleanup(func() { close(release) })
	compressor := memory.NewCompressor() // 无模板时本地生成直接失败，不访问网络。
	want := errors.New("original background error")
	stopped, proceed, actualDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(proceed) }) })
	var candidate *compactCandidate
	original := q.Working.Messages()
	state.Notify = func(s CompactState) {
		if s.State != "preparing" || s.Wait != "" {
			return
		}
		// preparing 在启动生成协程之前同步通知；模拟 done 已关闭且 finish 已清候选。
		state.mu.Lock()
		candidate = state.candidate
		cancel := candidate.cancel
		candidate.cancel = func() {
			close(stopped)
			<-proceed
			// 即使断言失败触发清理，也先替换关闭过的通道再放行真实生成协程。
			state.mu.Lock()
			candidate.done = actualDone
			state.mu.Unlock()
			cancel()
		}
		candidate.err = want
		close(candidate.done)
		state.mu.Unlock()
		state.finish(candidate, "failed", "", want)
	}
	if err := state.Maintain(context.Background(), compressor, q, false); err != nil {
		t.Fatalf("可选后台失败不应终止主任务：%v", err)
	}
	if candidate == nil || state.Snapshot().State != "failed" || !reflect.DeepEqual(original, q.Working.Messages()) {
		t.Fatalf("后台失败未保留原历史：%+v", state.Snapshot())
	}
	awaitCompact(t, stopped)
	// 真正的生成协程在 cancel 处受控暂停，放行后会自行替换已关闭通道。
	once.Do(func() { close(proceed) })
	awaitCompact(t, actualDone)
	if requests.Load() != 0 {
		t.Fatalf("本地失败发出了 %d 笔网络请求", requests.Load())
	}
}

func TestHardRejectsReadyCandidateWithoutActualGain(t *testing.T) {
	release := make(chan struct{})
	state, compressor, q, _, requests := newBackgroundMemoryTest(t, release)
	t.Cleanup(func() { close(release) })
	pending := memory.NewTextMessage(model.RoleUser, strings.Repeat("p", 4*30000))
	pending.Origin = memory.OriginUser
	q.Working.AddMessage(pending)
	messages := q.Working.Messages()
	if q.input(messages, state.Summary()) < q.budget() {
		t.Fatal("测试未进入硬缺口")
	}
	plan, err := memory.PlanCompact(messages, "", memory.CompactTools, q.budget(), 1000000, 32000, 1, true)
	if err != nil || plan == nil || len(plan.Groups) != 1 {
		t.Fatalf("plan=%+v error=%v", plan, err)
	}
	candidate := &compactCandidate{
		plan: plan, output: memory.CompactOutput{Observations: []memory.ToolObservation{{ID: plan.Groups[0].Index, Keep: true}}},
		done: make(chan struct{}), cancel: func() {}, started: time.Now(),
		state: CompactState{ID: "no-gain", State: "ready", Kind: memory.CompactTools},
	}
	close(candidate.done)
	state.candidate, state.current = candidate, candidate.state
	commits := 0
	q.Commit = func(context.Context, string, []memory.Message) error { commits++; return nil }
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = state.Maintain(ctx, compressor, q, false)
	if err == nil || state.Snapshot().Reason != "no_gain" || commits != 0 || requests.Load() != 0 || !reflect.DeepEqual(messages, q.Working.Messages()) {
		t.Fatalf("error=%v state=%+v commits=%d requests=%d，零收益不应采用或续费", err, state.Snapshot(), commits, requests.Load())
	}
}

func TestHardWaitReturnsOriginalFinishedFailure(t *testing.T) {
	release := make(chan struct{})
	state, compressor, q, _, requests := newBackgroundMemoryTest(t, release)
	t.Cleanup(func() { close(release) })
	q.Working.AddMessage(memory.NewTextMessage(model.RoleUser, strings.Repeat("p", 4*30000)))
	if q.input(q.Working.Messages(), state.Summary()) < q.budget() {
		t.Fatal("测试未进入硬缺口")
	}
	original := q.Working.Messages()
	want := errors.New("original hard generation error")
	candidate := &compactCandidate{
		plan: &memory.CompactPlan{Kind: memory.CompactTools}, done: make(chan struct{}), cancel: func() {},
		state: CompactState{ID: "existing", State: "preparing", Kind: memory.CompactTools}, started: time.Now(),
	}
	state.candidate, state.current = candidate, candidate.state
	state.Notify = func(s CompactState) {
		if s.Wait != "hard_limit" {
			return
		}
		// 在等待通知中先完成并清除候选，再让等待者检查 done，固定复现竞态。
		state.mu.Lock()
		candidate.err = want
		close(candidate.done)
		state.mu.Unlock()
		state.finish(candidate, "failed", "", want)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := state.Maintain(ctx, compressor, q, false)
	if !errors.Is(err, want) || !reflect.DeepEqual(original, q.Working.Messages()) || requests.Load() != 0 {
		t.Fatalf("error=%v state=%+v requests=%d，期望保留原错误和历史", err, state.Snapshot(), requests.Load())
	}
}

// 本机返回的观察逐条对应请求内编号，并记录源消息，避免把动态编号当作稳定来源身份。
type regressionCompactBatch struct {
	Summary string
	Sources []string
}

func regressionCompactServer(t *testing.T) (*httptest.Server, func() []regressionCompactBatch) {
	t.Helper()
	var batches []regressionCompactBatch
	var batchMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Messages) != 1 {
			http.Error(w, fmt.Sprintf("invalid compact request: %v", err), http.StatusBadRequest)
			return
		}
		// Windows 检出的内嵌模板可能使用 CRLF；测试解析不能依赖模板文件的换行风格。
		promptText := strings.ReplaceAll(req.Messages[0].Content, "\r\n", "\n")
		parts := strings.SplitN(promptText, "\nInput:\n", 2)
		if len(parts) != 2 {
			http.Error(w, "missing input", http.StatusBadRequest)
			return
		}
		var payload struct {
			Summary string             `json:"previous_summary"`
			Groups  []memory.ToolGroup `json:"groups"`
			History []memory.Message   `json:"history"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(parts[1])), &payload); err != nil {
			http.Error(w, fmt.Sprintf("invalid compact payload: %v", err), http.StatusBadRequest)
			return
		}
		batch := regressionCompactBatch{Summary: payload.Summary}
		output := memory.CompactOutput{}
		for i, msg := range payload.History {
			batch.Sources = append(batch.Sources, msg.Text())
			if memory.IsUserMessage(msg) && msg.Origin != memory.OriginLegacy {
				output.Keep = append(output.Keep, i)
			}
		}
		if len(payload.History) > 0 {
			output.Summary = payload.Summary + fmt.Sprintf("|merged-%d", len(payload.History))
		}
		for _, group := range payload.Groups {
			if len(group.Messages) == 0 || len(group.Messages[0].ToolCalls) != 1 {
				http.Error(w, "invalid tool source", http.StatusBadRequest)
				return
			}
			id := group.Messages[0].ToolCalls[0].ID
			batch.Sources = append(batch.Sources, id)
			observation := memory.ToolObservation{ID: group.Index, Text: "evidence " + id}
			batchMu.Lock()
			firstBatch := len(batches) == 0
			batchMu.Unlock()
			if id == "source-0" && firstBatch && len(payload.Groups) >= 2 {
				// 首批保留一组、压缩另一组；后续不能为 keep 再次付费。
				observation.Keep, observation.Text = true, ""
			}
			output.Observations = append(output.Observations, observation)
		}
		if len(batch.Sources) == 0 {
			http.Error(w, "missing compact sources", http.StatusBadRequest)
			return
		}
		batchMu.Lock()
		batches = append(batches, batch)
		batchMu.Unlock()
		encoded, _ := json.Marshal(output)
		content, _ := json.Marshal(string(encoded))
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"id\":\"local\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"offline\",\"choices\":[{\"index\":0,\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", content)
	}))
	t.Cleanup(server.Close)
	return server, func() []regressionCompactBatch {
		batchMu.Lock()
		defer batchMu.Unlock()
		return append([]regressionCompactBatch(nil), batches...)
	}
}

func TestHardLimitContinuesAcrossMoreThanTwoFreshToolBatches(t *testing.T) {
	server, snapshot := regressionCompactServer(t)
	loader, err := prompt.New()
	if err != nil {
		t.Fatal(err)
	}
	compressor := memory.NewCompressor()
	compressor.SetPrompts(loader)
	working := memory.NewWorkingMemory()
	user := memory.NewTextMessage(model.RoleUser, "inspect evidence")
	user.Origin, user.Consumed = memory.OriginUser, true
	messages := []memory.Message{user}
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("source-%d", i)
		messages = append(messages,
			memory.Message{Role: model.RoleAssistant, TextContent: "prose " + id, Consumed: true, ToolCalls: []memory.ToolCall{{ID: id, Name: "readfile", Arguments: `{}`}}},
			memory.Message{Role: model.RoleTool, ToolCallID: id, TextContent: strings.Repeat("x", 4*24000), ToolStatus: "completed", Consumed: true},
		)
	}
	tail := memory.NewTextMessage(model.RoleUser, strings.Repeat("t", 4*7000))
	tail.Origin, tail.Consumed = memory.OriginUser, true
	messages = append(messages, memory.Message{Role: model.RoleAssistant, TextContent: "explained", Consumed: true}, tail)
	working.SetMessages(messages)
	const oldSummary = "previous compacted facts"
	state := NewContextMemory(oldSummary)
	q := CompactRequest{Binding: regressionBind(t, regressionRouter(t, server.URL)), Working: working,
		Request: model.CompletionRequest{MaxTokens: 32000}, Coefficient: 1, Calibrated: true}
	if q.input(messages, oldSummary) < q.budget() {
		t.Fatal("测试未进入硬缺口")
	}
	var appliedMu sync.Mutex
	var beforeAfter [][2]int
	state.Notify = func(s CompactState) {
		if s.State == "applied" {
			appliedMu.Lock()
			beforeAfter = append(beforeAfter, [2]int{s.BeforeTokens, s.AfterTokens})
			appliedMu.Unlock()
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := state.Maintain(ctx, compressor, q, false); err != nil {
		t.Fatal(err)
	}
	gotBatches := snapshot()
	appliedMu.Lock()
	gotBeforeAfter := append([][2]int(nil), beforeAfter...)
	appliedMu.Unlock()
	if len(gotBatches) < 3 || len(gotBeforeAfter) != len(gotBatches) || len(gotBatches[0].Sources) < 2 || gotBatches[0].Sources[0] != "source-0" {
		t.Fatalf("batches=%d commits=%d，期望超过两批", len(gotBatches), len(gotBeforeAfter))
	}
	seen := make(map[string]bool)
	for i, batch := range gotBatches {
		if batch.Summary != oldSummary || len(batch.Sources) == 0 || gotBeforeAfter[i][1] >= gotBeforeAfter[i][0] {
			t.Fatalf("batch %d = %+v before/after=%v", i, batch, gotBeforeAfter[i])
		}
		for _, id := range batch.Sources {
			if seen[id] {
				t.Fatalf("来源 %s 被重复提交", id)
			}
			seen[id] = true
		}
	}
	if state.Summary() != oldSummary || q.input(working.Messages(), state.Summary()) >= q.budget() {
		t.Fatalf("summary=%q，最终仍超预算", state.Summary())
	}
	got := working.Messages()
	if len(seen) != 8 || len(got) != len(messages)-7 || len(got[1].ToolCalls) != 1 || got[2].ToolCallID != "source-0" {
		t.Fatalf("处理来源=%v，最终消息=%d，期望保留第一组、压缩其余七组", seen, len(got))
	}
}

func TestHardHistoryContinuesWithOnlyFreshSourceAndPreservesOriginals(t *testing.T) {
	// 与工具批次共用本机服务端，解析实际 prompt 并返回原话索引。
	server, snapshot := regressionCompactServer(t)
	loader, err := prompt.New()
	if err != nil {
		t.Fatal(err)
	}
	compressor := memory.NewCompressor()
	compressor.SetPrompts(loader)
	working := memory.NewWorkingMemory()
	var messages []memory.Message
	var originals []string
	for i := 0; i < 6; i++ {
		original := fmt.Sprintf("original-%d", i)
		user := memory.NewTextMessage(model.RoleUser, original)
		user.Origin, user.Consumed = memory.OriginUser, true
		answer := memory.NewTextMessage(model.RoleAssistant, fmt.Sprintf("answer-%d:", i)+strings.Repeat("x", 4*40000))
		answer.Consumed = true
		messages = append(messages, user, answer)
		originals = append(originals, original)
	}
	tail := memory.NewTextMessage(model.RoleUser, "pending requirement")
	tail.Origin = memory.OriginUser
	messages = append(messages, tail)
	working.SetMessages(messages)
	const oldSummary = "earlier facts"
	state := NewContextMemory(oldSummary)
	q := CompactRequest{Binding: regressionBind(t, regressionRouter(t, server.URL)), Working: working,
		Request: model.CompletionRequest{MaxTokens: 32000}, Coefficient: 1, Calibrated: true}
	if q.input(messages, oldSummary) < q.budget() {
		t.Fatal("测试未进入硬缺口")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := state.Maintain(ctx, compressor, q, false); err != nil {
		t.Fatal(err)
	}
	gotBatches := snapshot()
	if len(gotBatches) < 3 {
		t.Fatalf("history batches=%d，期望超过两批", len(gotBatches))
	}
	seen := map[string]bool{}
	summary := oldSummary
	for i, batch := range gotBatches {
		if batch.Summary != summary || len(batch.Sources) == 0 {
			t.Fatalf("batch %d 的摘要或来源错误", i)
		}
		for _, text := range batch.Sources {
			if strings.Contains(text, "Historical tool observation") || seen[text] {
				t.Fatalf("已处理的历史来源被重复归并：%.40s", text)
			}
			seen[text] = true
		}
		summary += fmt.Sprintf("|merged-%d", len(batch.Sources))
	}
	if state.Summary() != summary || q.input(working.Messages(), state.Summary()) >= q.budget() {
		t.Fatalf("summary=%q，期望 %q 且最终不超预算", state.Summary(), summary)
	}
	var gotOriginals []string
	for _, msg := range working.Messages() {
		if memory.IsUserMessage(msg) {
			gotOriginals = append(gotOriginals, msg.Text())
		}
	}
	originals = append(originals, tail.Text())
	if !reflect.DeepEqual(gotOriginals, originals) {
		t.Fatalf("原话丢失或重排：%v", gotOriginals)
	}
}

func TestCompactNotificationsDiscardLateProgressAfterTerminal(t *testing.T) {
	state := NewContextMemory("")
	var received []string
	state.Notify = func(s CompactState) { received = append(received, s.State) }
	ready := CompactState{ID: "one", State: "ready", Wait: "hard_limit"}
	state.current = CompactState{ID: "one", State: "applied"}
	state.emit(state.current)
	state.emit(ready)
	state.current = CompactState{ID: "two", State: "preparing"}
	state.emit(ready)
	if !reflect.DeepEqual(received, []string{"applied"}) {
		t.Fatalf("published states = %v, want only applied", received)
	}
}

func TestHardSlotWaitPublishesSameIDAndCancellationElapsed(t *testing.T) {
	release := make(chan struct{})
	state, compressor, q, _, requests := newBackgroundMemoryTest(t, release)
	t.Cleanup(func() { close(release) })
	if !compressor.TryBackground() {
		t.Fatal("cannot reserve test slot")
	}
	defer compressor.ReleaseBackground()
	q.Working.AddMessage(memory.NewTextMessage(model.RoleUser, strings.Repeat("x", 4*30000)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waiting := make(chan CompactState, 1)
	state.Notify = func(s CompactState) {
		if s.State == "preparing" && s.Wait == "hard_limit" {
			waiting <- s
		}
	}
	done := make(chan error, 1)
	go func() { done <- state.Maintain(ctx, compressor, q, false) }()
	var first CompactState
	select {
	case first = <-waiting:
	case <-time.After(5 * time.Second):
		t.Fatal("slot wait was invisible")
	}
	// 固定已等待一秒，避免立即取消时依赖 Windows 的计时粒度，也无需 Sleep。
	state.mu.Lock()
	state.candidate.waitStarted = state.candidate.waitStarted.Add(-time.Second)
	state.mu.Unlock()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("slot wait did not cancel")
	}
	terminal := state.Snapshot()
	if first.ID == "" || terminal.ID != first.ID || terminal.State != "cancelled" || terminal.WaitElapsed < time.Second || terminal.PrepareElapsed != 0 || requests.Load() != 0 {
		t.Fatalf("waiting = %+v, terminal = %+v, requests = %d", first, terminal, requests.Load())
	}
}
