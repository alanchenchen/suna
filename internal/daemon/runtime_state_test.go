package daemon

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/alanchenchen/suna/internal/protocol"
)

func TestExpiredInteractionReplyReturnsInvalidRequestReason(t *testing.T) {
	svc := newService(&Daemon{state: protocol.DaemonRuntimeReady, sinks: map[string]protocol.EventSink{}})
	_, err := svc.handleAskReply(protocol.Request{Params: protocol.AskUserReply{ID: "expired", Answer: "answer"}})
	var got protocolError
	if !errors.As(err, &got) {
		t.Fatalf("handleAskReply() error = %T %v, want protocolError", err, err)
	}
	data, ok := got.Data().(protocol.ProtocolErrorData)
	if got.Code() != -32602 || !ok || data.Kind != "invalid_request" || data.Reason != "interaction_not_found" {
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
	if !ok || data.Kind != "runtime_unavailable" || data.Reason != "starting" || !data.Retryable {
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
	if !ok || data.Kind != "runtime_unavailable" || data.Reason != "stopping" || data.Retryable {
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
