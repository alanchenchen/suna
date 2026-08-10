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
	"syscall"
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

func unixTestSocketPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(os.TempDir(), fmt.Sprintf("suna-local-%d-%d.sock", os.Getpid(), time.Now().UnixNano()))
	_ = os.Remove(path)
	t.Cleanup(func() { _ = os.Remove(path) })
	return path
}

func TestStaleUnixSocketErrorOnlyAcceptsMissingOrRefused(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "refused", err: &net.OpError{Err: syscall.ECONNREFUSED}, want: true},
		{name: "missing", err: &net.OpError{Err: syscall.ENOENT}, want: true},
		{name: "timeout", err: &net.OpError{Err: os.ErrDeadlineExceeded}, want: false},
		{name: "permission", err: &net.OpError{Err: syscall.EACCES}, want: false},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if got := isStaleUnixSocketError(tt.err); got != tt.want {
				t.Fatalf("isStaleUnixSocketError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUnixTransportRejectsActiveSocketWithoutReplacingIt(t *testing.T) {
	socketPath := unixTestSocketPath(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := NewPlatformTransport(socketPath)
	defer first.Close(context.Background())
	if err := first.Mount(ctx, &testService{}); err != nil {
		t.Fatalf("first Mount() error = %v", err)
	}
	before, err := os.Lstat(socketPath)
	if err != nil {
		t.Fatalf("Lstat() error = %v", err)
	}

	second := NewPlatformTransport(socketPath)
	if err := second.Mount(ctx, &testService{}); err == nil {
		_ = second.Close(context.Background())
		t.Fatal("second Mount() error = nil, want active socket conflict")
	}
	after, err := os.Lstat(socketPath)
	if err != nil {
		t.Fatalf("Lstat() after conflict error = %v", err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("active socket was replaced by second Mount()")
	}
	conn, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		t.Fatalf("Dial() after conflict error = %v", err)
	}
	_ = conn.Close()
}

func TestUnixTransportReplacesStaleSocket(t *testing.T) {
	socketPath := unixTestSocketPath(t)
	stale, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Listen() stale socket error = %v", err)
	}
	unixStale, ok := stale.(*net.UnixListener)
	if !ok {
		t.Fatal("stale listener is not a UnixListener")
	}
	unixStale.SetUnlinkOnClose(false)
	if err := stale.Close(); err != nil {
		t.Fatalf("Close() stale listener error = %v", err)
	}
	if _, err := os.Lstat(socketPath); err != nil {
		t.Fatalf("stale socket missing: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr := NewPlatformTransport(socketPath)
	defer tr.Close(context.Background())
	if err := tr.Mount(ctx, &testService{}); err != nil {
		t.Fatalf("Mount() error = %v", err)
	}
	conn, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		t.Fatalf("Dial() replacement error = %v", err)
	}
	_ = conn.Close()
}

func TestUnixTransportCloseDoesNotRemoveReplacementSocket(t *testing.T) {
	socketPath := unixTestSocketPath(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr := NewPlatformTransport(socketPath)
	if err := tr.Mount(ctx, &testService{}); err != nil {
		t.Fatalf("Mount() error = %v", err)
	}
	if err := os.Remove(socketPath); err != nil {
		t.Fatalf("Remove() owned socket error = %v", err)
	}
	replacement, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Listen() replacement error = %v", err)
	}
	defer replacement.Close()
	if err := tr.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := os.Lstat(socketPath); err != nil {
		t.Fatalf("replacement socket was removed: %v", err)
	}
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
