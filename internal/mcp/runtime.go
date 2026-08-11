package mcp

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/alanchenchen/suna/internal/config"
)

type Runtime struct {
	cfg config.MCPConfig
	// defaultWorkdirsDir 是未配置 CWD 的 MCP server 工作目录根路径。
	defaultWorkdirsDir string
	mu                 sync.RWMutex
	clients            map[string]*Client
	states             map[string]ServerState
	errors             map[string]string
	generations        map[string]uint64
	startCancels       map[string]context.CancelFunc
	backgroundCtx      context.Context
	onChange           func(ServerInfo)
	onCatalogSync      func(context.Context) error
	openServerFn       func(context.Context, string, config.MCPServerConfig) (*Client, error)
	startWG            sync.WaitGroup
	closeOnce          sync.Once
	closed             bool
}

func NewRuntime(cfg config.MCPConfig) *Runtime {
	return &Runtime{
		cfg:                cfg,
		defaultWorkdirsDir: config.DefaultMCPWorkdirsDir(),
		clients:            map[string]*Client{},
		states:             initialServerStates(cfg),
		errors:             map[string]string{},
		generations:        map[string]uint64{},
		startCancels:       map[string]context.CancelFunc{},
	}
}

func (r *Runtime) Start(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return fmt.Errorf("mcp runtime is closed")
	}
	r.backgroundCtx = ctx
	r.mu.Unlock()
	for name, sc := range r.Config().Servers {
		if sc.Enabled {
			r.startAsync(ctx, name, sc)
		}
	}
	return nil
}

func initialServerStates(cfg config.MCPConfig) map[string]ServerState {
	states := make(map[string]ServerState, len(cfg.Servers))
	for name, sc := range cfg.Servers {
		if sc.Enabled {
			states[name] = ServerStateStarting
		} else {
			states[name] = ServerStateDisabled
		}
	}
	return states
}

func (r *Runtime) SetOnChange(fn func(ServerInfo)) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.onChange = fn
	r.mu.Unlock()
}

func (r *Runtime) SetCatalogSync(fn func(context.Context) error) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.onCatalogSync = fn
	r.mu.Unlock()
}

func (r *Runtime) syncCatalog(ctx context.Context) error {
	r.mu.RLock()
	fn := r.onCatalogSync
	r.mu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn(ctx)
}

func (r *Runtime) SetConfig(cfg config.MCPConfig) {
	if r == nil {
		return
	}
	var oldClients []*Client
	var changes []ServerInfo
	var starts []struct {
		name string
		sc   config.MCPServerConfig
	}
	r.mu.Lock()
	for name, cancel := range r.startCancels {
		cancel()
		delete(r.startCancels, name)
		r.generations[name]++
	}
	for name, client := range r.clients {
		oldConfig, oldConfigured := r.cfg.Servers[name]
		newConfig, newConfigured := cfg.Servers[name]
		if !newConfigured || !newConfig.Enabled || !oldConfigured || !reflect.DeepEqual(oldConfig, newConfig) {
			oldClients = append(oldClients, client)
			delete(r.clients, name)
		}
	}
	r.cfg = cfg
	for name, sc := range cfg.Servers {
		if !sc.Enabled {
			r.states[name] = ServerStateDisabled
			delete(r.errors, name)
		} else if r.clients[name] != nil {
			r.states[name] = ServerStateActive
		} else {
			r.states[name] = ServerStateStarting
			if r.backgroundCtx != nil {
				starts = append(starts, struct {
					name string
					sc   config.MCPServerConfig
				}{name: name, sc: sc})
			}
		}
		changes = append(changes, r.serverInfoLocked(name))
	}
	for name := range r.states {
		if _, ok := cfg.Servers[name]; !ok {
			delete(r.states, name)
			delete(r.errors, name)
			delete(r.generations, name)
		}
	}
	r.mu.Unlock()
	for _, client := range oldClients {
		client.Close()
	}
	for _, info := range changes {
		r.notify(info)
	}
	for _, start := range starts {
		r.startAsync(r.backgroundCtx, start.name, start.sc)
	}
}

func (r *Runtime) Config() config.MCPConfig {
	if r == nil {
		return config.MCPConfig{}
	}
	r.mu.RLock()
	cfg := cloneConfig(r.cfg)
	r.mu.RUnlock()
	return cfg
}

