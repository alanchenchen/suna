package daemon

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/alanchenchen/suna/internal/agent"
	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/protocol"
)

/*
Daemon 是 Suna runtime / 后台 daemon 的核心结构。

设计原则：
  - TUI 只负责交互与渲染，核心业务由 daemon 持有
  - daemon core 不假设具体进程形态：官方 TUI 使用后台 local daemon，第三方接入使用前台 stdio runtime
  - 生命周期由 transport 声明的 retention policy 决定：local 可 idle_exit，stdio 可 client_bound，未来 server transport 可 persistent
  - PID 文件只属于后台 local daemon 管理语义，runtime 入口默认不写 sunad.pid
  - 记忆提取队列持久化到 SQLite，未开始的后台记忆整理可留到下次启动恢复
  - 只挂载 protocol.Transport，不关心 local/stdio/web 等具体通信实现

生命周期：
 1. 启动 → 按入口选项注册 PID → 挂载 transports
 2. 运行 → protocol.Service 处理请求 → 驱动 Agent Loop
 3. 退出 → transport lifecycle / stop 请求 / 系统信号 → 关闭 Agent 与 transports
*/
type Options struct {
	// RegisterPID 只用于后台 local daemon；stdio runtime 或未来公开 transport 不应默认写 sunad.pid。
	RegisterPID bool
}

type Daemon struct {
	cfg     *config.Config
	opts    Options
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
func New(cfg *config.Config, transports []protocol.Transport, opts ...Options) (*Daemon, error) {
	options := Options{}
	if len(opts) > 0 {
		options = opts[0]
	}
	agent, err := agent.NewAgent(cfg)
	if err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}

	return &Daemon{
		cfg:        cfg,
		opts:       options,
		agent:      agent,
		transports: transports,
		sinks:      make(map[string]protocol.EventSink),
	}, nil
}

// Run 启动 daemon 主循环（前台阻塞）
func (d *Daemon) Run() error {
	return d.run("sunad")
}

// RunAs 启动 daemon 主循环，并使用指定进程标签输出人类诊断；协议数据仍由 transport 决定。
func (d *Daemon) RunAs(label string) error {
	if label == "" {
		label = "sunad"
	}
	return d.run(label)
}

func (d *Daemon) run(label string) error {
	d.startTime = time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	d.cancelFn = cancel
	defer cancel()

	if d.opts.RegisterPID {
		// PID 文件是 local daemon 管理命令的发现机制；runtime 入口通过 Options 显式关闭。
		if err := d.writePID(); err != nil {
			return fmt.Errorf("write pid: %w", err)
		}
		defer d.removePID()
	}

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
		fmt.Fprintf(os.Stderr, "%s: received signal %s, shutting down...\n", label, sig)
		cancel()
	}()

	// 启动生命周期监控
	lc := NewLifecycle(d)
	go lc.Watch(ctx)

	fmt.Fprintf(os.Stderr, "%s: started (pid %d)\n", label, os.Getpid())

	// 阻塞等待退出
	<-ctx.Done()

	// 优雅关闭：无客户端或停止请求进入退出流程后，先取消当前 run，再释放资源。
	d.agent.CancelCurrentRun()
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

func (d *Daemon) hasActiveConnection() bool {
	// Service sink 和 transport 连接表之间存在极短时间差；两边任一可见连接都视为 active，避免 client_bound transport 刚挂载就被误停。
	if d.ConnectionCount() > 0 {
		return true
	}
	for _, tr := range d.transports {
		if tr.ConnectionCount() > 0 {
			return true
		}
	}
	return false
}

func (d *Daemon) transportInfos() []protocol.TransportInfo {
	// daemon 只读取 transport 声明的生命周期策略，不根据 transport 名称推断业务语义。
	infos := make([]protocol.TransportInfo, 0, len(d.transports))
	for _, tr := range d.transports {
		infos = append(infos, tr.Info())
	}
	return infos
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
