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
	if _, _, _, err := manager.beginRun("client-a"); err != nil {
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

func TestRunAgentEventsBroadcastsIdleSessionStateAfterRunCloses(t *testing.T) {
	ctx := context.Background()
	manager := newTestSessionManager(t)
	snapshot, err := manager.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	if _, err := manager.attach(ctx, "client-b", snapshot.Session.ID, false); err != nil {
		t.Fatalf("attach observer error = %v", err)
	}
	if _, _, _, err := manager.beginRun("client-a"); err != nil {
		t.Fatalf("beginRun error = %v", err)
	}

	ownerSink := &captureEventSink{}
	observerSink := &captureEventSink{}
	d := &Daemon{sessions: manager, sinks: map[string]protocol.EventSink{"client-a": ownerSink, "client-b": observerSink}}
	svc := newService(d)
	events := make(chan agent.Event, 1)
	events <- agent.Event{Type: agent.EventStatus, Status: agent.StatusDone}
	close(events)

	svc.runAgentEvents(ctx, "client-a", snapshot.Session.ID, manager.currentRunID(snapshot.Session.ID), "input", events, ownerSink)

	for _, sink := range []*captureEventSink{ownerSink, observerSink} {
		foundIdle := false
		for _, event := range sink.events {
			if event.Method != protocol.NotifySessionUpdated {
				continue
			}
			params, ok := event.Params.(protocol.SessionStateParams)
			if ok && params.Session.Status == protocol.SessionStatusIdle && params.Session.ClientCount == 2 {
				foundIdle = true
				break
			}
		}
		if !foundIdle {
			t.Fatalf("idle session.updated not broadcast to attached client: %#v", sink.events)
		}
	}
}

func TestHandleCompactBroadcastsIdleSessionState(t *testing.T) {
	ctx := context.Background()
	manager := newTestSessionManager(t)
	snapshot, err := manager.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	if _, err := manager.attach(ctx, "client-b", snapshot.Session.ID, false); err != nil {
		t.Fatalf("attach observer error = %v", err)
	}

	ownerSink := &captureEventSink{}
	observerSink := &captureEventSink{}
	d := &Daemon{sessions: manager, sinks: map[string]protocol.EventSink{"client-a": ownerSink, "client-b": observerSink}}
	svc := newService(d)
	if _, err := svc.handleCompact(ctx, protocol.Request{ConnID: "client-a"}, ownerSink); err != nil {
		t.Fatalf("compact error = %v", err)
	}

	for name, sink := range map[string]*captureEventSink{"owner": ownerSink, "observer": observerSink} {
		foundIdle := false
		for _, event := range sink.events {
			params, ok := event.Params.(protocol.SessionStateParams)
			if event.Method == protocol.NotifySessionUpdated && ok && params.Session.Status == protocol.SessionStatusIdle && params.Session.ClientCount == 2 {
				foundIdle = true
				break
			}
		}
		if !foundIdle {
			t.Fatalf("%s client did not receive compact idle state: %#v", name, sink.events)
		}
	}
}

func TestRunAgentEventsKeepsSessionBusyUntilEventStreamCloses(t *testing.T) {
	ctx := context.Background()
	manager := newTestSessionManager(t)
	snapshot, err := manager.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	if _, _, _, err := manager.beginRun("client-a"); err != nil {
		t.Fatalf("beginRun error = %v", err)
	}

	d := &Daemon{sessions: manager, sinks: map[string]protocol.EventSink{}}
	svc := newService(d)
	events := make(chan agent.Event)
	done := make(chan struct{})
	go func() {
		svc.runAgentEvents(ctx, "client-a", snapshot.Session.ID, "run-1", "input", events, &captureEventSink{})
		close(done)
	}()

	events <- agent.Event{Type: agent.EventStatus, Status: agent.StatusDone}
	if _, _, _, err := manager.beginRun("client-a"); err == nil {
		t.Fatal("beginRun before event stream close error = nil, want session_busy")
	}

	close(events)
	<-done
	if _, _, _, err := manager.beginRun("client-a"); err != nil {
		t.Fatalf("beginRun after event stream close error = %v", err)
	}
}

func TestCancellingSuppressesNonCancelledRunStates(t *testing.T) {
	ctx := context.Background()
	manager := newTestSessionManager(t)
	snapshot, err := manager.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	if _, _, _, err := manager.beginRun("client-a"); err != nil {
		t.Fatalf("beginRun error = %v", err)
	}
	runID := manager.currentRunID(snapshot.Session.ID)
	sink := &captureEventSink{}
	d := &Daemon{sessions: manager, sinks: map[string]protocol.EventSink{"client-a": sink}}
	svc := newService(d)

	var publishCalls int
	newlyMarked, err := manager.markCancelling("client-a", func(_ *sessionRuntime, sessionID, gotRunID string, phase protocol.AgentRunPhase) {
		publishCalls++
		svc.emitAgentRun(ctx, sessionID, "client-a", protocol.AgentRunParams{RunID: gotRunID, State: protocol.AgentRunCancelling, Phase: phase})
	})
	if err != nil || !newlyMarked {
		t.Fatalf("first cancel = %v, %v", newlyMarked, err)
	}
	newlyMarked, err = manager.markCancelling("client-a", func(*sessionRuntime, string, string, protocol.AgentRunPhase) { publishCalls++ })
	if err != nil || newlyMarked || publishCalls != 1 {
		t.Fatalf("duplicate cancel = %v, %v, calls=%d", newlyMarked, err, publishCalls)
	}

	events := make(chan agent.Event, 3)
	events <- agent.Event{Type: agent.EventStatus, Status: agent.StatusWaitingLLM}
	events <- agent.Event{Type: agent.EventStatus, Status: agent.StatusLLMRetrying}
	events <- agent.Event{Type: agent.EventStatus, Status: agent.StatusDone}
	close(events)
	svc.runAgentEvents(ctx, "client-a", snapshot.Session.ID, runID, "input", events, sink)

	var states []protocol.AgentRunState
	for _, event := range sink.events {
		if event.Method != protocol.NotifyAgentRun {
			continue
		}
		params := event.Params.(protocol.AgentRunParams)
		states = append(states, params.State)
		if params.State == protocol.AgentRunCancelling && params.CanControl {
			t.Fatal("cancelling CanControl = true")
		}
	}
	want := []protocol.AgentRunState{protocol.AgentRunCancelling, protocol.AgentRunCancelled}
	if len(states) != len(want) || states[0] != want[0] || states[1] != want[1] {
		t.Fatalf("states = %#v, want %#v", states, want)
	}
}
