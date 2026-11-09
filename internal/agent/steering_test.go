package agent

import (
	"context"
	"errors"
	"fmt"
	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/runner"
	"sync"
	"testing"
	"time"
)

func TestSteeringMailboxQueuesRemovesAndSeals(t *testing.T) {
	mailbox := newSteeringMailbox("run-1")
	first, created, err := mailbox.enqueue("run-1", "client-1", "first")
	if err != nil || !created {
		t.Fatalf("enqueue first = %#v, %v, %v", first, created, err)
	}
	second, created, err := mailbox.enqueue("run-1", "client-2", "second")
	if err != nil || !created {
		t.Fatalf("enqueue second = %#v, %v, %v", second, created, err)
	}
	if first.Sequence != 1 || second.Sequence != 2 {
		t.Fatalf("sequences = %d/%d, want 1/2", first.Sequence, second.Sequence)
	}
	duplicate, created, err := mailbox.enqueue("run-1", "client-1", "first")
	if err != nil || created || duplicate.ID != first.ID {
		t.Fatalf("duplicate = %#v, %v, %v, want existing item", duplicate, created, err)
	}
	if _, _, err := mailbox.enqueue("run-1", "client-1", "changed"); err == nil {
		t.Fatal("conflicting client message error = nil")
	}
	removed, _, changed, err := mailbox.remove("run-1", second.ID)
	if err != nil || !changed || removed.State != SteeringRemoved {
		t.Fatalf("remove = %#v, %v, %v", removed, changed, err)
	}
	removedAgain, _, changed, err := mailbox.remove("run-1", second.ID)
	if err != nil || changed || removedAgain.State != SteeringRemoved {
		t.Fatalf("idempotent remove = %#v, %v, %v", removedAgain, changed, err)
	}
	got := mailbox.take(false)
	for _, item := range got {
		mailbox.applied(item.ID)
	}
	if len(got) != 1 || got[0].ID != first.ID || got[0].Sequence != first.Sequence || got[0].Message.Text() != "first" {
		t.Fatalf("take() = %#v, want first only", got)
	}
	if got := mailbox.take(true); len(got) != 0 {
		t.Fatalf("sealing take = %#v, want empty", got)
	}
	if _, _, err := mailbox.enqueue("run-1", "client-3", "late"); err == nil {
		t.Fatal("enqueue after seal error = nil")
	}
}

func TestSteeringMailboxRemoveRestoresLatestAppliedInput(t *testing.T) {
	mailbox := newSteeringMailbox("run-1")
	_, _, _ = mailbox.enqueue("run-1", "client-1", "applied")
	for _, item := range mailbox.take(false) {
		mailbox.applied(item.ID)
	}
	queued, _, err := mailbox.enqueue("run-1", "client-2", "queued")
	if err != nil {
		t.Fatalf("enqueue queued error = %v", err)
	}
	_, latest, changed, err := mailbox.remove("run-1", queued.ID)
	if err != nil || !changed || latest != "applied" {
		t.Fatalf("remove latest = %q, %v, %v, want applied", latest, changed, err)
	}
}

func TestSteeringMailboxBlocksChangesDuringInteraction(t *testing.T) {
	mailbox := newSteeringMailbox("run-1")
	item, _, err := mailbox.enqueue("run-1", "client-1", "queued")
	if err != nil {
		t.Fatalf("enqueue error = %v", err)
	}
	finish := mailbox.beginInteraction()
	defer finish()
	if _, _, err := mailbox.enqueue("run-1", "client-2", "blocked"); err == nil {
		t.Fatal("enqueue during interaction error = nil")
	}
	if _, _, _, err := mailbox.remove("run-1", item.ID); err == nil {
		t.Fatal("remove during interaction error = nil")
	}
	finish()
	if _, _, changed, err := mailbox.remove("run-1", item.ID); err != nil || !changed {
		t.Fatalf("remove after interaction = %v, %v", changed, err)
	}
}

func TestSteeringMailboxBoundsTotalAcceptedMessagesPerRun(t *testing.T) {
	mailbox := newSteeringMailbox("run-1")
	for i := 0; i < MaxSteeringMessages; i++ {
		clientID := fmt.Sprintf("client-%d", i)
		if _, _, err := mailbox.enqueue("run-1", clientID, "message"); err != nil {
			t.Fatalf("enqueue %d error = %v", i, err)
		}
		for _, item := range mailbox.take(false) {
			mailbox.applied(item.ID)
		}
	}
	if _, _, err := mailbox.enqueue("run-1", "overflow", "message"); err == nil {
		t.Fatal("enqueue after total run limit error = nil")
	}
}

