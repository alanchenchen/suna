package daemon

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/alanchenchen/suna/internal/protocol"
)

func TestSessionStateErrorsMapToStableRequestKinds(t *testing.T) {
	tests := []struct {
		message string
		kind    protocol.ErrorKind
	}{
		{message: "session_required", kind: protocol.ErrorKindSessionRequired},
		{message: "session not loaded", kind: protocol.ErrorKindSessionRequired},
		{message: "session_busy", kind: protocol.ErrorKindSessionBusy},
		{message: "session not running", kind: protocol.ErrorKindSessionBusy},
	}
	for _, tt := range tests {
		err := requestErrorForState(errors.New(tt.message))
		data := err.Data().(protocol.ProtocolErrorData)
		if err.Code() != int(protocol.ErrorCodeInvalidParams) || data.Kind != tt.kind {
			t.Fatalf("requestErrorForState(%q) = code %d data %#v", tt.message, err.Code(), data)
		}
	}
}

func TestExpiredInteractionReplyReturnsInvalidRequestReason(t *testing.T) {
	svc := newService(&Daemon{state: protocol.DaemonRuntimeReady, sinks: map[string]protocol.EventSink{}})
	_, err := svc.handleAskReply(protocol.Request{Params: protocol.AskUserReply{ID: "expired", Answer: "answer"}})
	var got *protocol.RequestError
	if !errors.As(err, &got) {
		t.Fatalf("handleAskReply() error = %T %v, want *protocol.RequestError", err, err)
	}
	data, ok := got.Data().(protocol.ProtocolErrorData)
	if got.Code() != int(protocol.ErrorCodeInvalidParams) || !ok || data.Kind != protocol.ErrorKindInvalidRequest || data.Reason != protocol.ErrorReasonInteractionNotFound {
		t.Fatalf("interaction error = code %d data %#v", got.Code(), got.Data())
	}
}

func TestServiceExposesStartingStateButRejectsBusinessRequests(t *testing.T) {
	d := &Daemon{state: protocol.DaemonRuntimeStarting, sinks: map[string]protocol.EventSink{}}
	svc := newService(d)
	result, err := svc.Handle(context.Background(), protocol.Request{Method: protocol.MethodDaemonStatus}, nil)
	if err != nil {
		t.Fatalf("daemon.status error = %v", err)
	}
	status, ok := result.(protocol.DaemonStatusParams)
	if !ok || status.State != protocol.DaemonRuntimeStarting {
		t.Fatalf("daemon.status = %#v, want starting", result)
	}

	_, err = svc.Handle(context.Background(), protocol.Request{Method: protocol.MethodSessionList}, nil)
	if err == nil {
		t.Fatal("session.list error = nil, want runtime unavailable")
	}
	dataErr, ok := err.(interface{ Data() any })
	if !ok {
		t.Fatalf("session.list error = %T, want data error", err)
	}
	data, ok := dataErr.Data().(protocol.ProtocolErrorData)
	if !ok || data.Kind != protocol.ErrorKindRuntimeUnavailable || data.Reason != protocol.ErrorReasonRuntimeStarting || !data.Retryable {
		t.Fatalf("error data = %#v, want retryable runtime_unavailable", dataErr.Data())
	}
}

func TestStopPublishesStoppingBeforeCancellation(t *testing.T) {
	d := &Daemon{state: protocol.DaemonRuntimeReady, sinks: map[string]protocol.EventSink{}}
	cancelled := false
	d.cancelFn = func() {
		cancelled = true
		if got := d.RuntimeState(); got != protocol.DaemonRuntimeStopping {
			t.Fatalf("state during cancellation = %q, want stopping", got)
		}
	}

	d.Stop()
	if !cancelled {
		t.Fatal("cancel callback was not called")
	}
	if got := d.RuntimeState(); got != protocol.DaemonRuntimeStopping {
		t.Fatalf("state after Stop() = %q, want stopping", got)
	}
	_, err := newService(d).Handle(context.Background(), protocol.Request{Method: protocol.MethodSessionList}, nil)
	if err == nil {
		t.Fatal("session.list error = nil, want runtime unavailable while stopping")
	}
	dataErr, ok := err.(interface{ Data() any })
	if !ok {
		t.Fatalf("session.list error = %T, want data error", err)
	}
	data, ok := dataErr.Data().(protocol.ProtocolErrorData)
	if !ok || data.Kind != protocol.ErrorKindRuntimeUnavailable || data.Reason != protocol.ErrorReasonRuntimeStopping || data.Retryable {
		t.Fatalf("error data = %#v, want non-retryable stopping runtime_unavailable", dataErr.Data())
	}
}

func TestRuntimeHelloReturnsRuntimeCatalogWithoutVersionGate(t *testing.T) {
	d := &Daemon{state: protocol.DaemonRuntimeStarting, sinks: map[string]protocol.EventSink{}}
	svc := newService(d)
	result, err := svc.Handle(context.Background(), protocol.Request{Method: protocol.MethodRuntimeHello, Params: protocol.RuntimeHelloParams{
		Transport: "tcp",
		Client:    protocol.RuntimeClient{Name: "example-ui", Version: "1.0.0"},
	}}, nil)
	if err != nil {
		t.Fatalf("runtime.hello error = %v", err)
	}
	hello := result.(protocol.RuntimeHelloResult)
	if hello.RuntimeVersion == "" || hello.Transport != "tcp" {
		t.Fatalf("runtime.hello = %#v", hello)
	}
	if !slices.Contains(hello.Catalog.Methods, protocol.MethodSendMessage) ||
		!slices.Contains(hello.Catalog.Notifications, protocol.NotifyAgentRun) ||
		!slices.Contains(hello.Catalog.Features, protocol.FeatureRunSteeringText) {
		t.Fatalf("runtime.hello catalog = %#v", hello.Catalog)
	}
}
