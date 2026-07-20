package daemon

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/alanchenchen/suna/internal/protocol"
)

func TestHandleDebugMemoryRejectsTCP(t *testing.T) {
	s := &service{}
	ctx := protocol.WithTransport(context.Background(), "tcp")
	_, err := s.handleDebugMemory(ctx, protocol.Request{})
	if err == nil {
		t.Fatal("handleDebugMemory error = nil, want local-only rejection")
	}
}

func TestHandleDebugMemoryReturnsRuntimeStats(t *testing.T) {
	s := &service{}
	ctx := protocol.WithTransport(context.Background(), "local")
	result, err := s.handleDebugMemory(ctx, protocol.Request{Params: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatalf("handleDebugMemory error = %v", err)
	}
	if result.PID <= 0 {
		t.Fatalf("PID = %d, want positive", result.PID)
	}
	if result.Timestamp.IsZero() {
		t.Fatal("Timestamp is zero")
	}
	if result.Sys == 0 {
		t.Fatal("Sys = 0, want runtime memory stats")
	}
}