func TestSteeringMailboxRejectsPendingOnRunFailure(t *testing.T) {
	mailbox := newSteeringMailbox("run-1")
	item, _, err := mailbox.enqueue("run-1", "client-1", "keep this")
	if err != nil {
		t.Fatalf("enqueue error = %v", err)
	}
	got := mailbox.rejectPending()
	if len(got) != 1 || got[0].ID != item.ID || got[0].State != SteeringRejected || got[0].Text != "keep this" {
		t.Fatalf("rejectPending() = %#v", got)
	}
}

func TestSteeringInteractionsRemainPendingUntilAllFinish(t *testing.T) {
	a := &Agent{}
	a.PrepareSteering("run-1")
	item, _, err := a.EnqueueSteering("run-1", "initial", "queued")
	if err != nil {
		t.Fatal(err)
	}
	first := a.beginSteeringInteraction("run-1")
	second := a.beginSteeringInteraction("run-1")
	defer first()
	defer second()
	assertBlocked := func() {
		t.Helper()
		if _, _, err := a.EnqueueSteering("run-1", "new", "new"); err == nil || err.Error() != "interaction_pending" {
			t.Fatalf("enqueue error = %v, want interaction_pending", err)
		}
		if _, _, err := a.RemoveSteering("run-1", item.ID); err == nil || err.Error() != "interaction_pending" {
			t.Fatalf("remove error = %v, want interaction_pending", err)
		}
	}
	assertBlocked()
	first()
	first()
	assertBlocked()
	second()
	if _, _, err := a.EnqueueSteering("run-1", "new", "new"); err != nil {
		t.Fatal(err)
	}
	if _, removed, err := a.RemoveSteering("run-1", item.ID); err != nil || !removed {
		t.Fatalf("remove = %v/%v, want true/nil", removed, err)
	}
}

func TestSteeringInteractionFinishIsBoundToOriginalMailbox(t *testing.T) {
	a := &Agent{}
	a.PrepareSteering("old")
	oldFinish := a.beginSteeringInteraction("old")
	a.clearSteeringMailbox("old")
	a.PrepareSteering("new")
	newFinish := a.beginSteeringInteraction("new")
	defer newFinish()
	oldFinish()
	a.beginSteeringInteraction("old")()
	if _, _, err := a.EnqueueSteering("new", "message", "text"); err == nil || err.Error() != "interaction_pending" {
		t.Fatalf("enqueue error = %v, want interaction_pending", err)
	}
	newFinish()
	if _, _, err := a.EnqueueSteering("new", "message", "text"); err != nil {
		t.Fatal(err)
	}
	// 不存在 mailbox 的工具调用不创建隐式 run，也无需额外释放状态。
	a.clearSteeringMailbox("new")
	finish := a.beginSteeringInteraction("missing")
	finish()
	finish()
	if a.steering != nil {
		t.Fatal("missing run created a mailbox")
	}
}

func TestSteeringInteractionCountHandlesConcurrentFinish(t *testing.T) {
	a := &Agent{}
	a.PrepareSteering("run-1")
	anchor := a.beginSteeringInteraction("run-1")
	defer anchor()
	const workers = 32
	finishes := make(chan func(), workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() { finishes <- a.beginSteeringInteraction("run-1") })
	}
	wg.Wait()
	close(finishes)
	for finish := range finishes {
		wg.Go(finish)
		wg.Go(finish)
	}
	wg.Wait()
	if _, _, err := a.EnqueueSteering("run-1", "blocked", "text"); err == nil || err.Error() != "interaction_pending" {
		t.Fatalf("enqueue error = %v, want remaining interaction to block", err)
	}
	anchor()
	if _, _, err := a.EnqueueSteering("run-1", "allowed", "text"); err != nil {
		t.Fatal(err)
	}
	a.steering.mu.Lock()
	count := a.steering.interactionCount
	a.steering.mu.Unlock()
	if count != 0 {
		t.Fatalf("interaction count = %d, want 0", count)
	}
}

func TestSteeringMailboxClaimPreservesSealAndQueueSemantics(t *testing.T) {
	mailbox := newSteeringMailbox("run")
	first, _, err := mailbox.enqueue("run", "first", "first")
	if err != nil {
		t.Fatal(err)
	}
	claimed := mailbox.take(true)
	if len(claimed) != 1 || claimed[0].ID != first.ID {
		t.Fatalf("take(true) = %+v, want first", claimed)
	}
	if got := mailbox.take(false); len(got) != 0 {
		t.Fatalf("second take = %+v, want no duplicate claim", got)
	}
	// 有消息的终态取出不能封闭队列：原语义允许下一次主请求期间继续 steering。
	second, _, err := mailbox.enqueue("run", "second", "second")
	if err != nil {
		t.Fatalf("enqueue after nonempty take(true): %v", err)
	}
	if _, _, removed, err := mailbox.remove("run", second.ID); err != nil || !removed {
		t.Fatalf("remove unclaimed = %v/%v, want true/nil", removed, err)
	}
	if _, _, removed, err := mailbox.remove("run", first.ID); err == nil || removed {
		t.Fatalf("remove claimed = %v/%v, want false/error", removed, err)
	}
	mailbox.applied(first.ID)
	if got := mailbox.take(true); len(got) != 0 {
		t.Fatalf("empty sealing take = %+v, want empty", got)
	}
	if _, _, err := mailbox.enqueue("run", "late", "late"); err == nil {
		t.Fatal("enqueue after empty take(true) error = nil")
	}
}

