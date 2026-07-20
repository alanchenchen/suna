package tcp

import (
	"context"
	"encoding/json"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/protocol"
)

type testService struct {
	mu           sync.Mutex
	connected    int
	disconnected int
	lastRequest  protocol.Request
	lastContext  context.Context
}

func (s *testService) OnConnect(_ context.Context, _ string, _ protocol.EventSink) {
	s.mu.Lock()
	s.connected++
	s.mu.Unlock()
}

func (s *testService) OnDisconnect(_ context.Context, _ string) {
	s.mu.Lock()
	s.disconnected++
	s.mu.Unlock()
}

func (s *testService) Handle(ctx context.Context, req protocol.Request, _ protocol.EventSink) (any, error) {
	s.mu.Lock()
	s.lastRequest = req
	s.lastContext = ctx
	s.mu.Unlock()
	return map[string]string{"method": req.Method}, nil
}

func TestNewDefaultFallsBackToRandomPortWhenDefaultIsBusy(t *testing.T) {
	busy, err := net.Listen("tcp", DefaultEndpoint)
	if err != nil {
		t.Skipf("default endpoint is unavailable for conflict test: %v", err)
	}
	defer busy.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr := NewDefault()
	defer tr.Close(context.Background())
	if err := tr.Mount(ctx, &testService{}); err != nil {
		t.Fatalf("Mount error = %v", err)
	}
	if got := tr.Endpoint(); got == DefaultEndpoint {
		t.Fatalf("Endpoint = %q, want fallback endpoint", got)
	}
	conn, err := net.DialTimeout("tcp", tr.Endpoint(), time.Second)
	if err != nil {
		t.Fatalf("Dial fallback endpoint error = %v", err)
	}
	defer conn.Close()
}

func TestExplicitPortConflictFailsWithoutFallback(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen error = %v", err)
	}
	defer busy.Close()

	tr := New(busy.Addr().String())
	if err := tr.Mount(context.Background(), &testService{}); err == nil {
		t.Fatal("Mount error = nil, want explicit port conflict")
	}
}

func TestTransportRejectsNonLoopbackEndpoint(t *testing.T) {
	if err := ValidateEndpoint("0.0.0.0:7632"); err == nil {
		t.Fatal("ValidateEndpoint accepted non-loopback address")
	}
	if err := ValidateEndpoint("127.0.0.1:7632"); err != nil {
		t.Fatalf("ValidateEndpoint loopback error = %v", err)
	}
}

func TestTransportDoesNotCountConnectionBeforeHello(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr := New("127.0.0.1:17631")
	defer tr.Close(context.Background())
	if err := tr.Mount(ctx, &testService{}); err != nil {
		t.Fatalf("Mount error = %v", err)
	}
	conn, err := net.DialTimeout("tcp", tr.Endpoint(), time.Second)
	if err != nil {
		t.Fatalf("Dial error = %v", err)
	}
	defer conn.Close()
	time.Sleep(10 * time.Millisecond)
	if got := tr.ConnectionCount(); got != 0 {
		t.Fatalf("ConnectionCount before hello = %d, want 0", got)
	}
}

func TestTransportServesJSONRPCOverTCP(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tr := New("127.0.0.1:17631")
	defer tr.Close(context.Background())
	service := &testService{}
	if err := tr.Mount(ctx, service); err != nil {
		t.Fatalf("Mount error = %v", err)
	}
	if got := tr.Info().Retention; got != protocol.RetentionIdleExit {
		t.Fatalf("Retention = %q, want %q", got, protocol.RetentionIdleExit)
	}
	if got := tr.Info().IdleTimeout; got != 2*time.Second {
		t.Fatalf("IdleTimeout = %s, want %s", got, 2*time.Second)
	}

	conn, err := net.DialTimeout("tcp", tr.Endpoint(), time.Second)
	if err != nil {
		t.Fatalf("Dial error = %v", err)
	}
	defer conn.Close()
	decoder := json.NewDecoder(conn)

	if _, err := conn.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"runtime.hello","params":{"protocol_version":"0.2"}}` + "\n")); err != nil {
		t.Fatalf("hello Write error = %v", err)
	}
	assertHelloResponse(t, decoder, 1)

	if _, err := conn.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"echo"}` + "\n")); err != nil {
		t.Fatalf("echo Write error = %v", err)
	}
	assertMethodResponse(t, decoder, 2, "echo")
	service.mu.Lock()
	gotTransport := protocol.TransportFromContext(service.lastContext)
	service.mu.Unlock()
	if got, want := gotTransport, "tcp"; got != want {
		t.Fatalf("request transport = %q, want %q", got, want)
	}
}

func assertHelloResponse(t *testing.T, decoder *json.Decoder, id int) {
	t.Helper()
	var response struct {
		ID     int `json:"id"`
		Result struct {
			Transport string `json:"transport"`
		} `json:"result"`
	}
	if err := decoder.Decode(&response); err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	if got := response.ID; got != id {
		t.Fatalf("response id = %d, want %d", got, id)
	}
	if got := response.Result.Transport; got != "" {
		t.Fatalf("hello transport = %q, want empty for a standalone test service", got)
	}
}

func assertMethodResponse(t *testing.T, decoder *json.Decoder, id int, method string) {
	t.Helper()
	var response struct {
		ID     int               `json:"id"`
		Result map[string]string `json:"result"`
	}
	if err := decoder.Decode(&response); err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	if got := response.ID; got != id {
		t.Fatalf("response id = %d, want %d", got, id)
	}
	if got := response.Result["method"]; got != method {
		t.Fatalf("response method = %q, want %q", got, method)
	}
}

func TestTransportRequiresHelloBeforeRequests(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr := New("127.0.0.1:17631")
	defer tr.Close(context.Background())
	if err := tr.Mount(ctx, &testService{}); err != nil {
		t.Fatalf("Mount error = %v", err)
	}

	conn, err := net.DialTimeout("tcp", tr.Endpoint(), time.Second)
	if err != nil {
		t.Fatalf("Dial error = %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"echo"}` + "\n")); err != nil {
		t.Fatalf("Write error = %v", err)
	}
	var response struct {
		Error *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		t.Fatalf("Decode error = %v", err)
	}
	if response.Error == nil || response.Error.Code != -32010 {
		t.Fatalf("handshake error = %#v, want code -32010", response.Error)
	}
}
