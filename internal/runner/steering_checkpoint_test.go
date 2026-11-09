package runner

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
)

func TestRunnerAbsorbsSteeringAfterHardWaitAndRechecksBudgetBeforeTools(t *testing.T) {
	h := newCheckpointHarness(t, false)
	state := NewContextMemory("")
	h.request.Memory = state
	h.request.AutoCompress = true
	h.request.MaxTurns = 1
	h.runner.Compressor = memory.NewCompressor()
	budget := (CompactRequest{Binding: h.request.Binding, Request: model.CompletionRequest{MaxTokens: 1024}}).budget()
	messages := []memory.Message{
		{Role: model.RoleUser, Origin: memory.OriginUser, TextContent: "inspect source", Consumed: true},
		{Role: model.RoleAssistant, TextContent: "original explanation", Consumed: true, ToolCalls: []memory.ToolCall{{ID: "old-call", Name: "readfile", Arguments: `{}`}}},
		{Role: model.RoleTool, ToolCallID: "old-call", TextContent: strings.Repeat("e", budget*4*65/100), ToolStatus: "completed", Consumed: true},
		{Role: model.RoleAssistant, TextContent: "evidence explained", Consumed: true},
		{Role: model.RoleUser, Origin: memory.OriginUser, TextContent: strings.Repeat("t", budget*4*15/100), Consumed: true},
		{Role: model.RoleAssistant, TextContent: "recent answer", Consumed: true},
		{Role: model.RoleUser, Origin: memory.OriginUser, TextContent: strings.Repeat("p", budget*4*25/100)},
	}
	h.working.SetMessages(messages)
	plan, err := memory.PlanCompact(messages, "", memory.CompactTools, budget, 1000000, 32000, 1, true)
	if err != nil || plan == nil || len(plan.Groups) != 1 {
		t.Fatalf("plan = %+v/%v, want one complete tool group", plan, err)
	}
	candidate := &compactCandidate{
		plan: plan, output: memory.CompactOutput{Observations: []memory.ToolObservation{{ID: 1, Text: "saved evidence"}}},
		done: make(chan struct{}), cancel: func() {}, started: time.Now(),
		state: CompactState{ID: "existing", Kind: memory.CompactTools, State: "preparing"},
	}
	state.candidate = candidate
	state.current = candidate.state
	waiting := make(chan struct{}, 1)
	state.Notify = func(s CompactState) {
		if s.Wait == "hard_limit" {
			select {
			case waiting <- struct{}{}:
			default:
			}
		}
	}
	correction := "use the corrected requirement before executing tools"
	queue := make(chan SteeringInput, 1)
	h.request.TakeSteering = func(bool) []SteeringInput {
		select {
		case input := <-queue:
			return []SteeringInput{input}
		default:
			return nil
		}
	}
	projects, correctionProjections, applied, saved := 0, 0, 0, false
	h.request.Project = func(context.Context) func([]memory.Message) []model.Message {
		projects++
		return func(messages []memory.Message) []model.Message {
			if len(messages) > 0 && messages[len(messages)-1].Text() == correction {
				correctionProjections++
			}
			// 保持实际请求背景参与每次预算比较，而不是只估裸 WorkingMemory。
			return append([]model.Message{model.NewTextMessage(model.RoleUser, "fixed request background")}, memory.ModelMessages(messages)...)
		}
	}
	h.runner.Hooks.Checkpoint = func(context.Context) error {
		messages := h.working.Messages()
		if len(messages) > 0 && messages[len(messages)-1].Text() == correction {
			saved = true
		}
		return nil
	}
	h.runner.Hooks.OnAssistantSaved = nil
	h.runner.Hooks.OnSteeringApplied = func(context.Context, SteeringInput) {
		if saved {
			applied++
		}
	}
	h.start()
	select {
	case <-waiting:
	case done := <-h.done:
		t.Fatalf("Run returned before hard wait: %v", done.err)
	case <-h.ctx.Done():
		t.Fatal(h.ctx.Err())
	}
	if records := h.server.snapshot(); len(records) != 0 {
		t.Fatalf("main requests while hard wait blocked = %d, want 0", len(records))
	}
	queue <- SteeringInput{ID: "correction", Message: memory.NewTextMessage(model.RoleUser, correction)}
	// 只有确认硬等待后才投递纠正并释放同一候选，避免依赖调度或 sleep。
	state.mu.Lock()
	candidate.state.State = "ready"
	state.current = candidate.state
	close(candidate.done)
	state.mu.Unlock()
	done := h.finish(t)
	if done.err == nil || done.err.Error() != "max turns exceeded (1)" || !done.result.HadToolCall {
		t.Fatalf("Run = %+v/%v, want one physical tool turn", done.result, done.err)
	}
	if projects != 1 || correctionProjections < 2 || applied != 1 || !saved {
		t.Fatalf("projects=%d correction projections=%d applied=%d saved=%v, want 1/>=2/1/true", projects, correctionProjections, applied, saved)
	}
	if state.Snapshot().State != "applied" {
		t.Fatalf("compact state = %+v, want applied existing candidate", state.Snapshot())
	}
	records := h.server.snapshot()
	if len(records) != 1 || records[0].decodeErr != nil {
		t.Fatalf("model requests = %+v, want exactly one main request", records)
	}
	last := records[0].request.Messages[len(records[0].request.Messages)-1]
	var content string
	if err := json.Unmarshal(last.Content, &content); err != nil || last.Role != "user" || content != correction {
		t.Fatalf("last request message = %+v/%v, want correction", last, err)
	}
	calls := h.executor.snapshot()
	if len(calls) != 2 {
		t.Fatalf("executions = %+v, want two declared calls", calls)
	}
	for _, call := range calls {
		messages := call.WorkingMessages
		if len(messages) < 2 || messages[len(messages)-2].Text() != correction {
			t.Fatalf("tool %s started without correction in context", call.ID)
		}
	}
}

