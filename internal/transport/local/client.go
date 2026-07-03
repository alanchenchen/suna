package local

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	transportjsonrpc "github.com/alanchenchen/suna/internal/transport/jsonrpc"
)

type Client struct {
	conn  net.Conn
	mu    sync.Mutex
	reqID int
	// pending 保存 request id 到响应 channel 的映射；notification 不进入这个表。
	pending map[int]chan clientResult
	// notify 只接收 daemon 主动 notification，method response 必须走 pending response 路径。
	notify func(method string, params json.RawMessage)
	closed bool
}

type clientResult struct {
	result json.RawMessage
	err    error
}

// DialDefault 连接当前平台默认 local daemon endpoint。
func DialDefault(timeout time.Duration) (*Client, error) {
	return Dial(DefaultEndpoint(), timeout)
}

func Dial(endpoint string, timeout time.Duration) (*Client, error) {
	conn, err := platformDial(endpoint, timeout)
	if err != nil {
		return nil, err
	}
	c := &Client{conn: conn, pending: make(map[int]chan clientResult)}
	go c.receiveLoop()
	return c, nil
}

func (c *Client) OnNotify(fn func(method string, params json.RawMessage)) {
	c.mu.Lock()
	c.notify = fn
	c.mu.Unlock()
}

func (c *Client) InvokeRaw(ctx context.Context, method string, params any) (json.RawMessage, error) {
	// 同一个连接允许并发 Invoke；写入请求和 pending 表登记必须在同一把锁内完成，避免 response 先到而找不到 channel。
	c.mu.Lock()
	if c.closed || c.conn == nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("not connected")
	}
	c.reqID++
	id := c.reqID
	ch := make(chan clientResult, 1)
	c.pending[id] = ch
	req := transportjsonrpc.Request{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	data, err := json.Marshal(req)
	if err != nil {
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}
	data = append(data, '\n')
	_, err = c.conn.Write(data)
	if err != nil {
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}
	c.mu.Unlock()

	select {
	case res := <-ch:
		return res.result, res.err
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closed && c.conn != nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	conn := c.conn
	c.conn = nil
	c.closePendingLocked(fmt.Errorf("connection closed"))
	c.mu.Unlock()
	if conn != nil {
		return conn.Close()
	}
	return nil
}

const maxRetainedClientLineBuffer = 256 * 1024

// receiveLoop 按 NDJSON 分帧读取 daemon 输出；超大行处理后释放 buffer，避免长期保留异常大容量。
func (c *Client) receiveLoop() {
	var buf [4096]byte
	var lineBuf []byte
	for {
		n, err := c.conn.Read(buf[:])
		if err != nil {
			c.mu.Lock()
			c.closed = true
			c.closePendingLocked(err)
			c.mu.Unlock()
			return
		}
		for i := 0; i < n; i++ {
			if buf[i] == '\n' {
				if len(lineBuf) > 0 {
					c.handleMessage(lineBuf)
					if cap(lineBuf) > maxRetainedClientLineBuffer {
						lineBuf = nil
					} else {
						lineBuf = lineBuf[:0]
					}
				}
				continue
			}
			lineBuf = append(lineBuf, buf[i])
		}
	}
}

func (c *Client) handleMessage(raw []byte) {
	var meta struct {
		Method string `json:"method"`
		ID     int    `json:"id"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return
	}
	if meta.Method != "" && meta.ID == 0 {
		// 没有 id 且带 method 的消息是 daemon notification；不能再伪装成 method response。
		var rawMsg map[string]json.RawMessage
		if err := json.Unmarshal(raw, &rawMsg); err != nil {
			return
		}
		c.mu.Lock()
		notify := c.notify
		c.mu.Unlock()
		if notify != nil {
			notify(meta.Method, rawMsg["params"])
		}
		return
	}

	var resp struct {
		ID     int                     `json:"id"`
		Result json.RawMessage         `json:"result,omitempty"`
		Error  *transportjsonrpc.Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return
	}
	res := clientResult{result: resp.Result}
	if resp.Error != nil {
		res.err = errors.New(resp.Error.Message)
	}
	c.mu.Lock()
	ch := c.pending[resp.ID]
	delete(c.pending, resp.ID)
	c.mu.Unlock()
	if ch != nil {
		ch <- res
	}
}

func (c *Client) closePendingLocked(err error) {
	for id, ch := range c.pending {
		delete(c.pending, id)
		ch <- clientResult{err: err}
	}
}
