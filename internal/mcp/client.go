package mcp

import (
	"context"
	"sort"
	"sync"

	"github.com/alanchenchen/suna/internal/config"
)

type Client struct {
	id      string
	cfg     config.MCPServerConfig
	rpc     *rpcClient
	toolsMu sync.Mutex
	tools   []Tool
}

func NewClient(id string, cfg config.MCPServerConfig) (*Client, error) {
	return &Client{id: id, cfg: cfg}, nil
}

func (c *Client) Start(ctx context.Context) error {
	transport, err := startStdio(c.cfg.Command, c.cfg.Args, c.cfg.CWD, c.cfg.Env)
	if err != nil {
		return err
	}
	c.rpc = newRPCClient(transport)
	ctx, cancel := context.WithTimeout(ctx, serverTimeout(c.cfg))
	defer cancel()
	var init initializeResult
	params := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "suna", "version": "0.1.0"},
	}
	if err := c.rpc.call(ctx, "initialize", params, &init); err != nil {
		return err
	}
	return c.rpc.notify("notifications/initialized", nil)
}

func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	c.toolsMu.Lock()
	defer c.toolsMu.Unlock()
	if len(c.tools) > 0 {
		return append([]Tool(nil), c.tools...), nil
	}
	ctx, cancel := context.WithTimeout(ctx, serverTimeout(c.cfg))
	defer cancel()
	var res listToolsResult
	if err := c.rpc.call(ctx, "tools/list", nil, &res); err != nil {
		return nil, err
	}
	items := make([]Tool, 0, len(res.Tools))
	for _, t := range res.Tools {
		items = append(items, Tool{Server: c.id, Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	c.tools = items
	return append([]Tool(nil), items...), nil
}

func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (CallResult, error) {
	ctx, cancel := context.WithTimeout(ctx, serverTimeout(c.cfg))
	defer cancel()
	var res callToolResult
	if err := c.rpc.call(ctx, "tools/call", map[string]any{"name": name, "arguments": args}, &res); err != nil {
		return CallResult{}, err
	}
	out := CallResult{IsError: res.IsError, Content: make([]Content, 0, len(res.Content))}
	for _, item := range res.Content {
		out.Content = append(out.Content, Content{Type: item.Type, Text: item.Text, Data: item.Data, MimeType: item.MimeType, Name: item.Name})
	}
	return out, nil
}

func (c *Client) Close() {
	if c != nil && c.rpc != nil {
		_ = c.rpc.close()
	}
}