func TestRunnerRechecksOversizedSteeringBeforeAnyPhysicalRequest(t *testing.T) {
	h := newCheckpointHarness(t, false)
	h.request.Memory = NewContextMemory("")
	h.request.AutoCompress = true
	h.request.MaxTurns = 1
	h.runner.Compressor = memory.NewCompressor()
	entered, release := make(chan struct{}), make(chan struct{})
	projected := false
	h.request.Project = func(context.Context) func([]memory.Message) []model.Message {
		return func(messages []memory.Message) []model.Message {
			if !projected {
				projected = true
				close(entered)
				select {
				case <-release:
				case <-h.ctx.Done():
				}
			}
			return memory.ModelMessages(messages)
		}
	}
	queue := make(chan SteeringInput, 1)
	h.request.TakeSteering = func(bool) []SteeringInput {
		select {
		case input := <-queue:
			return []SteeringInput{input}
		default:
			return nil
		}
	}
	saves, applied := 0, 0
	h.runner.Hooks.Checkpoint = func(context.Context) error { saves++; return nil }
	h.runner.Hooks.OnSteeringApplied = func(context.Context, SteeringInput) { applied++ }
	h.start()
	select {
	case <-entered:
	case done := <-h.done:
		t.Fatalf("Run returned before budget boundary: %v", done.err)
	case <-h.ctx.Done():
		t.Fatal(h.ctx.Err())
	}
	// 没有可压的已消费来源；新输入单独超窗时必须先明确容量错误，不能照发主请求。
	correction := strings.Repeat("correction ", h.request.Binding.ContextWindow())
	queue <- SteeringInput{ID: "large-correction", Message: memory.NewTextMessage(model.RoleUser, correction)}
	close(release)
	done := h.finish(t)
	if done.err == nil || !strings.Contains(done.err.Error(), "context capacity exceeded") || saves != 1 || applied != 1 {
		t.Fatalf("Run error=%v saves=%d applied=%d, want capacity error after one committed correction", done.err, saves, applied)
	}
	if len(h.server.snapshot()) != 0 || len(h.executor.snapshot()) != 0 {
		t.Fatalf("oversized correction started work: requests=%d tools=%d", len(h.server.snapshot()), len(h.executor.snapshot()))
	}
	messages := h.working.Messages()
	if len(messages) != 2 || messages[1].Text() != correction || messages[1].Origin != memory.OriginUser {
		t.Fatal("capacity failure lost committed correction")
	}
}
