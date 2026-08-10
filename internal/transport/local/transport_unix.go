//go:build !windows

package local

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/protocol"
	transportjsonrpc "github.com/alanchenchen/suna/internal/transport/jsonrpc"
)

type UnixSocketTransport struct {
	socketPath string
	listener   net.Listener
	socketInfo os.FileInfo
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

func (t *UnixSocketTransport) Info() protocol.TransportInfo {
	// 官方 TUI 使用后台 local daemon，最后一个客户端断开后保留短暂宽限期，便于 status/stop 等短连接复用。
	return protocol.TransportInfo{Retention: protocol.RetentionIdleExit, IdleTimeout: 2 * time.Second}
}

func (t *UnixSocketTransport) Mount(ctx context.Context, svc protocol.Service) error {
	t.svc = svc
	t.ctx, t.cancel = context.WithCancel(ctx)
	if err := os.MkdirAll(filepath.Dir(t.socketPath), 0755); err != nil {
		return fmt.Errorf("create socket dir: %w", err)
	}
	// Unix 允许 unlink 正在监听的 socket，因此必须先探测活跃端点；只清理仍是同一文件的 stale socket。
	if info, err := os.Lstat(t.socketPath); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("local endpoint %s exists and is not a socket", t.socketPath)
		}
		conn, dialErr := net.DialTimeout("unix", t.socketPath, 2*time.Second)
		if dialErr == nil {
			_ = conn.Close()
			return fmt.Errorf("daemon already running (socket %s is active)", t.socketPath)
		}
		if !isStaleUnixSocketError(dialErr) {
			return fmt.Errorf("probe unix socket %s: %w", t.socketPath, dialErr)
		}
		current, statErr := os.Lstat(t.socketPath)
		if statErr == nil && os.SameFile(info, current) {
			if err := os.Remove(t.socketPath); err != nil {
				return fmt.Errorf("remove stale unix socket: %w", err)
			}
		} else if statErr == nil {
			return fmt.Errorf("local endpoint %s changed while checking stale socket", t.socketPath)
		} else if !os.IsNotExist(statErr) {
			return fmt.Errorf("stat unix socket: %w", statErr)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat unix socket: %w", err)
	}
	listener, err := net.Listen("unix", t.socketPath)
	if err != nil {
		return fmt.Errorf("listen unix socket: %w", err)
	}
	if unixListener, ok := listener.(*net.UnixListener); ok {
		// 标准库默认在 Close 时按路径 unlink；关闭隐式清理，避免旧 listener 删除后来替换的 socket。
		unixListener.SetUnlinkOnClose(false)
	}
	info, err := os.Lstat(t.socketPath)
	if err != nil {
		_ = listener.Close()
		return fmt.Errorf("stat mounted unix socket: %w", err)
	}
	// socket 权限限制为当前用户可读写，避免其他本机用户连接 daemon。
	if err := os.Chmod(t.socketPath, 0600); err != nil {
		_ = listener.Close()
		removeSocketIfSame(t.socketPath, info)
		return fmt.Errorf("chmod unix socket: %w", err)
	}
	t.listener = listener
	t.socketInfo = info
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
		// 每个连接必须独立 goroutine 处理；否则首个长连接会阻塞 accept loop，导致 status/stop 等后续 local 请求超时。
		go transportjsonrpc.ServeConn(t.ctx, sc, t.svc, transportjsonrpc.Options{Transport: t.Name()}, func() {
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
	removeSocketIfSame(t.socketPath, t.socketInfo)
	t.socketInfo = nil
	_ = ctx
	return nil
}

func isStaleUnixSocketError(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ENOENT)
}

func removeSocketIfSame(path string, owned os.FileInfo) {
	if owned == nil {
		return
	}
	current, err := os.Lstat(path)
	if err == nil && os.SameFile(owned, current) {
		_ = os.Remove(path)
	}
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
