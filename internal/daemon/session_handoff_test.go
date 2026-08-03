package daemon

import (
	"context"
	"testing"

	"github.com/alanchenchen/suna/internal/protocol"
)

func TestSessionAttachBroadcastsSharedClientCount(t *testing.T) {
	ctx := context.Background()
	manager := newTestSessionManager(t)
	snapshot, err := manager.create(ctx, "local-tui", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}

	localSink := &captureEventSink{}
	tcpSink := &captureEventSink{}
	d := &Daemon{
		sessions: manager,
		sinks: map[string]protocol.EventSink{
			"local-tui": localSink,
			"tcp-app":   tcpSink,
		},
	}
	svc := newService(d)
	result, err := svc.handleSessionAttach(ctx, protocol.Request{ConnID: "tcp-app", Params: protocol.SessionAttachParams{SessionID: snapshot.Session.ID}})
	if err != nil {
		t.Fatalf("attach error = %v", err)
	}
	joined, ok := result.(protocol.SessionSnapshot)
	if !ok {
		t.Fatalf("attach result = %T, want protocol.SessionSnapshot", result)
	}
	if got, want := joined.Session.ClientCount, 2; got != want {
		t.Fatalf("attach client count = %d, want %d", got, want)
	}

	localShared := receivedSessionClientCount(localSink.events, snapshot.Session.ID, 2)
	tcpShared := receivedSessionClientCount(tcpSink.events, snapshot.Session.ID, 2)
	if !localShared {
		t.Fatalf("local client did not receive shared session state: %#v", localSink.events)
	}
	if !tcpShared {
		t.Fatalf("tcp client did not receive shared session state: %#v", tcpSink.events)
	}

	d.removeConnection("tcp-app")
	if !receivedSessionClientCount(localSink.events, snapshot.Session.ID, 1) {
		t.Fatalf("local client did not receive detached session state: %#v", localSink.events)
	}
}

func receivedSessionClientCount(events []protocol.Event, sessionID string, want int) bool {
	for _, event := range events {
		if event.Method != protocol.NotifySessionUpdated {
			continue
		}
		params, ok := event.Params.(protocol.SessionStateParams)
		if ok && params.Session.ID == sessionID && params.Session.ClientCount == want {
			return true
		}
	}
	return false
}
