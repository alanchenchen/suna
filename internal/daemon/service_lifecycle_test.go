package daemon

import (
	"context"
	"testing"

	"github.com/alanchenchen/suna/internal/agent"
	"github.com/alanchenchen/suna/internal/protocol"
)

type captureEventSink struct {
	events []protocol.Event
}

func (s *captureEventSink) Emit(_ context.Context, event protocol.Event) error {
	s.events = append(s.events, event)
	return nil
}

func TestServiceClearsPendingInteractionsWhenRuntimeBecomesOrphaned(t *testing.T) {
	ctx := context.Background()
	manager := newTestSessionManager(t)
	d := &Daemon{sessions: manager, sinks: map[string]protocol.EventSink{}}
	svc := newService(d)
	manager.onOrphan = svc.cancelPendingInteractions

	snapshot, err := manager.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	if _, _, err := manager.beginRun("client-a"); err != nil {
		t.Fatalf("beginRun error = %v", err)
	}
	svc.pendingAsks.Store("ask", pendingInteraction{sessionID: snapshot.Session.ID, reply: make(chan string, 1)})
	svc.pendingGuards.Store("guard", pendingInteraction{sessionID: snapshot.Session.ID, reply: make(chan string, 1)})
	svc.pendingAsks.Store("other-ask", pendingInteraction{sessionID: "other", reply: make(chan string, 1)})
	svc.pendingGuards.Store("other-guard", pendingInteraction{sessionID: "other", reply: make(chan string, 1)})

	manager.detach("client-a")
	if _, ok := svc.pendingAsks.Load("ask"); ok {
		t.Fatal("orphan pending ask still exists")
	}
	if _, ok := svc.pendingGuards.Load("guard"); ok {
		t.Fatal("orphan pending guard still exists")
	}
	if _, ok := svc.pendingAsks.Load("other-ask"); !ok {
		t.Fatal("other session pending ask was removed")
	}
	if _, ok := svc.pendingGuards.Load("other-guard"); !ok {
		t.Fatal("other session pending guard was removed")
	}
}

func TestRunAgentEventsKeepsSessionBusyUntilEventStreamCloses(t *testing.T) {
	ctx := context.Background()
	manager := newTestSessionManager(t)
	snapshot, err := manager.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	if _, _, err := manager.beginRun("client-a"); err != nil {
		t.Fatalf("beginRun error = %v", err)
	}

	d := &Daemon{sessions: manager, sinks: map[string]protocol.EventSink{}}
	svc := newService(d)
	events := make(chan agent.Event)
	done := make(chan struct{})
	go func() {
		svc.runAgentEvents(ctx, "client-a", snapshot.Session.ID, "input", events, &captureEventSink{})
		close(done)
	}()

	events <- agent.Event{Type: agent.EventStatus, Status: agent.StatusDone}
	if _, _, err := manager.beginRun("client-a"); err == nil {
		t.Fatal("beginRun before event stream close error = nil, want session_busy")
	}

	close(events)
	<-done
	if _, _, err := manager.beginRun("client-a"); err != nil {
		t.Fatalf("beginRun after event stream close error = %v", err)
	}
}
