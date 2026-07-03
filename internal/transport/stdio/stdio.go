package stdio

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"sync"

	"github.com/google/uuid"

	"github.com/alanchenchen/suna/internal/protocol"
	transportjsonrpc "github.com/alanchenchen/suna/internal/transport/jsonrpc"
)

type Transport struct {
	in  io.Reader
	out io.Writer
	// stdio 是单连接 transport：父进程关闭 stdin/stdout 后 runtime 应跟随退出。
	conn   *conn
	mu     sync.Mutex
	closed bool
}

type conn struct {
	id     string
	reader *bufio.Reader
	out    io.Writer
	mu     sync.Mutex
}

func New(in io.Reader, out io.Writer) *Transport {
	return &Transport{in: in, out: out}
}

func (t *Transport) Name() string { return "stdio" }

func (t *Transport) Info() protocol.TransportInfo {
	// stdio runtime 绑定父进程 stdio 连接；连接消失后不保留后台进程。
	return protocol.TransportInfo{Retention: protocol.RetentionClientBound}
}

func (t *Transport) Mount(ctx context.Context, svc protocol.Service) error {
	c := &conn{id: uuid.New().String()[:8], reader: bufio.NewReader(t.in), out: t.out}
	t.mu.Lock()
	t.conn = c
	t.mu.Unlock()
	// stdio runtime 对外公开，必须先 runtime.hello，避免客户端误用旧 local/TUI 内部协议心智。
	go transportjsonrpc.ServeConn(ctx, c, svc, transportjsonrpc.Options{RequireHello: true, Transport: t.Name()}, func() {
		t.mu.Lock()
		t.conn = nil
		t.closed = true
		t.mu.Unlock()
	})
	return nil
}

func (t *Transport) Close(ctx context.Context) error {
	_ = ctx
	t.mu.Lock()
	c := t.conn
	t.conn = nil
	t.closed = true
	t.mu.Unlock()
	if c != nil {
		return c.Close()
	}
	return nil
}

func (t *Transport) ConnectionCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.conn == nil || t.closed {
		return 0
	}
	return 1
}

func (c *conn) ID() string { return c.id }

func (c *conn) Send(ctx context.Context, msg []byte) error {
	// stdout 是公开协议通道，只写 NDJSON；人类诊断必须走 stderr。
	_ = ctx
	c.mu.Lock()
	defer c.mu.Unlock()
	data := make([]byte, 0, len(msg)+1)
	data = append(data, msg...)
	data = append(data, '\n')
	_, err := c.out.Write(data)
	return err
}

func (c *conn) Receive() ([]byte, error) {
	for {
		line, err := c.reader.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		line = bytes.TrimRight(line, "\r\n")
		if len(line) == 0 {
			continue
		}
		return line, nil
	}
}

func (c *conn) Close() error { return nil }
