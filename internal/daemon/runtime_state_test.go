package daemon

import (
	"context"
	"testing"

	"github.com/alanchenchen/suna/internal/protocol"
	transportjsonrpc "github.com/alanchenchen/suna/internal/transport/jsonrpc"
)

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

func TestRuntimeHelloUsesProtocolVersion04(t *testing.T) {
	d := &Daemon{state: protocol.DaemonRuntimeStarting, sinks: map[string]protocol.EventSink{}}
	svc := newService(d)
	result, err := svc.Handle(context.Background(), protocol.Request{Method: protocol.MethodRuntimeHello, Params: protocol.RuntimeHelloParams{ProtocolVersion: "0.4", Transport: "tcp"}}, nil)
	if err != nil {
		t.Fatalf("runtime.hello error = %v", err)
	}
	hello := result.(protocol.RuntimeHelloResult)
	if hello.ProtocolVersion != "0.4" || !hello.Capabilities["mcp_status_updates"] {
		t.Fatalf("runtime.hello = %#v", hello)
	}

	_, err = svc.Handle(context.Background(), protocol.Request{Method: protocol.MethodRuntimeHello, Params: protocol.RuntimeHelloParams{ProtocolVersion: "0.3"}}, nil)
	if err == nil {
		t.Fatal("runtime.hello 0.3 error = nil, want rejection")
	}
	if coded, ok := err.(transportjsonrpc.CodedError); !ok || coded.Code() != -32602 {
		t.Fatalf("runtime.hello 0.3 error = %#v, want -32602", err)
	}
}
