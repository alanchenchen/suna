package daemon

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/alanchenchen/suna/internal/agent"
	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/protocol"
)

/*
Daemon 是 sunad 守护进程的核心结构。

设计原则（01-architecture.md Daemon 架构）：
  - 核心逻辑与客户端进程生命周期完全解耦
  - 感知源 24/7 运行，不依赖 TUI 或未来 Web UI
  - 记忆异步批量提取，不受 UI 生命周期约束
  - 只挂载 protocol.Transport，不关心 local/web 等具体通信实现

生命周期：
 1. 启动 → 创建 PID 文件 → 挂载 transports
 2. 运行 → protocol.Service 处理请求 → 驱动 Agent Loop
 3. 退出 → 无客户端 + 无感知源 → 等 30 分钟 → 退出
*/
type Daemon struct {
	cfg     *config.Config
	agent   *agent.Agent
	service *service
	// transports 是 daemon 的全部通信入口；daemon 只认识 protocol.Transport，不关心具体是 socket、pipe 还是 Web。
	transports []protocol.Transport

	startTime time.Time
	mu        sync.Mutex
	sinks     map[string]protocol.EventSink
	cancelFn  context.CancelFunc
}

// New 创建 Daemon 实例。具体 transport 由入口层注入，daemon 只认识 protocol.Transport。
func New(cfg *config.Config, transports []protocol.Transport) (*Daemon, error) {
	agent, err := agent.NewAgent(cfg)
	if err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}

	return &Daemon{
		cfg:        cfg,
		agent:      agent,
		transports: transports,
		sinks:      make(map[string]protocol.EventSink),
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

	d.service = newService(d)
	for _, tr := range d.transports {
		// Mount 会启动具体监听逻辑，并把收到的请求统一转发给 protocol.Service。
		if err := tr.Mount(ctx, d.service); err != nil {
			return fmt.Errorf("mount transport %s: %w", tr.Name(), err)
		}
		defer tr.Close(ctx)
	}

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

func (d *Daemon) addConnection(connID string, sink protocol.EventSink) {
	d.mu.Lock()
	// 保存 EventSink，使后台 agent run 在原连接处理协程之外也能继续推送事件。
	d.sinks[connID] = sink
	d.mu.Unlock()
}

func (d *Daemon) removeConnection(connID string) {
	d.mu.Lock()
	delete(d.sinks, connID)
	d.mu.Unlock()
}

func (d *Daemon) sinkFor(connID string, fallback protocol.EventSink) protocol.EventSink {
	d.mu.Lock()
	sink, ok := d.sinks[connID]
	d.mu.Unlock()
	if ok && sink != nil {
		return sink
	}
	return fallback
}

// ConnectionCount 返回当前连接数
func (d *Daemon) ConnectionCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.sinks)
}

func (d *Daemon) BroadcastToAll(ctx context.Context, method string, params any) {
	d.mu.Lock()
	sinks := make([]protocol.EventSink, 0, len(d.sinks))
	for _, sink := range d.sinks {
		sinks = append(sinks, sink)
	}
	d.mu.Unlock()
	for _, sink := range sinks {
		_ = sink.Emit(ctx, protocol.Event{Method: method, Params: params})
	}
}

func (d *Daemon) Agent() *agent.Agent {
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
	return os.WriteFile(d.cfg.PIDPath(), []byte(strconv.Itoa(os.Getpid())), 0644)
}

func (d *Daemon) removePID() {
	os.Remove(d.cfg.PIDPath())
}
