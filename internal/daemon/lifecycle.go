package daemon

import (
	"context"
	"time"

	"github.com/alanchenchen/suna/internal/logging"
	"github.com/alanchenchen/suna/internal/protocol"
)

/*
Lifecycle 管理 daemon 的自动退出策略。

transport 声明自身 retention policy，daemon 根据所有 transport 的声明和当前连接数统一决定退出；
这样 local/TUI、TCP 客户端和未来 WebSocket 可以共享 daemon core，而不把生命周期写死到某个命令或 transport 中。
*/

const (
	// lifecycleTick 是生命周期轮询粒度；具体无客户端宽限时间由 transport 的 IdleTimeout 声明。
	lifecycleTick = 500 * time.Millisecond
)

type Lifecycle struct {
	daemon *Daemon
}

func NewLifecycle(d *Daemon) *Lifecycle {
	return &Lifecycle{daemon: d}
}

// Watch 启动生命周期监控 goroutine。
func (l *Lifecycle) Watch(ctx context.Context) {
	if l.evaluate(time.Time{}).stop {
		logging.Info("agent", "no_client_shutdown", logging.Event{"reason": "initial_no_client"})
		l.daemon.Stop()
		return
	}
	ticker := time.NewTicker(lifecycleTick)
	defer ticker.Stop()

	var noClientSince time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			decision := l.evaluate(noClientSince)
			if decision.keep {
				if !noClientSince.IsZero() {
					logging.Info("agent", "no_client_shutdown_cancelled", nil)
				}
				noClientSince = time.Time{}
				continue
			}
			if decision.stop {
				logging.Info("agent", "no_client_shutdown", logging.Event{"reason": decision.reason})
				l.daemon.Stop()
				return
			}
			if noClientSince.IsZero() {
				noClientSince = time.Now()
				logging.Info("agent", "no_client_shutdown_timer_started", logging.Event{"grace": decision.idle.String()})
				continue
			}
			if time.Since(noClientSince) >= decision.idle {
				logging.Info("agent", "no_client_shutdown", logging.Event{"idle_for": time.Since(noClientSince).Truncate(time.Millisecond).String()})
				l.daemon.Stop()
				return
			}
		}
	}
}

type lifecycleDecision struct {
	// keep 表示至少有一个 transport/连接要求 daemon 继续运行。
	keep bool
	// stop 表示无需等待宽限期，应立即停止 daemon。
	stop bool
	// idle 是 idle_exit transport 声明的无客户端宽限期。
	idle time.Duration
	// reason 用于日志记录立即停止的原因，避免 UI/CLI 依赖自由文本错误。
	reason string
}

func (l *Lifecycle) evaluate(noClientSince time.Time) lifecycleDecision {
	// retention policy 的优先级是 persistent > active connection > client_bound > idle_exit。
	// 未声明 retention 的 transport 没有保活语义；无连接时会直接退出，便于暴露新 transport 漏填策略的问题。
	infos := l.daemon.transportInfos()
	for _, info := range infos {
		if info.Retention == protocol.RetentionPersistent {
			return lifecycleDecision{keep: true}
		}
	}
	if l.daemon.hasActiveConnection() {
		return lifecycleDecision{keep: true}
	}
	for _, info := range infos {
		if info.Retention == protocol.RetentionClientBound {
			return lifecycleDecision{stop: true, reason: "client_bound_disconnected"}
		}
	}
	idle := maxIdleTimeout(infos)
	if idle <= 0 {
		return lifecycleDecision{stop: true, reason: "no_clients"}
	}
	_ = noClientSince
	return lifecycleDecision{idle: idle}
}

func maxIdleTimeout(infos []protocol.TransportInfo) time.Duration {
	var idle time.Duration
	for _, info := range infos {
		if info.Retention != protocol.RetentionIdleExit {
			continue
		}
		if info.IdleTimeout > idle {
			idle = info.IdleTimeout
		}
	}
	return idle
}
