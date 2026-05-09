package tui

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/alanchenchen/suna/internal/ipc"
)

/*
Client TUI 端 IPC 客户端。

职责：
  - 连接 daemon (Unix Socket / Named Pipe)
  - 发送 JSON-RPC 请求，接收响应
  - 接收 daemon 推送的 notification，分发给回调函数

设计原则（01-architecture.md I/O 抽象层）：

	TUI 不持有任何业务逻辑、状态、数据库连接。
	TUI 只做两件事：渲染 UI、通过 IPC 与 daemon 通信。
*/
type ipcClient struct {
	conn      net.Conn
	mu        sync.Mutex
	connected bool
	reqID     int

	onNotify func(method string, params json.RawMessage)
}

// NewIPCClient 创建 TUI 端 IPC 客户端
func NewIPCClient() *ipcClient {
	return &ipcClient{}
}

func (c *ipcClient) Connect() error {
	socketPath := defaultSocketPath()

	conn, err := platformDial(socketPath, 5*time.Second)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w\nIs sunad running? Start it with: suna daemon", err)
	}

	c.conn = conn
	c.connected = true

	go c.receiveLoop()

	return nil
}

func (c *ipcClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connected = false
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *ipcClient) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

func (c *ipcClient) OnNotify(fn func(method string, params json.RawMessage)) {
	c.onNotify = fn
}

func (c *ipcClient) SendRequestNotify(method string, params any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.conn == nil {
		return fmt.Errorf("not connected")
	}

	req := ipc.Request{
		JSONRPC: "2.0",
		ID:      c.nextReqIDLocked(),
		Method:  method,
		Params:  params,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}

	data = append(data, '\n')
	_, err = c.conn.Write(data)
	return err
}

func (c *ipcClient) nextReqIDLocked() int {
	c.reqID++
	return c.reqID
}

func (c *ipcClient) SendMessage(content string) error {
	return c.SendRequestNotify(ipc.MethodSendMessage, ipc.SendMessageParams{Content: content})
}

func (c *ipcClient) Cancel() error {
	return c.SendRequestNotify(ipc.MethodCancel, nil)
}

func (c *ipcClient) AskReply(askID, answer string) error {
	return c.SendRequestNotify("agent.askReply", map[string]string{
		"id":     askID,
		"answer": answer,
	})
}

func (c *ipcClient) NewSession() error {
	return c.SendRequestNotify(ipc.MethodSessionNew, nil)
}

func (c *ipcClient) RestoreSession() error {
	return c.SendRequestNotify(ipc.MethodSessionRestore, nil)
}

func (c *ipcClient) SearchMemory(query string, topK int) error {
	return c.SendRequestNotify(ipc.MethodMemorySearch, ipc.MemorySearchParams{
		Query: query,
		TopK:  topK,
	})
}

func (c *ipcClient) Compact() error {
	return c.SendRequestNotify(ipc.MethodCompact, nil)
}

func (c *ipcClient) DaemonStatus() error {
	return c.SendRequestNotify(ipc.MethodDaemonStatus, nil)
}

func (c *ipcClient) ConfigGet() error {
	return c.SendRequestNotify(ipc.MethodConfigGet, nil)
}

func (c *ipcClient) receiveLoop() {
	var buf [4096]byte
	var lineBuf []byte

	for {
		n, err := c.conn.Read(buf[:])
		if err != nil {
			c.mu.Lock()
			c.connected = false
			c.mu.Unlock()
			return
		}

		for i := 0; i < n; i++ {
			if buf[i] == '\n' {
				if len(lineBuf) > 0 {
					c.handleMessage(lineBuf)
					lineBuf = lineBuf[:0]
				}
				continue
			}
			lineBuf = append(lineBuf, buf[i])
		}
	}
}

func (c *ipcClient) handleMessage(raw []byte) {
	var notif ipc.Notification
	if err := json.Unmarshal(raw, &notif); err != nil {
		return
	}

	if notif.Method != "" && c.onNotify != nil {
		var rawParams json.RawMessage
		var rawMsg map[string]json.RawMessage
		json.Unmarshal(raw, &rawMsg)
		if p, ok := rawMsg["params"]; ok {
			rawParams = p
		}
		c.onNotify(notif.Method, rawParams)
		return
	}

	var resp ipc.Response
	if err := json.Unmarshal(raw, &resp); err != nil || resp.Result == nil || c.onNotify == nil {
		return
	}
	var rawResult json.RawMessage
	if rawMsg := map[string]json.RawMessage{}; json.Unmarshal(raw, &rawMsg) == nil {
		rawResult = rawMsg["result"]
	}
	if rawResult == nil {
		return
	}
	if looksLikeDaemonStatus(rawResult) {
		c.onNotify("daemon.full_status", rawResult)
		return
	}
	if looksLikeConfig(rawResult) {
		c.onNotify("config.state", rawResult)
	}
}

func looksLikeDaemonStatus(raw json.RawMessage) bool {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return false
	}
	_, hasPID := m["pid"]
	_, hasProvider := m["provider"]
	_, hasModel := m["model"]
	return hasPID && (hasProvider || hasModel)
}

func looksLikeConfig(raw json.RawMessage) bool {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return false
	}
	_, hasModels := m["models"]
	_, hasDefault := m["default"]
	return hasModels && hasDefault
}
