//go:build windows

package local

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"github.com/Microsoft/go-winio"
	"github.com/google/uuid"

	"github.com/alanchenchen/suna/internal/protocol"
)

type NamedPipeTransport struct {
	pipePath string
	listener net.Listener
	svc      protocol.Service
	ctx      context.Context
	cancel   context.CancelFunc
	conns    map[string]*pipeConn
	mu       sync.Mutex
	closed   atomic.Bool
}

type pipeConn struct {
	id   string
	conn net.Conn
	mu   sync.Mutex
}

// NewPlatformTransport 在 Windows 平台使用 Named Pipe；平台选择由文件名和 build tag 在编译期完成。
func NewPlatformTransport(pipePath string) *NamedPipeTransport {
	return &NamedPipeTransport{pipePath: pipePath, conns: make(map[string]*pipeConn)}
}

func (t *NamedPipeTransport) Name() string { return "local" }

func (t *NamedPipeTransport) Mount(ctx context.Context, svc protocol.Service) error {
	t.svc = svc
	t.ctx, t.cancel = context.WithCancel(ctx)
	// DACL 仅允许当前用户连接，和 Unix socket 0600 权限保持同一安全语义。
	listener, err := winio.ListenPipe(t.pipePath, &winio.PipeConfig{SecurityDescriptor: "D:P(A;;GA;;;CO)"})
	if err != nil {
		return fmt.Errorf("listen named pipe: %w", err)
	}
	t.listener = listener
	go t.acceptLoop()
	return nil
}

func (t *NamedPipeTransport) acceptLoop() {
	for {
		select {
		case <-t.ctx.Done():
			return
		default:
		}
		conn, err := t.listener.Accept()
		if err != nil {
			if t.closed.Load() {
				return
			}
			continue
		}
		pc := &pipeConn{id: uuid.New().String()[:8], conn: conn}
		t.mu.Lock()
		t.conns[pc.id] = pc
		t.mu.Unlock()
		// 每个连接独立处理 JSON-RPC 流，业务逻辑通过 protocol.Service 进入 daemon。
		go serveConn(t.ctx, pc, t.svc, func() {
			t.mu.Lock()
			delete(t.conns, pc.id)
			t.mu.Unlock()
			pc.Close()
		})
	}
}

func (t *NamedPipeTransport) Close(ctx context.Context) error {
	t.closed.Store(true)
	if t.cancel != nil {
		t.cancel()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, c := range t.conns {
		c.conn.Close()
	}
	t.conns = make(map[string]*pipeConn)
	if t.listener != nil {
		t.listener.Close()
	}
	_ = ctx
	return nil
}

func (t *NamedPipeTransport) ConnectionCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.conns)
}

func (c *pipeConn) ID() string { return c.id }

func (c *pipeConn) Send(ctx context.Context, msg []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	done := make(chan error, 1)
	go func() {
		data := append(msg, '\n')
		_, err := c.conn.Write(data)
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *pipeConn) Receive() ([]byte, error) {
	var buf [1]byte
	var line []byte
	for {
		_, err := c.conn.Read(buf[:])
		if err != nil {
			return nil, err
		}
		if buf[0] == '\n' {
			if len(line) > 0 {
				return line, nil
			}
			continue
		}
		line = append(line, buf[0])
	}
}

func (c *pipeConn) Close() error { return c.conn.Close() }
