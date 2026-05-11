package daemon

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/core"
	"github.com/alanchenchen/suna/internal/ipc"
)

/*
Daemon 是 sunad 守护进程的核心结构。

设计原则（01-architecture.md Daemon 架构）：
  - 核心逻辑与 TUI 进程生命周期完全解耦
  - 感知源 24/7 运行，不依赖 TUI
  - 记忆异步批量提取，不受 UI 生命周期约束
  - 通过 IPC (Unix Socket / Named Pipe) 与 TUI 通信

生命周期：
 1. 启动 → 创建 PID/socket 文件 → 监听 IPC
 2. 运行 → 处理 IPC 请求 → 驱动 Agent Loop
 3. 退出 → 无客户端 + 无感知源 → 等 30 分钟 → 退出
*/
type Daemon struct {
	cfg       *config.Config
	agent     *core.Agent
	server    *ipc.Server
	transport ipc.Transport

	startTime time.Time
	mu        sync.Mutex
	conns     map[string]ipc.Conn
	cancelFn  context.CancelFunc
}

// New 创建 Daemon 实例
func New(cfg *config.Config) (*Daemon, error) {
	agent, err := core.NewAgent(cfg)
	if err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}

	homeDir, _ := os.UserHomeDir()
	socketPath := filepath.Join(homeDir, ".suna", "sunad.sock")

	transport := ipc.NewPlatformTransport(socketPath)

	return &Daemon{
		cfg:       cfg,
		agent:     agent,
		transport: transport,
		conns:     make(map[string]ipc.Conn),
	}, nil
}

// Run 启动 daemon 主循环（前台阻塞）
func (d *Daemon) Run() error {
	d.startTime = time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	d.cancelFn = cancel
	defer cancel()

	// 写 PID 文件
	if err := d.writePID(); err != nil {
		return fmt.Errorf("write pid: %w", err)
	}
	defer d.removePID()

	// 启动 IPC 监听
	d.transport.OnConnect(func(conn ipc.Conn) {
		d.handleConnect(ctx, conn)
	})

	if err := d.transport.Listen(ctx); err != nil {
		return fmt.Errorf("ipc listen: %w", err)
	}
	defer d.transport.Close()

	// 创建 IPC Server 处理 JSON-RPC
	d.server = ipc.NewServer(d.agent, d)
	defer d.server.Close()

	// 信号处理
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigCh
		fmt.Fprintf(os.Stderr, "sunad: received signal %s, shutting down...\n", sig)
		cancel()
	}()

	// 启动生命周期监控
	lc := NewLifecycle(d)
	go lc.Watch(ctx)

	fmt.Fprintf(os.Stderr, "sunad: started (pid %d)\n", os.Getpid())

	// 阻塞等待退出
	<-ctx.Done()

	// 优雅关闭
	d.agent.Close()
	return nil
}

// Stop 停止 daemon
func (d *Daemon) Stop() {
	if d.cancelFn != nil {
		d.cancelFn()
	}
}

// handleConnect 处理新 IPC 连接
func (d *Daemon) handleConnect(ctx context.Context, conn ipc.Conn) {
	d.mu.Lock()
	d.conns[conn.ID()] = conn
	d.mu.Unlock()

	// 推送初始状态
	d.server.SendDaemonState(ctx, conn)

	// 为该连接启动读写 goroutine
	go d.server.HandleConn(ctx, conn, func() {
		d.mu.Lock()
		delete(d.conns, conn.ID())
		d.mu.Unlock()
		conn.Close()
	})
}

// ConnectionCount 返回当前连接数
func (d *Daemon) ConnectionCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.conns)
}

// BroadcastToAll 向所有连接广播通知
func (d *Daemon) BroadcastToAll(ctx context.Context, method string, params any) {
	d.mu.Lock()
	conns := make([]ipc.Conn, 0, len(d.conns))
	for _, c := range d.conns {
		conns = append(conns, c)
	}
	d.mu.Unlock()

	d.server.Broadcast(ctx, conns, method, params)
}

// SendToConn 向指定连接发送通知
func (d *Daemon) SendToConn(ctx context.Context, connID string, method string, params any) {
	d.mu.Lock()
	conn, ok := d.conns[connID]
	d.mu.Unlock()
	if !ok {
		return
	}
	d.server.Send(ctx, conn, method, params)
}

// Agent 返回 agent 实例（供 IPC Server 调用）
func (d *Daemon) Agent() *core.Agent {
	return d.agent
}

// Uptime 返回运行时长
func (d *Daemon) Uptime() time.Duration {
	return time.Since(d.startTime)
}

func (d *Daemon) ProviderName() string {
	if mc, ok := d.agent.Config().ActiveModelConfig(); ok {
		return mc.Provider
	}
	return ""
}

func (d *Daemon) ModelName() string {
	if mc, ok := d.agent.Config().ActiveModelConfig(); ok {
		return mc.Model
	}
	return ""
}

func (d *Daemon) writePID() error {
	homeDir, _ := os.UserHomeDir()
	pidPath := filepath.Join(homeDir, ".suna", "sunad.pid")
	return os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0644)
}

func (d *Daemon) removePID() {
	homeDir, _ := os.UserHomeDir()
	pidPath := filepath.Join(homeDir, ".suna", "sunad.pid")
	os.Remove(pidPath)
}

// IsRunning 检查 daemon 是否在运行（通过 PID 文件 + socket 连接测试）
func IsRunning() bool {
	homeDir, _ := os.UserHomeDir()
	pidPath := filepath.Join(homeDir, ".suna", "sunad.pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// 发送 signal 0 检测进程是否存活
	return proc.Signal(syscall.Signal(0)) == nil
}

// ReadPID 读取 PID 文件
func ReadPID() (int, error) {
	homeDir, _ := os.UserHomeDir()
	pidPath := filepath.Join(homeDir, ".suna", "sunad.pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(data))
}
