//go:build !windows

package local

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/protocol"
)

type UnixSocketTransport struct {
	socketPath string
	listener   net.Listener
	svc        protocol.Service
	ctx        context.Context
	cancel     context.CancelFunc
	conns      map[string]*socketConn
	mu         sync.Mutex
	closed     atomic.Bool
}

type socketConn struct {
	id     string
	conn   net.Conn
	reader *bufio.Reader
	mu     sync.Mutex
}

// DefaultEndpoint 返回当前平台 local transport 使用的默认监听地址。
func DefaultEndpoint() string {
	return config.DefaultSocketPath()
}

func platformDial(endpoint string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", endpoint, timeout)
}

// NewPlatformTransport 在 Unix-like 平台使用 Unix domain socket；平台选择由文件名和 build tag 在编译期完成。
func NewPlatformTransport(socketPath string) *UnixSocketTransport {
	return &UnixSocketTransport{socketPath: socketPath, conns: make(map[string]*socketConn)}
}

func (t *UnixSocketTransport) Name() string { return "local" }

func (t *UnixSocketTransport) Mount(ctx context.Context, svc protocol.Service) error {
	t.svc = svc
	t.ctx, t.cancel = context.WithCancel(ctx)
	// 启动前清理残留 socket；如果残留 socket 仍可连接，说明已有 daemon 在运行。
	if err := os.Remove(t.socketPath); err != nil && !os.IsNotExist(err) {
		conn, err := net.DialTimeout("unix", t.socketPath, 2*time.Second)
		if err == nil {
			conn.Close()
			return fmt.Errorf("daemon already running (socket %s is active)", t.socketPath)
		}
		os.Remove(t.socketPath)
	}
	if err := os.MkdirAll(filepath.Dir(t.socketPath), 0755); err != nil {
		return fmt.Errorf("create socket dir: %w", err)
	}
	listener, err := net.Listen("unix", t.socketPath)
	if err != nil {
		return fmt.Errorf("listen unix socket: %w", err)
	}
	// socket 权限限制为当前用户可读写，避免其他本机用户连接 daemon。
	os.Chmod(t.socketPath, 0600)
	t.listener = listener
	go t.acceptLoop()
	return nil
}

func (t *UnixSocketTransport) acceptLoop() {
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
		sc := &socketConn{id: uuid.New().String()[:8], conn: conn, reader: bufio.NewReader(conn)}
		t.mu.Lock()
		t.conns[sc.id] = sc
		t.mu.Unlock()
		// 每个连接独立处理 JSON-RPC 流，业务逻辑通过 protocol.Service 进入 daemon。
		go serveConn(t.ctx, sc, t.svc, func() {
			t.mu.Lock()
			delete(t.conns, sc.id)
			t.mu.Unlock()
			sc.Close()
		})
	}
}

func (t *UnixSocketTransport) Close(ctx context.Context) error {
	t.closed.Store(true)
	if t.cancel != nil {
		t.cancel()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, c := range t.conns {
		c.conn.Close()
	}
	t.conns = make(map[string]*socketConn)
	if t.listener != nil {
		t.listener.Close()
	}
	os.Remove(t.socketPath)
	_ = ctx
	return nil
}

func (t *UnixSocketTransport) ConnectionCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.conns)
}

func (c *socketConn) ID() string { return c.id }

func (c *socketConn) Send(ctx context.Context, msg []byte) error {
	return sendFrame(ctx, &c.mu, c.conn, msg)
}

func (c *socketConn) Receive() ([]byte, error) {
	return receiveFrame(c.reader)
}

func (c *socketConn) Close() error { return c.conn.Close() }
