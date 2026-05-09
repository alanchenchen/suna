//go:build !windows

package ipc

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// UnixSocketTransport Unix Domain Socket 传输层实现
// 用于 macOS 和 Linux
type UnixSocketTransport struct {
	socketPath string
	listener   net.Listener
	connCB     func(Conn)
	ctx        context.Context
	cancel     context.CancelFunc
	conns      map[string]*socketConn
	mu         sync.Mutex
	closed     atomic.Bool
}

type socketConn struct {
	id   string
	conn net.Conn
	mu   sync.Mutex
}

func NewUnixSocketTransport(socketPath string) *UnixSocketTransport {
	return &UnixSocketTransport{
		socketPath: socketPath,
		conns:      make(map[string]*socketConn),
	}
}

// NewPlatformTransport 创建平台对应的 Transport（Unix Socket 或 Named Pipe）
func NewPlatformTransport(socketPath string) *UnixSocketTransport {
	return NewUnixSocketTransport(socketPath)
}

func (t *UnixSocketTransport) Listen(ctx context.Context) error {
	t.ctx, t.cancel = context.WithCancel(ctx)

	// 清理残留 socket 文件
	if err := os.Remove(t.socketPath); err != nil && !os.IsNotExist(err) {
		// 残留文件可能是活跃的 daemon，尝试连接检测
		conn, err := net.DialTimeout("unix", t.socketPath, 2*time.Second)
		if err == nil {
			conn.Close()
			return fmt.Errorf("daemon already running (socket %s is active)", t.socketPath)
		}
		// 连不上 → 残留文件 → 删除
		os.Remove(t.socketPath)
	}

	// 确保 socket 目录存在
	if err := os.MkdirAll(filepath.Dir(t.socketPath), 0755); err != nil {
		return fmt.Errorf("create socket dir: %w", err)
	}

	listener, err := net.Listen("unix", t.socketPath)
	if err != nil {
		return fmt.Errorf("listen unix socket: %w", err)
	}

	// socket 文件权限 0600：只有当前用户可连接
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

		sc := &socketConn{
			id:   uuid.New().String()[:8],
			conn: conn,
		}

		t.mu.Lock()
		t.conns[sc.id] = sc
		t.mu.Unlock()

		if t.connCB != nil {
			t.connCB(sc)
		}
	}
}

func (t *UnixSocketTransport) Close() error {
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
	return nil
}

func (t *UnixSocketTransport) OnConnect(cb func(Conn)) {
	t.connCB = cb
}

// RemoveConn 从连接池移除（连接断开时调用）
func (t *UnixSocketTransport) RemoveConn(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.conns, id)
}

// socketConn 实现 Conn 接口

func (c *socketConn) ID() string { return c.id }

func (c *socketConn) Send(ctx context.Context, msg []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 带超时的写入
	done := make(chan error, 1)
	go func() {
		// NDJSON: 每条消息一行
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

func (c *socketConn) Receive() ([]byte, error) {
	// NDJSON: 按行读取
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

func (c *socketConn) Close() error {
	return c.conn.Close()
}
