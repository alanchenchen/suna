//go:build windows

package local

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

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
	id     string
	conn   net.Conn
	reader *bufio.Reader
	mu     sync.Mutex
}

// DefaultEndpoint 返回当前平台 local transport 使用的默认监听地址。
func DefaultEndpoint() string {
	return `\\.\pipe\sunad-` + currentUserPipeSuffix()
}

func currentUserPipeSuffix() string {
	// Windows Named Pipe 位于全局命名空间。把当前用户目录哈希进 pipe 名，
	// 避免不同用户会话、旧发行版或残留实例抢占同一个 \\.\pipe\sunad；
	// UserHomeDir 在少数环境不可用时，回退到 Windows 用户相关环境变量。
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.Getenv("USERPROFILE")
	}
	if home == "" {
		home = os.Getenv("USERNAME")
	}
	if home == "" {
		return "default"
	}
	sum := sha1.Sum([]byte(home))
	return hex.EncodeToString(sum[:])[:12]
}

func platformDial(endpoint string, timeout time.Duration) (net.Conn, error) {
	return winio.DialPipe(endpoint, &timeout)
}

// NewPlatformTransport 在 Windows 平台使用 Named Pipe；平台选择由文件名和 build tag 在编译期完成。
func NewPlatformTransport(pipePath string) *NamedPipeTransport {
	return &NamedPipeTransport{pipePath: pipePath, conns: make(map[string]*pipeConn)}
}

func (t *NamedPipeTransport) Name() string { return "local" }

func (t *NamedPipeTransport) Mount(ctx context.Context, svc protocol.Service) error {
	t.svc = svc
	t.ctx, t.cancel = context.WithCancel(ctx)
	// 使用 go-winio 默认 Named Pipe ACL。此前 CO-only SDDL 在部分 Windows 环境下
	// 会让同一用户客户端 DialPipe 返回 Access is denied；pipe 名已按用户隔离。
	listener, err := winio.ListenPipe(t.pipePath, &winio.PipeConfig{
		InputBufferSize:  64 * 1024,
		OutputBufferSize: 64 * 1024,
	})
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
		pc := &pipeConn{id: uuid.New().String()[:8], conn: conn, reader: bufio.NewReader(conn)}
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
	return sendFrame(ctx, &c.mu, c.conn, msg)
}

func (c *pipeConn) Receive() ([]byte, error) {
	return receiveFrame(c.reader)
}

func (c *pipeConn) Close() error { return c.conn.Close() }
