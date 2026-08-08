package daemon

import (
	"context"
	"testing"

	"github.com/alanchenchen/suna/internal/protocol"
)

func TestSessionCatalogBroadcastsClientCountToUnattachedObservers(t *testing.T) {
	ctx := context.Background()
	manager := newTestSessionManager(t)
	snapshot, err := manager.create(ctx, "local-tui", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}

	localSink := &captureEventSink{}
	joinedSink := &captureEventSink{}
	catalogSink := &captureEventSink{}
	d := &Daemon{
		sessions: manager,
		sinks: map[string]protocol.EventSink{
			"local-tui":        localSink,
			"joined-client":    joinedSink,
			"catalog-observer": catalogSink,
		},
	}
	svc := newService(d)
	result, err := svc.handleSessionAttach(ctx, protocol.Request{ConnID: "joined-client", Params: protocol.SessionAttachParams{SessionID: snapshot.Session.ID}})
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

	for name, sink := range map[string]*captureEventSink{"owner": localSink, "joined": joinedSink, "catalog": catalogSink} {
		if !receivedSessionClientCount(sink.Events(), snapshot.Session.ID, 2) {
			t.Fatalf("%s client did not receive shared session state: %#v", name, sink.Events())
		}
	}

	d.removeConnection("joined-client")
	for name, sink := range map[string]*captureEventSink{"owner": localSink, "catalog": catalogSink} {
		if !receivedSessionClientCount(sink.Events(), snapshot.Session.ID, 1) {
			t.Fatalf("%s client did not receive detached session state: %#v", name, sink.Events())
		}
	}
	if receivedSessionClientCount(joinedSink.Events(), snapshot.Session.ID, 1) {
		t.Fatalf("disconnected client received catalog update: %#v", joinedSink.Events())
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
