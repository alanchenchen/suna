//go:build !windows

package local

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/protocol"
)

type testService struct {
	delayFirst <-chan struct{}
	seen       atomic.Int32
	mu         sync.Mutex
	sinks      map[string]protocol.EventSink
}

func (s *testService) OnConnect(ctx context.Context, connID string, sink protocol.EventSink) {
	_ = ctx
	s.mu.Lock()
	if s.sinks == nil {
		s.sinks = make(map[string]protocol.EventSink)
	}
	s.sinks[connID] = sink
	s.mu.Unlock()
}

func (s *testService) OnDisconnect(ctx context.Context, connID string) {
	_ = ctx
	s.mu.Lock()
	delete(s.sinks, connID)
	s.mu.Unlock()
}

func (s *testService) Handle(ctx context.Context, req protocol.Request, sink protocol.EventSink) (any, error) {
	// first 请求故意阻塞，用于验证 accept loop 不会被单个长连接占住。
	_ = ctx
	_ = sink
	if req.Method == "first" && s.delayFirst != nil {
		s.seen.Add(1)
		<-s.delayFirst
	} else {
		s.seen.Add(1)
	}
	return map[string]string{"method": req.Method}, nil
}

func TestUnixTransportAcceptsSecondConnectionWhileFirstIsServing(t *testing.T) {
	socketPath := filepath.Join(os.TempDir(), fmt.Sprintf("suna-local-%d-%d.sock", os.Getpid(), time.Now().UnixNano()))
	_ = os.Remove(socketPath)
	defer os.Remove(socketPath)
	tr := NewPlatformTransport(socketPath)
	blockFirst := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer tr.Close(context.Background())
	if err := tr.Mount(ctx, &testService{delayFirst: blockFirst}); err != nil {
		t.Fatalf("Mount error = %v", err)
	}

	first, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		t.Fatalf("first Dial error = %v", err)
	}
	defer first.Close()
	if _, err := first.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"first"}` + "\n")); err != nil {
		t.Fatalf("first Write error = %v", err)
	}

	second, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		t.Fatalf("second Dial error = %v", err)
	}
	defer second.Close()
	if _, err := second.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"second"}` + "\n")); err != nil {
		t.Fatalf("second Write error = %v", err)
	}
	if err := second.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline error = %v", err)
	}
	var resp struct {
		ID     int               `json:"id"`
		Result map[string]string `json:"result"`
	}
	if err := json.NewDecoder(second).Decode(&resp); err != nil {
		t.Fatalf("second Decode error = %v", err)
	}
	if resp.ID != 2 || resp.Result["method"] != "second" {
		t.Fatalf("second response got id=%d method=%q, want id=2 method=second", resp.ID, resp.Result["method"])
	}

	close(blockFirst)
}