func cloneConfig(cfg config.MCPConfig) config.MCPConfig {
	servers := make(map[string]config.MCPServerConfig, len(cfg.Servers))
	for name, sc := range cfg.Servers {
		sc.Args = append([]string(nil), sc.Args...)
		sc.Env = cloneStringMap(sc.Env)
		sc.Headers = cloneStringMap(sc.Headers)
		servers[name] = sc
	}
	return config.MCPConfig{Servers: servers}
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
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
	cfg := cloneConfig(r.cfg)
	states := make(map[string]ServerState, len(r.states))
	for name, state := range r.states {
		states[name] = state
	}
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
		item := ServerInfo{ID: name, Transport: transport, Command: commandSummary(sc), State: states[name], Error: errors[name]}
		if item.State == "" {
			if sc.Enabled {
				item.State = ServerStateStarting
			} else {
				item.State = ServerStateDisabled
			}
		}
		client := clients[name]
		if client != nil {
			tools, err := client.ListTools(ctx)
			if err != nil {
				item.State = ServerStateError
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
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return fmt.Errorf("mcp runtime is closed")
	}
	sc, ok := r.cfg.Servers[name]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("mcp server %q not configured", name)
	}
	generation := r.nextGenerationLocked(name)
	if !active {
		previousConfig := sc
		client := r.clients[name]
		previousState := r.states[name]
		previousError := r.errors[name]
		delete(r.clients, name)
		delete(r.errors, name)
		r.states[name] = ServerStateDisabled
		sc.Enabled = false
		r.cfg.Servers[name] = sc
		r.mu.Unlock()
		if err := r.syncCatalog(ctx); err != nil {
			r.mu.Lock()
			current, configured := r.cfg.Servers[name]
			if configured && r.generations[name] == generation && !current.Enabled {
				r.cfg.Servers[name] = previousConfig
				if client != nil {
					r.clients[name] = client
				}
				r.states[name] = previousState
				if previousError != "" {
					r.errors[name] = previousError
				}
			}
			r.mu.Unlock()
			return err
		}
		if client != nil {
			client.Close()
		}
		r.mu.RLock()
		info := r.serverInfoLocked(name)
		r.mu.RUnlock()
		r.notify(info)
		return nil
	}
	sc.Enabled = true
	r.cfg.Servers[name] = sc
	r.states[name] = ServerStateStarting
	delete(r.errors, name)
	info := r.serverInfoLocked(name)
	r.startWG.Add(1)
	r.mu.Unlock()
	r.notify(info)
	defer r.startWG.Done()
	return r.startGeneration(ctx, name, sc, generation)
}

func (r *Runtime) ReloadServer(ctx context.Context, name string) error {
	if r == nil {
		return fmt.Errorf("mcp runtime is not initialized")
	}
	name = strings.TrimSpace(name)
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return fmt.Errorf("mcp runtime is closed")
	}
	sc, ok := r.cfg.Servers[name]
	active := r.clients[name] != nil
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("mcp server %q not configured", name)
	}
	if !active {
		r.mu.Unlock()
		return fmt.Errorf("mcp server %q is not active; activate it first", name)
	}
	generation := r.nextGenerationLocked(name)
	r.states[name] = ServerStateStarting
	delete(r.errors, name)
	info := r.serverInfoLocked(name)
	r.startWG.Add(1)
	r.mu.Unlock()
	r.notify(info)
	defer r.startWG.Done()
	// reload 先完成新连接和 tools/list，再按 generation 替换；失败时旧 client 仍可继续执行。
	return r.startGeneration(ctx, name, sc, generation)
}

func (r *Runtime) startAsync(parent context.Context, name string, sc config.MCPServerConfig) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	generation := r.nextGenerationLocked(name)
	r.states[name] = ServerStateStarting
	delete(r.errors, name)
	info := r.serverInfoLocked(name)
	r.startWG.Add(1)
	r.mu.Unlock()
	r.notify(info)
	go func() {
		defer r.startWG.Done()
		_ = r.startGeneration(parent, name, sc, generation)
	}()
}

func (r *Runtime) nextGenerationLocked(name string) uint64 {
	if cancel := r.startCancels[name]; cancel != nil {
		cancel()
	}
	delete(r.startCancels, name)
	r.generations[name]++
	return r.generations[name]
}

