package tcp

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/alanchenchen/suna/internal/protocol"
	transportjsonrpc "github.com/alanchenchen/suna/internal/transport/jsonrpc"
)

const (
	DefaultEndpoint            = "127.0.0.1:7632"
	unauthenticatedReadTimeout = 5 * time.Second
)

// Transport 通过受信任的 TCP 长连接暴露统一 JSON-RPC protocol。
// 鉴权、TLS 和公网访问策略由外部 gateway 或部署环境负责。
type Transport struct {
	endpoint         string
	fallbackToRandom bool
	listener         net.Listener
	svc              protocol.Service
	ctx              context.Context
	cancel           context.CancelFunc
	conns            map[string]*conn
	mu               sync.Mutex
	closed           atomic.Bool
}

type conn struct {
	id     string
	conn   net.Conn
	reader *bufio.Reader
	mu     sync.Mutex
}

func New(endpoint string) *Transport {
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	return &Transport{endpoint: endpoint, conns: make(map[string]*conn)}
}

// NewDefault 创建默认 TCP listener；默认端口被占用时自动退回 loopback 随机端口，
// 由 daemon.status 和 suna serve --json 暴露实际 endpoint。
func NewDefault() *Transport {
	tr := New(DefaultEndpoint)
	tr.fallbackToRandom = true
	return tr
}

func (t *Transport) Name() string { return "tcp" }

func (t *Transport) Info() protocol.TransportInfo {
	return protocol.TransportInfo{Retention: protocol.RetentionIdleExit, IdleTimeout: 2 * time.Second}
}

// Endpoint 返回实际监听地址；默认端口冲突回退随机端口后，这里返回实际分配的地址。
func (t *Transport) Endpoint() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.listener != nil {
		return t.listener.Addr().String()
	}
	return t.endpoint
}

func (t *Transport) Mount(ctx context.Context, svc protocol.Service) error {
	if err := ValidateEndpoint(t.endpoint); err != nil {
		return err
	}
	t.ctx, t.cancel = context.WithCancel(ctx)
	listener, err := net.Listen("tcp", t.endpoint)
	if err != nil && t.fallbackToRandom {
		listener, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		t.cancel()
		return fmt.Errorf("listen tcp %s: %w", t.endpoint, err)
	}
	t.mu.Lock()
	t.listener = listener
	t.svc = svc
	t.mu.Unlock()
	go t.acceptLoop(listener)
	return nil
}

func (t *Transport) acceptLoop(listener net.Listener) {
	for {
		accepted, err := listener.Accept()
		if err != nil {
			if t.closed.Load() || t.ctx.Err() != nil {
				return
			}
			continue
		}
		_ = accepted.SetDeadline(time.Now().Add(unauthenticatedReadTimeout))
		c := &conn{id: uuid.New().String()[:8], conn: accepted, reader: bufio.NewReader(accepted)}
		go transportjsonrpc.ServeConn(t.ctx, c, t.svc, transportjsonrpc.Options{RequireHello: true, Transport: t.Name(), OnHandshake: func() {
			_ = accepted.SetDeadline(time.Time{})
			t.mu.Lock()
			t.conns[c.id] = c
			t.mu.Unlock()
		}}, func() {
			t.mu.Lock()
			delete(t.conns, c.id)
			t.mu.Unlock()
			_ = c.Close()
		})
	}
}

func ValidateEndpoint(endpoint string) error {
	host, portText, err := net.SplitHostPort(endpoint)
	if err != nil {
		return fmt.Errorf("invalid TCP listen address %q: %w", endpoint, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return fmt.Errorf("TCP listen address %q must use a port from 1 to 65535", endpoint)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("TCP listen address %q must use a loopback host", endpoint)
	}
	return nil
}

func (t *Transport) Close(ctx context.Context) error {
	t.closed.Store(true)
	if t.cancel != nil {
		t.cancel()
	}
	t.mu.Lock()
	for _, c := range t.conns {
		_ = c.conn.Close()
	}
	t.conns = make(map[string]*conn)
	listener := t.listener
	t.listener = nil
	t.mu.Unlock()
	if listener != nil {
		_ = listener.Close()
	}
	_ = ctx
	return nil
}

func (t *Transport) ConnectionCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.conns)
}

func (c *conn) ID() string { return c.id }

func (c *conn) Send(ctx context.Context, msg []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.conn.SetWriteDeadline(deadline)
		defer c.conn.SetWriteDeadline(time.Time{})
	}
	data := append(append(make([]byte, 0, len(msg)+1), msg...), '\n')
	for len(data) > 0 {
		n, err := c.conn.Write(data)
		if err != nil {
			return err
		}
		data = data[n:]
	}
	return nil
}

func (c *conn) Receive() ([]byte, error) {
	for {
		line, err := c.reader.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		line = bytes.TrimRight(line, "\r\n")
		if len(line) > 0 {
			return line, nil
		}
	}
}

func (c *conn) Close() error { return c.conn.Close() }
