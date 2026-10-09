package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/guard"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/prompt"
	"github.com/alanchenchen/suna/internal/runner"
	"github.com/alanchenchen/suna/internal/tools"
	"github.com/alanchenchen/suna/internal/tools/agenttools"
)

const guardTestTimeout = 5 * time.Second

type guardConcurrencyProvider struct {
	mu    sync.Mutex
	calls []string
}

func (p *guardConcurrencyProvider) Specs(context.Context) ([]tools.Spec, error) {
	return []tools.Spec{
		{Name: "exec", Category: tools.Act, Source: tools.Source{Kind: tools.SourceBuiltin, ID: "test"}},
		{Name: "readfile", Category: tools.Perceive, Source: tools.Source{Kind: tools.SourceBuiltin, ID: "test"}},
	}, nil
}

func (p *guardConcurrencyProvider) Execute(_ context.Context, call tools.Call) (tools.Result, bool) {
	p.mu.Lock()
	p.calls = append(p.calls, call.ID)
	p.mu.Unlock()
	return tools.TextResult(call.ID), true
}

func (*guardConcurrencyProvider) Close(context.Context) error { return nil }

func (p *guardConcurrencyProvider) executedIDs() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.calls...)
}

func newGuardConcurrencyAgent(t *testing.T, mode guard.Mode, reviewer guard.LLMReviewer) (*Agent, *guardConcurrencyProvider) {
	t.Helper()
	provider := &guardConcurrencyProvider{}
	manager := tools.NewManager()
	manager.RegisterProvider(provider)
	if err := manager.Reload(context.Background()); err != nil {
		t.Fatalf("加载测试工具失败：%v", err)
	}
	g := guard.NewGuardWithMode(nil, "guard-concurrency-test", mode)
	if reviewer != nil {
		g.SetLLMReviewer(reviewer)
	}
	return &Agent{tools: manager, guard: g, sessionID: "guard-concurrency-test"}, provider
}

func contextForGuardRun(runID, boundaryID string) context.Context {
	return tools.WithExecutionContext(context.Background(), tools.ExecutionContext{SessionID: "guard-concurrency-test", RunID: runID, BoundaryID: boundaryID})
}

func awaitGuardValue[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	timer := time.NewTimer(guardTestTimeout)
	defer timer.Stop()
	select {
	case value := <-ch:
		return value
	case <-timer.C:
		t.Fatalf("等待%s超时", what)
		var zero T
		return zero
	}
}

func assertGuardExecutedIDs(t *testing.T, provider *guardConcurrencyProvider, want ...string) {
	t.Helper()
	got := provider.executedIDs()
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("工具执行 ID = %v，期望 %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("工具执行 ID = %v，期望 %v", got, want)
		}
	}
}

