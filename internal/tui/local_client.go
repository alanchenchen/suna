package tui

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/transport/local"
)

/*
localClient 是当前 TUI 使用的 local transport 客户端。

职责：
  - 连接 daemon 的本地入口（Unix socket / Windows named pipe）
  - 用 local transport 的 JSON-RPC framing 承载 protocol request
  - 接收 daemon 的 protocol event notification，分发给 UI 回调

设计原则（01-architecture.md I/O 抽象层）：

	TUI 不持有任何业务逻辑、状态、数据库连接。
	TUI 只负责渲染 UI，并通过 protocol schema 与 daemon 交互。
*/
type localClient struct {
	conn      net.Conn
	mu        sync.Mutex
	connected bool
	reqID     int
	pending   map[int]string

	onNotify func(method string, params json.RawMessage)
}

// NewLocalClient 创建当前 TUI 的 local transport 客户端。
func NewLocalClient() *localClient {
	return &localClient{}
}

func (c *localClient) Connect() error {
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

func (c *localClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connected = false
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *localClient) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

func (c *localClient) OnNotify(fn func(method string, params json.RawMessage)) {
	c.onNotify = fn
}

func (c *localClient) SendRequestNotify(method string, params any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.conn == nil {
		return fmt.Errorf("not connected")
	}
	if c.pending == nil {
		c.pending = make(map[int]string)
	}
	id := c.nextReqIDLocked()
	c.pending[id] = method

	req := local.Request{
		JSONRPC: "2.0",
		ID:      id,
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

func (c *localClient) nextReqIDLocked() int {
	c.reqID++
	return c.reqID
}

func (c *localClient) SendMessage(content string) error {
	return c.SendRequestNotify(protocol.MethodSendMessage, protocol.SendMessageParams{Parts: []protocol.MessagePart{{Type: "text", Text: content}}})
}

func (c *localClient) Cancel() error {
	return c.SendRequestNotify(protocol.MethodCancel, nil)
}

func (c *localClient) AskReply(askID, answer string) error {
	return c.SendRequestNotify("agent.askReply", map[string]string{
		"id":     askID,
		"answer": answer,
	})
}

func (c *localClient) GuardReply(guardID, decision string) error {
	return c.SendRequestNotify(protocol.MethodGuardReply, protocol.GuardReplyParams{ID: guardID, Decision: decision})
}

func (c *localClient) NewSession() error {
	return c.SendRequestNotify(protocol.MethodSessionNew, nil)
}

func (c *localClient) RestoreSession() error {
	return c.SendRequestNotify(protocol.MethodSessionRestore, nil)
}

func (c *localClient) ListMemory() error {
	return c.SendRequestNotify(protocol.MethodMemoryList, nil)
}

func (c *localClient) Compact() error {
	return c.SendRequestNotify(protocol.MethodCompact, nil)
}

func (c *localClient) DaemonStatus() error {
	return c.SendRequestNotify(protocol.MethodDaemonStatus, nil)
}

func (c *localClient) ConfigGet() error {
	return c.SendRequestNotify(protocol.MethodConfigGet, nil)
}

func (c *localClient) ConfigSet(params protocol.ConfigSetParams) error {
	return c.SendRequestNotify(protocol.MethodConfigSet, params)
}

func (c *localClient) receiveLoop() {
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

func (c *localClient) handleMessage(raw []byte) {
	var meta struct {
		Method string `json:"method"`
		ID     int    `json:"id"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return
	}

	if meta.Method != "" && meta.ID == 0 && c.onNotify != nil {
		var rawParams json.RawMessage
		var rawMsg map[string]json.RawMessage
		json.Unmarshal(raw, &rawMsg)
		if p, ok := rawMsg["params"]; ok {
			rawParams = p
		}
		c.onNotify(meta.Method, rawParams)
		return
	}

	var resp local.Response
	if err := json.Unmarshal(raw, &resp); err != nil || c.onNotify == nil {
		return
	}
	method := ""
	c.mu.Lock()
	if c.pending != nil {
		method = c.pending[resp.ID]
		delete(c.pending, resp.ID)
	}
	c.mu.Unlock()
	if resp.Error != nil {
		data, _ := json.Marshal(map[string]string{"message": resp.Error.Message})
		if method == protocol.MethodCompact {
			c.onNotify("compact.error", data)
		} else {
			c.onNotify("config.error", data)
		}
		return
	}
	if resp.Result == nil {
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
	return hasModels
}