func (r *Runtime) startGeneration(parent context.Context, name string, sc config.MCPServerConfig, generation uint64) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	r.mu.Lock()
	if r.closed || r.generations[name] != generation {
		r.mu.Unlock()
		cancel()
		return context.Canceled
	}
	r.startCancels[name] = cancel
	r.mu.Unlock()

	client, err := r.connectServer(ctx, name, sc)

	var old *Client
	r.mu.Lock()
	if r.generations[name] == generation {
		delete(r.startCancels, name)
	}
	current, configured := r.cfg.Servers[name]
	valid := !r.closed && configured && current.Enabled && r.generations[name] == generation
	if !valid {
		r.mu.Unlock()
		if client != nil {
			client.Close()
		}
		return context.Canceled
	}
	if err != nil {
		if r.clients[name] != nil {
			r.states[name] = ServerStateActive
		} else {
			r.states[name] = ServerStateError
		}
		r.errors[name] = err.Error()
		info := r.serverInfoLocked(name)
		r.mu.Unlock()
		r.notify(info)
		return err
	}
	old = r.clients[name]
	r.clients[name] = client
	// active 只在 Tool Catalog 原子刷新完成后发布；刷新期间保持 starting。
	r.states[name] = ServerStateStarting
	delete(r.errors, name)
	r.mu.Unlock()
	if err := r.syncCatalog(ctx); err != nil {
		r.mu.Lock()
		if r.generations[name] == generation && r.clients[name] == client {
			if old != nil {
				r.clients[name] = old
				r.states[name] = ServerStateActive
			} else {
				delete(r.clients, name)
				r.states[name] = ServerStateError
			}
			r.errors[name] = err.Error()
		}
		info := r.serverInfoLocked(name)
		r.mu.Unlock()
		client.Close()
		r.notify(info)
		return err
	}
	r.mu.Lock()
	current, configured = r.cfg.Servers[name]
	valid = !r.closed && configured && current.Enabled && r.generations[name] == generation && r.clients[name] == client
	if !valid {
		r.mu.Unlock()
		client.Close()
		return context.Canceled
	}
	r.states[name] = ServerStateActive
	info := r.serverInfoLocked(name)
	r.mu.Unlock()
	if old != nil && old != client {
		old.Close()
	}
	r.notify(info)
	return nil
}

func (r *Runtime) connectServer(ctx context.Context, name string, sc config.MCPServerConfig) (*Client, error) {
	r.mu.RLock()
	fn := r.openServerFn
	r.mu.RUnlock()
	if fn != nil {
		return fn(ctx, name, sc)
	}
	return r.openServer(ctx, name, sc)
}

func (r *Runtime) openServer(ctx context.Context, name string, sc config.MCPServerConfig) (*Client, error) {
	if sc.Transport == "" {
		sc.Transport = TransportStdio
	}
	if sc.Transport != TransportStdio {
		return nil, fmt.Errorf("mcp server %q: unsupported transport %q", name, sc.Transport)
	}
	if strings.TrimSpace(sc.CWD) == "" {
		workdir, err := r.defaultServerWorkdir(name)
		if err != nil {
			return nil, fmt.Errorf("mcp server %q: prepare workdir: %w", name, err)
		}
		sc.CWD = workdir
	}
	client, err := NewClient(name, sc)
	if err != nil {
		return nil, fmt.Errorf("mcp server %q: %w", name, err)
	}
	if err := client.Start(ctx); err != nil {
		client.Close()
		return nil, fmt.Errorf("mcp server %q: %w", name, err)
	}
	if _, err := client.ListTools(ctx); err != nil {
		client.Close()
		return nil, fmt.Errorf("mcp server %q: %w", name, err)
	}
	return client, nil
}

func (r *Runtime) defaultServerWorkdir(name string) (string, error) {
	slug := safeServerSlug(name)
	hash := sha256.Sum256([]byte(name))
	dir := filepath.Join(r.defaultWorkdirsDir, fmt.Sprintf("%s-%x", slug, hash[:6]))
	// 目录只在确实启动未配置 CWD 的 server 时创建，并限制为 owner-only。
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

func safeServerSlug(name string) string {
	const maxBytes = 48
	var b strings.Builder
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			b.WriteRune(c)
		} else if b.Len() > 0 {
			b.WriteByte('-')
		}
		if b.Len() >= maxBytes {
			break
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "server"
	}
	return slug
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

func (r *Runtime) serverInfoLocked(name string) ServerInfo {
	sc, ok := r.cfg.Servers[name]
	if !ok {
		return ServerInfo{ID: name}
	}
	transport := sc.Transport
	if transport == "" {
		transport = TransportStdio
	}
	info := ServerInfo{ID: name, Transport: transport, Command: commandSummary(sc), State: r.states[name], Error: r.errors[name]}
	if info.State == "" {
		if sc.Enabled {
			info.State = ServerStateStarting
		} else {
			info.State = ServerStateDisabled
		}
	}
	if client := r.clients[name]; client != nil {
		client.toolsMu.Lock()
		info.ToolCount = len(client.tools)
		client.toolsMu.Unlock()
	}
	return info
}

func (r *Runtime) notify(info ServerInfo) {
	r.mu.RLock()
	fn := r.onChange
	r.mu.RUnlock()
	if fn != nil && info.ID != "" {
		fn(info)
	}
}

func (r *Runtime) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closed = true
		for name, cancel := range r.startCancels {
			cancel()
			delete(r.startCancels, name)
			r.generations[name]++
		}
		clients := make([]*Client, 0, len(r.clients))
		for name, client := range r.clients {
			clients = append(clients, client)
			delete(r.clients, name)
		}
		r.mu.Unlock()
		for _, client := range clients {
			client.Close()
		}
	})
	done := make(chan struct{})
	go func() {
		r.startWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