func TestGuardSmartReviewsOverlapAndKeepEvidenceIsolated(t *testing.T) {
	entered := make(chan guard.ReviewRequest, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	reviewer := func(_ context.Context, req guard.ReviewRequest) (string, error) {
		entered <- req
		<-release
		if strings.Contains(req.Target, "MAIN_CMD") {
			return `{"decision":"approve","reason":"test approval"}`, nil
		}
		return `{"decision":"reject","reason":"test rejection"}`, nil
	}
	a, provider := newGuardConcurrencyAgent(t, guard.ModeSmart, reviewer)
	events := make(chan Event, 64)
	main := mainExecutor{agent: a, events: events}
	sub := subtaskExecutor{agent: a, events: events, allowedTools: map[string]bool{"exec": true, "readfile": true}, spawnID: "sub-1", task: "SUBTASK_TASK_ONLY"}

	type outcome struct {
		id     string
		result tools.Result
	}
	outcomes := make(chan outcome, 2)
	go func() {
		outcomes <- outcome{"main-approved", main.ExecuteTool(contextForGuardRun("review-run", "main"), runner.ToolExecution{
			ID: "main-approved", Name: "exec", Params: map[string]any{"command": "echo MAIN_CMD > main.txt"}, Intent: "MAIN_RATIONALE",
			WorkingMessages: []model.Message{model.NewTextMessage(model.RoleUser, "MAIN_EVIDENCE_ONLY")},
		})}
	}()
	go func() {
		outcomes <- outcome{"subtask-rejected", sub.ExecuteTool(contextForGuardRun("review-run", "spawn:sub-1"), runner.ToolExecution{
			ID: "subtask-rejected", Name: "exec", Params: map[string]any{"command": "echo SUBTASK_CMD > subtask.txt"}, Intent: "SUBTASK_RATIONALE",
			WorkingMessages: []model.Message{model.NewTextMessage(model.RoleUser, "SUBTASK_EVIDENCE_ONLY")},
		})}
	}()

	requests := make(map[string]guard.ReviewRequest, 2)
	for range 2 {
		req := awaitGuardValue(t, entered, "并发 Guard review")
		if strings.Contains(req.Target, "MAIN_CMD") {
			requests["main"] = req
		} else if strings.Contains(req.Target, "SUBTASK_CMD") {
			requests["subtask"] = req
		} else {
			t.Fatalf("收到未知 review 目标：%q", req.Target)
		}
	}
	if len(requests) != 2 {
		t.Fatalf("review 请求未分别覆盖主任务和子任务：%v", requests)
	}
	if !strings.Contains(requests["main"].Context.Evidence, "MAIN_EVIDENCE_ONLY") || !strings.Contains(requests["main"].Context.Evidence, "MAIN_RATIONALE") || strings.Contains(requests["main"].Context.Evidence, "SUBTASK_") {
		t.Fatalf("主任务 review 证据串用：%q", requests["main"].Context.Evidence)
	}
	if !strings.Contains(requests["subtask"].Context.Evidence, "SUBTASK_TASK_ONLY") || !strings.Contains(requests["subtask"].Context.Evidence, "SUBTASK_RATIONALE") || strings.Contains(requests["subtask"].Context.Evidence, "MAIN_") {
		t.Fatalf("子任务 review 证据串用：%q", requests["subtask"].Context.Evidence)
	}

	// 两个 review 仍被屏障挂起时，只读调用必须独立快速完成。
	fast := make(chan tools.Result, 1)
	go func() {
		fast <- main.ExecuteTool(contextForGuardRun("review-run", "main"), runner.ToolExecution{ID: "read-fast", Name: "readfile", Params: map[string]any{"path": "safe.txt"}})
	}()
	if result := awaitGuardValue(t, fast, "并发 review 期间的 readfile"); result.IsError {
		t.Fatalf("readfile 结果出错：%#v", result)
	}
	assertGuardExecutedIDs(t, provider, "read-fast")

	releaseOnce.Do(func() { close(release) })
	got := make(map[string]tools.Result, 2)
	for range 2 {
		out := awaitGuardValue(t, outcomes, "Guard 工具结果")
		got[out.id] = out.result
	}
	if got["main-approved"].IsError || !got["subtask-rejected"].IsError {
		t.Fatalf("主/子任务决策结果不匹配：%#v", got)
	}
	assertGuardExecutedIDs(t, provider, "read-fast", "main-approved")
}

func TestGuardAskConfirmationsRouteRepliesAndWaitForAllInteractions(t *testing.T) {
	a, provider := newGuardConcurrencyAgent(t, guard.ModeAsk, nil)
	const runID = "ask-run"
	a.PrepareSteering(runID)
	events := make(chan Event, 128)
	main := mainExecutor{agent: a, events: events}
	sub := subtaskExecutor{agent: a, events: events, allowedTools: map[string]bool{"exec": true}, spawnID: "sub-ask"}

	type invocation struct {
		name string
		id   string
		ctx  context.Context
		stop context.CancelFunc
	}
	calls := []invocation{}
	for _, item := range []struct {
		name, id, boundary string
	}{
		{"main-1", "main-1", "main"},
		{"main-2", "main-2", "main"},
		{"sub-1", "sub-1", "spawn:sub-ask"},
		{"sub-2", "sub-2", "spawn:sub-ask"},
	} {
		ctx, cancel := context.WithCancel(contextForGuardRun(runID, item.boundary))
		calls = append(calls, invocation{name: item.name, id: item.id, ctx: ctx, stop: cancel})
	}
	defer func() {
		for _, call := range calls {
			call.stop()
		}
	}()
	type outcome struct {
		name   string
		result tools.Result
	}
	outcomes := make(chan outcome, len(calls))
	for _, call := range calls {
		go func() {
			toolCall := runner.ToolExecution{ID: call.id, Name: "exec", Params: map[string]any{"command": "echo " + call.id + " > " + call.id + ".txt"}}
			var result tools.Result
			if strings.HasPrefix(call.name, "sub-") {
				result = sub.ExecuteTool(call.ctx, toolCall)
			} else {
				result = main.ExecuteTool(call.ctx, toolCall)
			}
			outcomes <- outcome{name: call.name, result: result}
		}()
	}

	confirmations := make(map[string]Event, len(calls))
	timer := time.NewTimer(guardTestTimeout)
	defer timer.Stop()
	for len(confirmations) < len(calls) {
		select {
		case event := <-events:
			if event.Type == EventGuardConfirm {
				confirmations[event.GuardToolCallID] = event
			}
		case <-timer.C:
			t.Fatalf("未等到全部 GuardConfirm 事件，收到 %d/%d", len(confirmations), len(calls))
		}
	}
	for _, id := range []string{"main-1", "main-2", "spawn:sub-ask:sub-1", "spawn:sub-ask:sub-2"} {
		if confirmations[id].Reply == nil {
			t.Fatalf("缺少确认事件 %q：%#v", id, confirmations)
		}
	}
	if _, _, err := a.EnqueueSteering(runID, "before-replies", "hold while confirming"); err == nil || err.Error() != "interaction_pending" {
		t.Fatalf("全部确认挂起时 steering 错误 = %v，期望 interaction_pending", err)
	}

	// 按与事件到达顺序不同的顺序答复，验证每个 Reply 只影响对应调用。
	confirmations["spawn:sub-ask:sub-1"].Reply <- "reject"
	confirmations["main-2"].Reply <- "approve"
	confirmations["main-1"].Reply <- "approve"
	finished := make(map[string]tools.Result, 3)
	for range 3 {
		out := awaitGuardValue(t, outcomes, "已答复确认")
		finished[out.name] = out.result
	}
	if finished["main-1"].IsError || finished["main-2"].IsError || !finished["sub-1"].IsError {
		t.Fatalf("乱序答复未路由到对应调用：%#v", finished)
	}
	if _, _, err := a.EnqueueSteering(runID, "one-left", "still pending"); err == nil || err.Error() != "interaction_pending" {
		t.Fatalf("仍有一个确认挂起时 steering 错误 = %v，期望 interaction_pending", err)
	}
	assertGuardExecutedIDs(t, provider, "main-1", "main-2")

	calls[3].stop()
	last := awaitGuardValue(t, outcomes, "取消剩余确认")
	if last.name != "sub-2" || !last.result.IsError {
		t.Fatalf("取消剩余确认结果 = %#v，期望 sub-2 失败", last)
	}
	assertGuardExecutedIDs(t, provider, "main-1", "main-2")
	if _, _, err := a.EnqueueSteering(runID, "after-all", "all interactions finished"); err != nil {
		t.Fatalf("全部交互结束后 steering 仍被阻塞：%v", err)
	}
}

func TestGuardCancellationPreventsExecutionInMainAndSubtask(t *testing.T) {
	for _, duringReview := range []bool{false, true} {
		for _, isSubtask := range []bool{false, true} {
			name := "main"
			if isSubtask {
				name = "subtask"
			}
			if duringReview {
				name += "-cancel-during-review"
			} else {
				name += "-cancel-before-call"
			}
			t.Run(name, func(t *testing.T) {
				entered := make(chan struct{}, 1)
				release := make(chan struct{})
				var releaseOnce sync.Once
				defer releaseOnce.Do(func() { close(release) })
				var reviewer guard.LLMReviewer
				if duringReview {
					reviewer = func(context.Context, guard.ReviewRequest) (string, error) {
						entered <- struct{}{}
						<-release
						// 故意在取消后仍返回批准，模拟不响应取消的 review 回调。
						return `{"decision":"approve","reason":"approved after cancellation"}`, nil
					}
				} else {
					reviewer = func(context.Context, guard.ReviewRequest) (string, error) {
						return `{"decision":"approve"}`, nil
					}
				}
				a, provider := newGuardConcurrencyAgent(t, guard.ModeSmart, reviewer)
				events := make(chan Event, 16)
				ctx, cancel := context.WithCancel(contextForGuardRun("cancel-run", "main"))
				defer cancel()
				if !duringReview {
					cancel()
				}
				call := runner.ToolExecution{ID: "must-not-execute", Name: "exec", Params: map[string]any{"command": "echo cancellation-test > cancellation.txt"}}
				var result tools.Result
				done := make(chan tools.Result, 1)
				if isSubtask {
					executor := subtaskExecutor{agent: a, events: events, allowedTools: map[string]bool{"exec": true}, spawnID: "cancel-sub"}
					go func() { done <- executor.ExecuteTool(ctx, call) }()
				} else {
					executor := mainExecutor{agent: a, events: events}
					go func() { done <- executor.ExecuteTool(ctx, call) }()
				}
				if duringReview {
					awaitGuardValue(t, entered, "Guard review 开始")
					cancel()
					releaseOnce.Do(func() { close(release) })
				}
				result = awaitGuardValue(t, done, "取消后的工具结果")
				if !result.IsError {
					t.Fatalf("取消后的工具结果未标记失败：%#v", result)
				}
				assertGuardExecutedIDs(t, provider)
			})
		}
	}
}

func TestGuardAskUserAndSkillInteractionsReleaseSteeringIndependently(t *testing.T) {
	a, _ := newGuardConcurrencyAgent(t, guard.ModeAsk, nil)
	const runID = "mixed-interactions-run"
	a.PrepareSteering(runID)
	events := make(chan Event, 32)

	confirmCtx, cancelConfirm := context.WithCancel(contextForGuardRun(runID, "main"))
	askCtx, cancelAsk := context.WithCancel(contextForGuardRun(runID, "main"))
	skillCtx, cancelSkill := context.WithCancel(contextForGuardRun(runID, "main"))
	defer cancelConfirm()
	defer cancelAsk()
	defer cancelSkill()
	type outcome struct {
		name   string
		answer string
		result tools.Result
		err    error
	}
	outcomes := make(chan outcome, 3)
	go func() {
		ok := a.confirmGuard(confirmCtx, "confirm-id", runner.ToolExecution{ID: "confirm-id", Name: "exec"}, &guard.GuardResult{Decision: guard.Confirm}, events)
		outcomes <- outcome{name: "confirm", answer: map[bool]string{true: "approve", false: "reject"}[ok]}
	}()
	go func() {
		ctx := agenttools.WithEvents(askCtx, (chan<- Event)(events))
		result := a.ExecuteAskUserTool(ctx, map[string]any{"question": "ask-question"})
		outcomes <- outcome{name: "askuser", result: result}
	}()
	go func() {
		ctx := contextWithSkillRuntime(skillCtx, a, events)
		answer, err := (agentSkillPrompter{}).AskChoice(ctx, "skill-question", []string{"yes", "no"})
		outcomes <- outcome{name: "skill", answer: answer, err: err}
	}()

	replies := make(map[string]chan string, 3)
	timer := time.NewTimer(guardTestTimeout)
	defer timer.Stop()
	for len(replies) < 3 {
		select {
		case event := <-events:
			switch event.Type {
			case EventGuardConfirm:
				replies["confirm"] = event.Reply
			case EventAskUser:
				if event.Question == "ask-question" {
					replies["askuser"] = event.Reply
				} else if event.Question == "skill-question" {
					replies["skill"] = event.Reply
				}
			}
		case out := <-outcomes:
			t.Fatalf("交互事件发出前调用已结束：%#v；已收到回复通道：%v", out, replies)
		case <-timer.C:
			t.Fatalf("未收到 GuardConfirm、AskUser、Skill 全部交互事件：%v", replies)
		}
	}
	if _, _, err := a.EnqueueSteering(runID, "mixed-before", "all three pending"); err == nil || err.Error() != "interaction_pending" {
		t.Fatalf("三类交互挂起时 steering 错误 = %v，期望 interaction_pending", err)
	}

	// 先完成 Skill 与 GuardConfirm，AskUser 仍挂起时 mailbox 不能提前解锁。
	replies["skill"] <- "skill-answer"
	replies["confirm"] <- "approve"
	finished := make(map[string]outcome, 2)
	for range 2 {
		out := awaitGuardValue(t, outcomes, "已回复的混合交互")
		finished[out.name] = out
	}
	if finished["skill"].err != nil || finished["skill"].answer != "skill-answer" || finished["confirm"].answer != "approve" {
		t.Fatalf("Skill/GuardConfirm 回复错配：%#v", finished)
	}
	if _, _, err := a.EnqueueSteering(runID, "mixed-one-left", "askuser still pending"); err == nil || err.Error() != "interaction_pending" {
		t.Fatalf("AskUser 仍挂起时 steering 错误 = %v，期望 interaction_pending", err)
	}
	cancelAsk()
	last := awaitGuardValue(t, outcomes, "取消的 AskUser")
	if last.name != "askuser" || !last.result.IsError {
		t.Fatalf("取消 AskUser 结果 = %#v，期望失败", last)
	}
	if _, _, err := a.EnqueueSteering(runID, "mixed-after", "all three finished"); err != nil {
		t.Fatalf("三类交互结束后 steering 仍被阻塞：%v", err)
	}
}

var _ tools.Provider = (*guardConcurrencyProvider)(nil)

func TestConcurrentGuardReviewsUseRealBindingAndPrompt(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		// 仅用本地假服务走完整模板、Binding、限流、SDK 与日志链路。
		content, _ := json.Marshal(`{"decision":"approve","reason":"local test"}`)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":%s}}]}\n\ndata: [DONE]\n\n", content)
	}))
	defer upstream.Close()
	// 测试失败时先释放 handler，再关闭 server，避免等待未结束的连接。
	defer releaseOnce.Do(func() { close(release) })
	router, err := model.NewRouter(&config.Config{Models: []config.ModelConfig{{
		Provider: "example-provider", Model: "test-model", Protocol: config.ModelProtocolOpenAIChat,
		BaseURL: upstream.URL + "/v1", APIKey: "test-api-key", ContextWindow: 128000, MaxOutputTokens: 8192,
	}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := router.Bind("example-provider/test-model")
	if err != nil {
		t.Fatal(err)
	}
	a, provider := newGuardConcurrencyAgent(t, guard.ModeSmart, nil)
	a.prompts, err = prompt.New()
	if err != nil {
		t.Fatal(err)
	}
	a.guard.SetLLMReviewer(a.guardLLMReview)
	ctx, cancel := context.WithCancel(model.WithBinding(contextForGuardRun("real-review", "main"), binding))
	defer cancel()
	done := make(chan tools.Result, 2)
	for _, id := range []string{"first", "second"} {
		go func() {
			done <- a.executeTool(ctx, runner.ToolExecution{ID: id, Name: "exec", Params: map[string]any{"action": "stop", "job_id": id}}, make(chan Event, 4))
		}()
	}
	for range 2 {
		awaitGuardValue(t, entered, "本地服务并发审核请求")
	}
	assertGuardExecutedIDs(t, provider)
	releaseOnce.Do(func() { close(release) })
	for range 2 {
		if result := awaitGuardValue(t, done, "完整调用链审核结果"); result.IsError {
			t.Fatalf("review result = %#v, want success", result)
		}
	}
	assertGuardExecutedIDs(t, provider, "first", "second")
}
