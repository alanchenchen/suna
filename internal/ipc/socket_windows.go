//go:build windows

package ipc

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"github.com/Microsoft/go-winio"
	"github.com/google/uuid"
)

// NamedPipeTransport Windows Named Pipe 传输层实现
type NamedPipeTransport struct {
	pipePath string
	listener net.Listener
	connCB   func(Conn)
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

func NewPlatformTransport(pipePath string) *NamedPipeTransport {
	return &NamedPipeTransport{
		pipePath: pipePath,
		conns:    make(map[string]*pipeConn),
	}
}

func (t *NamedPipeTransport) Listen(ctx context.Context) error {
	t.ctx, t.cancel = context.WithCancel(ctx)

	// DACL 仅允许当前用户连接
	cfg := &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;CO)",
	}

	listener, err := winio.ListenPipe(t.pipePath, cfg)
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

		pc := &pipeConn{
			id:   uuid.New().String()[:8],
			conn: conn,
		}

		t.mu.Lock()
		t.conns[pc.id] = pc
		t.mu.Unlock()

		if t.connCB != nil {
			t.connCB(pc)
		}
	}
}

func (t *NamedPipeTransport) Close() error {
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
	return nil
}

func (t *NamedPipeTransport) OnConnect(cb func(Conn)) {
	t.connCB = cb
}

func (t *NamedPipeTransport) RemoveConn(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.conns, id)
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

func (c *pipeConn) Close() error {
	return c.conn.Close()
}
