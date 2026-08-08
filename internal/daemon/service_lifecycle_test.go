package daemon

import (
	"context"
	"sync"
	"testing"

	"github.com/alanchenchen/suna/internal/agent"
	"github.com/alanchenchen/suna/internal/protocol"
)

type captureEventSink struct {
	mu     sync.Mutex
	events []protocol.Event
}

func (s *captureEventSink) Emit(_ context.Context, event protocol.Event) error {
	s.mu.Lock()
	s.events = append(s.events, event)
	s.mu.Unlock()
	return nil
}

func (s *captureEventSink) Events() []protocol.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]protocol.Event(nil), s.events...)
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

func TestRunLifecycleBroadcastsCatalogStateWithoutLeakingDetailedEvents(t *testing.T) {
	ctx := context.Background()
	manager := newTestSessionManager(t)
	_, err := manager.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}

	ownerSink := &captureEventSink{}
	observerSink := &captureEventSink{}
	d := &Daemon{sessions: manager, sinks: map[string]protocol.EventSink{"client-a": ownerSink, "catalog-observer": observerSink}}
	svc := newService(d)
	_, sessionID, runID, err := svc.beginAgentRun(ctx, "client-a")
	if err != nil {
		t.Fatalf("beginAgentRun error = %v", err)
	}
	if !receivedSessionStatus(observerSink.Events(), sessionID, protocol.SessionStatusRunning, 1) {
		t.Fatalf("unattached observer did not receive running catalog state: %#v", observerSink.Events())
	}
	if receivedMethod(observerSink.Events(), protocol.NotifyAgentRun) {
		t.Fatalf("unattached observer received agent.run: %#v", observerSink.Events())
	}

	events := make(chan agent.Event, 6)
	events <- agent.Event{Type: agent.EventStatus, Status: agent.StatusCompactRunning}
	events <- agent.Event{Type: agent.EventStatus, Status: agent.StatusCompactDone}
	events <- agent.Event{Type: agent.EventStatus, Status: agent.StatusWaitingLLM}
	events <- agent.Event{Type: agent.EventStream, Content: "private output"}
	events <- agent.Event{Type: agent.EventToolCall, ToolCallID: "tool-1", ToolName: "test-tool"}
	events <- agent.Event{Type: agent.EventStatus, Status: agent.StatusDone}
	close(events)
	svc.runAgentEvents(ctx, "client-a", sessionID, runID, "input", events, ownerSink)

	if !receivedSessionStatus(observerSink.Events(), sessionID, protocol.SessionStatusCompacting, 1) {
		t.Fatalf("unattached observer did not receive automatic compacting state: %#v", observerSink.Events())
	}
	if !receivedSessionStatus(observerSink.Events(), sessionID, protocol.SessionStatusIdle, 1) {
		t.Fatalf("unattached observer did not receive idle catalog state: %#v", observerSink.Events())
	}
	for _, method := range []string{protocol.NotifyAgentRun, protocol.NotifyAgentDelta, protocol.NotifyToolStart, protocol.NotifyCompactResult} {
		if receivedMethod(observerSink.Events(), method) {
			t.Fatalf("unattached observer received detailed %s event: %#v", method, observerSink.Events())
		}
	}
	for _, method := range []string{protocol.NotifyAgentRun, protocol.NotifyAgentDelta, protocol.NotifyToolStart, protocol.NotifyCompactResult} {
		if !receivedMethod(ownerSink.Events(), method) {
			t.Fatalf("attached owner did not receive detailed %s event: %#v", method, ownerSink.Events())
		}
	}
}

func TestResumeRunBroadcastsRunningCatalogStateToUnattachedObserver(t *testing.T) {
	ctx := context.Background()
	manager := newTestSessionManager(t)
	snapshot, err := manager.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}

	ownerSink := &captureEventSink{}
	observerSink := &captureEventSink{}
	d := &Daemon{sessions: manager, sinks: map[string]protocol.EventSink{"client-a": ownerSink, "catalog-observer": observerSink}}
	svc := newService(d)
	if _, err := svc.handleResumeRun(ctx, protocol.Request{ConnID: "client-a"}, ownerSink); err != nil {
		t.Fatalf("resume error = %v", err)
	}

	if !receivedSessionStatus(observerSink.Events(), snapshot.Session.ID, protocol.SessionStatusRunning, 1) {
		t.Fatalf("unattached observer did not receive resume running state: %#v", observerSink.Events())
	}
	if receivedMethod(observerSink.Events(), protocol.NotifyAgentRun) {
		t.Fatalf("unattached observer received resume agent.run: %#v", observerSink.Events())
	}
}

func TestHandleCompactBroadcastsCatalogStateToUnattachedObserver(t *testing.T) {
	ctx := context.Background()
	manager := newTestSessionManager(t)
	snapshot, err := manager.create(ctx, "client-a", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}

	ownerSink := &captureEventSink{}
	observerSink := &captureEventSink{}
	d := &Daemon{sessions: manager, sinks: map[string]protocol.EventSink{"client-a": ownerSink, "catalog-observer": observerSink}}
	svc := newService(d)
	if _, err := svc.handleCompact(ctx, protocol.Request{ConnID: "client-a"}, ownerSink); err != nil {
		t.Fatalf("compact error = %v", err)
	}

	if !receivedSessionStatus(observerSink.Events(), snapshot.Session.ID, protocol.SessionStatusCompacting, 1) {
		t.Fatalf("unattached observer did not receive compacting catalog state: %#v", observerSink.Events())
	}
	if !receivedSessionStatus(observerSink.Events(), snapshot.Session.ID, protocol.SessionStatusIdle, 1) {
		t.Fatalf("unattached observer did not receive compact idle state: %#v", observerSink.Events())
	}
	if receivedMethod(observerSink.Events(), protocol.NotifyCompactResult) {
		t.Fatalf("unattached observer received compact details: %#v", observerSink.Events())
	}
	if !receivedMethod(ownerSink.Events(), protocol.NotifyCompactResult) {
		t.Fatalf("attached owner did not receive compact result: %#v", ownerSink.Events())
	}
}

func receivedSessionStatus(events []protocol.Event, sessionID string, want protocol.SessionStatus, clientCount int) bool {
	for _, event := range events {
		params, ok := event.Params.(protocol.SessionStateParams)
		if event.Method == protocol.NotifySessionUpdated && ok && params.Session.ID == sessionID && params.Session.Status == want && params.Session.ClientCount == clientCount {
			return true
		}
	}
	return false
}

func receivedMethod(events []protocol.Event, method string) bool {
	for _, event := range events {
		if event.Method == method {
			return true
		}
	}
	return false
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
	for _, event := range sink.Events() {
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