func TestSteeringClaimCannotBeRemovedDuringCheckpointAndFailureRejectsRemainder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a := &Agent{}
	a.PrepareSteering("run")
	first, _, err := a.EnqueueSteering("run", "first", "first correction")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := a.EnqueueSteering("run", "second", "second correction")
	if err != nil {
		t.Fatal(err)
	}
	// 第二条保存失败便会退出，绑定只通过本地构造校验，不会发出模型请求。
	router, err := model.NewRouter(&config.Config{Models: []config.ModelConfig{{
		Provider: "custom", Protocol: config.ModelProtocolOpenAIChat, Model: "offline",
		BaseURL: "https://api.example.com/v1", APIKey: "test-api-key", ContextWindow: 128000, MaxOutputTokens: 1024,
	}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := router.Bind("custom/offline")
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan chan error)
	applied := make(chan runner.SteeringInput, 2)
	working := memory.NewWorkingMemory()
	r := &runner.Runner{Hooks: runner.Hooks{
		Checkpoint: func(ctx context.Context) error {
			reply := make(chan error, 1)
			select {
			case entered <- reply:
			case <-ctx.Done():
				return ctx.Err()
			}
			select {
			case err := <-reply:
				return err
			case <-ctx.Done():
				return ctx.Err()
			}
		},
		OnSteeringApplied: func(ctx context.Context, input runner.SteeringInput) {
			a.onSteeringApplied(ctx, input)
			applied <- input
		},
	}}
	done := make(chan error, 1)
	go func() {
		_, err := r.Run(ctx, runner.Request{Binding: binding, Working: working,
			TakeSteering: func(seal bool) []runner.SteeringInput { return a.takeSteering("run", seal) },
		})
		done <- err
	}()
	nextCheckpoint := func() chan error {
		t.Helper()
		select {
		case reply := <-entered:
			return reply
		case err := <-done:
			t.Fatalf("Run returned before checkpoint: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		return nil
	}
	reply := nextCheckpoint()
	for _, id := range []string{first.ID, second.ID} {
		if _, removed, err := a.RemoveSteering("run", id); err == nil || removed {
			t.Fatalf("remove claimed %s = %v/%v, want false/error", id, removed, err)
		}
	}
	duplicate, created, err := a.EnqueueSteering("run", "first", first.Text)
	if err != nil || created || duplicate.State != SteeringQueued {
		t.Fatalf("uncommitted duplicate = %+v/%v/%v, want queued/false/nil", duplicate, created, err)
	}
	// 保存阻塞时仍能接受和撤回新消息，证明 mailbox 锁不跨检查点持有。
	late, _, err := a.EnqueueSteering("run", "late", "late correction")
	if err != nil {
		t.Fatal(err)
	}
	if _, removed, err := a.RemoveSteering("run", late.ID); err != nil || !removed {
		t.Fatalf("remove unclaimed = %v/%v, want true/nil", removed, err)
	}
	select {
	case input := <-applied:
		t.Fatalf("applied before checkpoint success: %+v", input)
	default:
	}
	reply <- nil
	reply = nextCheckpoint()
	select {
	case input := <-applied:
		if input.ID != first.ID {
			t.Fatalf("applied ID = %s, want %s", input.ID, first.ID)
		}
	default:
		t.Fatal("first committed input was not applied")
	}
	failure := errors.New("steering checkpoint failed")
	reply <- failure
	select {
	case err := <-done:
		if !errors.Is(err, failure) {
			t.Fatalf("Run error = %v, want %v", err, failure)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case input := <-applied:
		t.Fatalf("failed checkpoint published applied: %+v", input)
	default:
	}
	rejected := a.RejectPendingSteering("run")
	if len(rejected) != 1 || rejected[0].ID != second.ID || rejected[0].State != SteeringRejected {
		t.Fatalf("rejected = %+v, want second correction only", rejected)
	}
	if messages := working.Messages(); len(messages) != 1 || messages[0].Text() != first.Text {
		t.Fatalf("working messages = %+v, want committed correction only", messages)
	}
	duplicate, _, err = a.EnqueueSteering("run", "first", first.Text)
	if err != nil || duplicate.State != SteeringApplied {
		t.Fatalf("committed duplicate = %+v/%v, want applied/nil", duplicate, err)
	}
	duplicate, _, err = a.EnqueueSteering("run", "second", second.Text)
	if err != nil || duplicate.State != SteeringRejected {
		t.Fatalf("failed duplicate = %+v/%v, want rejected/nil", duplicate, err)
	}
}
