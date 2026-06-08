package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
)

type rpcClient struct {
	transport *stdioTransport
	nextID    atomic.Int64
	mu        sync.Mutex
	pending   map[int64]chan rpcResponse
	closed    chan struct{}
	closeOnce sync.Once
}

func newRPCClient(transport *stdioTransport) *rpcClient {
	c := &rpcClient{transport: transport, pending: map[int64]chan rpcResponse{}, closed: make(chan struct{})}
	go c.readLoop()
	return c
}

func (c *rpcClient) call(ctx context.Context, method string, params map[string]any, out any) error {
	id := c.nextID.Add(1)
	respCh := make(chan rpcResponse, 1)
	c.mu.Lock()
	c.pending[id] = respCh
	c.mu.Unlock()

	req := rpcRequest{JSONRPC: jsonrpcVersion, ID: id, Method: method, Params: params}
	if err := c.transport.writeJSON(req); err != nil {
		c.removePending(id)
		return err
	}

	select {
	case <-ctx.Done():
		c.removePending(id)
		return ctx.Err()
	case <-c.closed:
		return fmt.Errorf("mcp rpc client closed")
	case resp := <-respCh:
		if resp.Error != nil {
			return fmt.Errorf("mcp %s error %d: %s", method, resp.Error.Code, resp.Error.Message)
		}
		if out == nil || len(resp.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return fmt.Errorf("decode mcp %s result: %w", method, err)
		}
		return nil
	}
}

func (c *rpcClient) notify(method string, params map[string]any) error {
	return c.transport.writeJSON(rpcNotification{JSONRPC: jsonrpcVersion, Method: method, Params: params})
}

func (c *rpcClient) close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
	})
	return c.transport.close()
}

func (c *rpcClient) readLoop() {
	defer c.closeOnce.Do(func() { close(c.closed) })
	for {
		line, err := c.transport.readLine()
		if err != nil {
			return
		}
		var probe struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  *rpcError       `json:"error"`
		}
		if err := json.Unmarshal(line, &probe); err != nil || probe.ID == nil {
			// MCP notifications are currently informational for Suna's tools-only MVP.
			continue
		}
		resp := rpcResponse{ID: *probe.ID, Result: probe.Result, Error: probe.Error}
		c.mu.Lock()
		ch := c.pending[*probe.ID]
		delete(c.pending, *probe.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- resp
		}
	}
}

func (c *rpcClient) removePending(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}
