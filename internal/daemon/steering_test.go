package daemon

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/alanchenchen/suna/internal/agent"
	"github.com/alanchenchen/suna/internal/protocol"
)

func TestRunAgentEventsOrdersAppliedSteeringAfterToolResult(t *testing.T) {
	ctx := context.Background()
	manager := newTestSessionManager(t)
	snapshot, err := manager.create(ctx, "owner", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	_, _, runID, err := manager.beginRun("owner")
	if err != nil {
		t.Fatalf("beginRun error = %v", err)
	}
	sink := &captureEventSink{}
	d := &Daemon{sessions: manager, sinks: map[string]protocol.EventSink{"owner": sink}}
	svc := newService(d)
	events := make(chan agent.Event, 3)
	done := make(chan struct{})
	go func() {
		svc.runAgentEvents(ctx, "owner", snapshot.Session.ID, runID, "input", events, sink)
		close(done)
	}()
	item := agent.SteeringItem{ID: "steer-1", ClientMsgID: "client-1", Text: "continue", State: agent.SteeringApplied, Sequence: 4}
	events <- agent.Event{Type: agent.EventToolResult, ToolCallID: "tool-1", ToolName: "readfile", ToolResult: "done"}
	events <- agent.Event{Type: agent.EventSteering, Steering: &item}
	events <- agent.Event{Type: agent.EventStatus, Status: agent.StatusDone}
	close(events)
	<-done

	var methods []string
	var applied protocol.SteeringMessage
	for _, event := range sink.Events() {
		methods = append(methods, event.Method)
		if event.Method == protocol.NotifySteering {
			body, _ := json.Marshal(event.Params)
			_ = json.Unmarshal(body, &applied)
		}
	}
	toolIdx, steeringIdx, userIdx := indexMethod(methods, protocol.NotifyToolEnd), indexMethod(methods, protocol.NotifySteering), indexMethod(methods, protocol.NotifySessionUserMessage)
	if toolIdx < 0 || steeringIdx <= toolIdx || userIdx <= steeringIdx {
		t.Fatalf("event methods = %v, want tool_end < steering < user_message", methods)
	}
	if applied.State != protocol.SteeringApplied || applied.Sequence != 4 {
		t.Fatalf("applied steering = %#v", applied)
	}
	if applied.SessionID != snapshot.Session.ID {
		t.Fatalf("applied steering session_id = %q, want %q", applied.SessionID, snapshot.Session.ID)
	}
}

func indexMethod(methods []string, method string) int {
	for i, got := range methods {
		if got == method {
			return i
		}
	}
	return -1
}

func TestSteeringQueuesMultipleMessagesAndRestoresSnapshot(t *testing.T) {
	ctx := context.Background()
	manager := newTestSessionManager(t)
	snapshot, err := manager.create(ctx, "owner", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	if _, err := manager.attach(ctx, "observer", snapshot.Session.ID, false); err != nil {
		t.Fatalf("attach observer error = %v", err)
	}
	ownerSink := &captureEventSink{}
	observerSink := &captureEventSink{}
	d := &Daemon{sessions: manager, sinks: map[string]protocol.EventSink{"owner": ownerSink, "observer": observerSink}}
	svc := newService(d)
	_, _, runID, err := svc.beginAgentRun(ctx, "owner")
	if err != nil {
		t.Fatalf("beginAgentRun error = %v", err)
	}

	first, err := svc.handleSteer(ctx, protocol.Request{ConnID: "owner", Params: protocol.SteerParams{RunID: runID, ClientMsgID: "client-1", Parts: textParts("first")}})
	if err != nil {
		t.Fatalf("handleSteer first error = %v", err)
	}
	second, err := svc.handleSteer(ctx, protocol.Request{ConnID: "owner", Params: protocol.SteerParams{RunID: runID, ClientMsgID: "client-2", Parts: textParts("second")}})
	if err != nil {
		t.Fatalf("handleSteer second error = %v", err)
	}
	if first.Message.Sequence != 1 || second.Message.Sequence != 2 || !first.Message.CanControl || !second.Message.CanControl {
		t.Fatalf("queued messages = %#v / %#v", first.Message, second.Message)
	}
	duplicate, err := svc.handleSteer(ctx, protocol.Request{ConnID: "owner", Params: protocol.SteerParams{RunID: runID, ClientMsgID: "client-1", Parts: textParts("first")}})
	if err != nil || duplicate.Message.ID != first.Message.ID || duplicate.Message.Sequence != 1 {
		t.Fatalf("duplicate = %#v, %v", duplicate.Message, err)
	}
	if got := countSteeringNotifications(ownerSink.Events(), protocol.SteeringQueued); got != 2 {
		t.Fatalf("owner queued notifications = %d, want 2", got)
	}
	observerMessages := steeringNotifications(observerSink.Events(), protocol.SteeringQueued)
	if len(observerMessages) != 2 || observerMessages[0].CanControl || observerMessages[1].CanControl {
		t.Fatalf("observer messages = %#v, want read-only", observerMessages)
	}

	joined, err := manager.attach(ctx, "observer", snapshot.Session.ID, true)
	if err != nil {
		t.Fatalf("reattach observer error = %v", err)
	}
	if joined.CurrentRun == nil || len(joined.CurrentRun.PendingSteering) != 2 || joined.CurrentRun.PendingSteering[0].CanControl {
		t.Fatalf("observer current run = %#v", joined.CurrentRun)
	}
	ownerSnapshot, err := manager.attach(ctx, "owner", snapshot.Session.ID, true)
	if err != nil {
		t.Fatalf("reattach owner error = %v", err)
	}
	if ownerSnapshot.CurrentRun == nil || len(ownerSnapshot.CurrentRun.PendingSteering) != 2 || !ownerSnapshot.CurrentRun.PendingSteering[0].CanControl {
		t.Fatalf("owner current run = %#v", ownerSnapshot.CurrentRun)
	}
}

func TestSteeringRejectsObserverWrongRunAndPendingInteraction(t *testing.T) {
	ctx := context.Background()
	manager := newTestSessionManager(t)
	snapshot, err := manager.create(ctx, "owner", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	if _, err := manager.attach(ctx, "observer", snapshot.Session.ID, false); err != nil {
		t.Fatalf("attach observer error = %v", err)
	}
	d := &Daemon{sessions: manager, sinks: map[string]protocol.EventSink{}}
	svc := newService(d)
	_, _, runID, err := svc.beginAgentRun(ctx, "owner")
	if err != nil {
		t.Fatalf("beginAgentRun error = %v", err)
	}
	params := protocol.SteerParams{RunID: runID, ClientMsgID: "client-1", Parts: textParts("message")}
	if _, err := svc.handleSteer(ctx, protocol.Request{ConnID: "observer", Params: params}); !requestErrorMatches(err, protocol.ErrorKindSessionBusy, protocol.ErrorReasonRunNotSteerable) {
		t.Fatalf("observer steer error = %#v", err)
	}
	params.RunID = "wrong"
	if _, err := svc.handleSteer(ctx, protocol.Request{ConnID: "owner", Params: params}); !requestErrorMatches(err, protocol.ErrorKindSessionBusy, protocol.ErrorReasonRunNotSteerable) {
		t.Fatalf("wrong run steer error = %#v", err)
	}
	manager.setWaiting(snapshot.Session.ID, protocol.RunWaitingAsk)
	params.RunID = runID
	if _, err := svc.handleSteer(ctx, protocol.Request{ConnID: "owner", Params: params}); !requestErrorMatches(err, protocol.ErrorKindSessionBusy, protocol.ErrorReasonInteractionPending) {
		t.Fatalf("pending interaction steer error = %#v", err)
	}
}

func requestErrorMatches(err error, kind protocol.ErrorKind, reason protocol.ErrorReason) bool {
	requestErr, ok := err.(*protocol.RequestError)
	if !ok {
		return false
	}
	data, ok := requestErr.Data().(protocol.ProtocolErrorData)
	return ok && data.Kind == kind && data.Reason == reason
}

func textParts(text string) []protocol.MessagePart {
	return []protocol.MessagePart{{Type: "text", Text: text}}
}

func countSteeringNotifications(events []protocol.Event, state protocol.SteeringState) int {
	return len(steeringNotifications(events, state))
}

func steeringNotifications(events []protocol.Event, state protocol.SteeringState) []protocol.SteeringMessage {
	var out []protocol.SteeringMessage
	for _, event := range events {
		if event.Method != protocol.NotifySteering {
			continue
		}
		body, _ := json.Marshal(event.Params)
		var message protocol.SteeringMessage
		if json.Unmarshal(body, &message) == nil && message.State == state {
			out = append(out, message)
		}
	}
	return out
}
