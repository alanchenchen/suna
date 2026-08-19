package agent

import (
	"fmt"
	"testing"
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
	_ = mailbox.take(false)
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
	mailbox.setInteractionPending(true)
	if _, _, err := mailbox.enqueue("run-1", "client-2", "blocked"); err == nil {
		t.Fatal("enqueue during interaction error = nil")
	}
	if _, _, _, err := mailbox.remove("run-1", item.ID); err == nil {
		t.Fatal("remove during interaction error = nil")
	}
	mailbox.setInteractionPending(false)
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
		_ = mailbox.take(false)
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
