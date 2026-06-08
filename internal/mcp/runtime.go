package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/alanchenchen/suna/internal/config"
)

type Runtime struct {
	cfg     config.MCPConfig
	mu      sync.RWMutex
	clients map[string]*Client
	errors  map[string]string
}

func NewRuntime(cfg config.MCPConfig) *Runtime {
	return &Runtime{cfg: cfg, clients: map[string]*Client{}, errors: map[string]string{}}
}

func (r *Runtime) Start(ctx context.Context) error {
	if r == nil || len(r.cfg.Servers) == 0 {
		return nil
	}
	names := make([]string, 0, len(r.cfg.Servers))
	for name := range r.cfg.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		sc := r.cfg.Servers[name]
		if !sc.Enabled {
			continue
		}
		// 单个 MCP server 失败不能阻塞 Suna 启动；错误保留到 /mcp 面板展示，用户可修复后 reload/activate。
		if err := r.startServer(ctx, name, sc); err != nil {
			r.setError(name, err)
		}
	}
	return nil
}

func (r *Runtime) SetConfig(cfg config.MCPConfig) {
	if r == nil {
		return
	}
	r.mu.Lock()
	oldClients := map[string]*Client{}
	for name, client := range r.clients {
		sc, ok := cfg.Servers[name]
		if !ok || !sc.Enabled {
			oldClients[name] = client
			delete(r.clients, name)
			delete(r.errors, name)
		}
	}
	r.cfg = cfg
	r.mu.Unlock()
	for _, client := range oldClients {
		client.Close()
	}
}

func (r *Runtime) Tools(ctx context.Context) ([]Tool, error) {
	if r == nil {
		return nil, nil
	}
	r.mu.RLock()
	clients := make([]*Client, 0, len(r.clients))
	for _, c := range r.clients {
		clients = append(clients, c)
	}
	r.mu.RUnlock()
	sort.Slice(clients, func(i, j int) bool { return clients[i].id < clients[j].id })
	var out []Tool
	for _, c := range clients {
		items, err := c.ListTools(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
	}
	return out, nil
}

func (r *Runtime) CallTool(ctx context.Context, server string, name string, args map[string]any) (CallResult, error) {
	r.mu.RLock()
	client := r.clients[server]
	r.mu.RUnlock()
	if client == nil {
		return CallResult{}, fmt.Errorf("mcp server %q not connected", server)
	}
	return client.CallTool(ctx, name, args)
}

func (r *Runtime) Status(ctx context.Context) []ServerInfo {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	cfg := r.cfg
	clients := make(map[string]*Client, len(r.clients))
	for name, client := range r.clients {
		clients[name] = client
	}
	errors := make(map[string]string, len(r.errors))
	for name, msg := range r.errors {
		errors[name] = msg
	}
	r.mu.RUnlock()

	names := make([]string, 0, len(cfg.Servers))
	for name := range cfg.Servers {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]ServerInfo, 0, len(names))
	for _, name := range names {
		sc := cfg.Servers[name]
		transport := sc.Transport
		if transport == "" {
			transport = TransportStdio
		}
		item := ServerInfo{ID: name, Transport: transport, Command: commandSummary(sc), Enabled: sc.Enabled, Configured: true, Error: errors[name]}
		client := clients[name]
		if client != nil {
			item.Active = true
			tools, err := client.ListTools(ctx)
			if err != nil {
				item.Active = false
				item.Error = err.Error()
			} else {
				item.ToolCount = len(tools)
			}
		}
		out = append(out, item)
	}
	return out
}

func (r *Runtime) SetActive(ctx context.Context, name string, active bool) error {
	if r == nil {
		return fmt.Errorf("mcp runtime is not initialized")
	}
	name = strings.TrimSpace(name)
	r.mu.RLock()
	sc, ok := r.cfg.Servers[name]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("mcp server %q not configured", name)
	}
	if active {
		if err := r.startServer(ctx, name, sc); err != nil {
			r.setError(name, err)
			return err
		}
		return nil
	}
	r.mu.Lock()
	client := r.clients[name]
	delete(r.clients, name)
	delete(r.errors, name)
	r.mu.Unlock()
	if client != nil {
		client.Close()
	}
	return nil
}

func (r *Runtime) ReloadServer(ctx context.Context, name string) error {
	if r == nil {
		return fmt.Errorf("mcp runtime is not initialized")
	}
	name = strings.TrimSpace(name)
	r.mu.RLock()
	sc, ok := r.cfg.Servers[name]
	active := r.clients[name] != nil
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("mcp server %q not configured", name)
	}
	if !active {
		return fmt.Errorf("mcp server %q is not active; activate it first", name)
	}
	// reload 只刷新运行态连接，不改写配置；先启动并完成 tools/list，再替换旧 client，
	// 避免新进程启动失败时把原本可用的 MCP server 断开。
	if err := r.startServer(ctx, name, sc); err != nil {
		r.setError(name, err)
		return err
	}
	return nil
}

func (r *Runtime) startServer(ctx context.Context, name string, sc config.MCPServerConfig) error {
	if sc.Transport == "" {
		sc.Transport = TransportStdio
	}
	if sc.Transport != TransportStdio {
		return fmt.Errorf("mcp server %q: unsupported transport %q", name, sc.Transport)
	}
	client, err := NewClient(name, sc)
	if err != nil {
		return fmt.Errorf("mcp server %q: %w", name, err)
	}
	if err := client.Start(ctx); err != nil {
		client.Close()
		return fmt.Errorf("mcp server %q: %w", name, err)
	}
	if _, err := client.ListTools(ctx); err != nil {
		client.Close()
		return fmt.Errorf("mcp server %q: %w", name, err)
	}
	r.mu.Lock()
	old := r.clients[name]
	r.clients[name] = client
	delete(r.errors, name)
	r.mu.Unlock()
	if old != nil {
		old.Close()
	}
	return nil
}

func (r *Runtime) setError(name string, err error) {
	if r == nil || err == nil {
		return
	}
	r.mu.Lock()
	if r.errors == nil {
		r.errors = map[string]string{}
	}
	r.errors[name] = err.Error()
	r.mu.Unlock()
}

func commandSummary(sc config.MCPServerConfig) string {
	if sc.Transport == "" || sc.Transport == TransportStdio {
		parts := append([]string{sc.Command}, sc.Args...)
		return strings.TrimSpace(strings.Join(parts, " "))
	}
	return strings.TrimSpace(sc.URL)
}

func (r *Runtime) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	clients := make([]*Client, 0, len(r.clients))
	for _, c := range r.clients {
		clients = append(clients, c)
	}
	r.mu.RUnlock()
	for _, c := range clients {
		c.Close()
	}
	return nil
}

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
