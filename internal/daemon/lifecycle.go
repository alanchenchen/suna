package daemon

import (
	"context"
	"time"

	"github.com/alanchenchen/suna/internal/logging"
)

/*
Lifecycle 管理 daemon 的自动退出策略。

当前产品形态是单 agent、单会话，daemon 不承担长期 trigger/cowork 任务；
因此 daemon 只在有客户端连接时保持运行。最后一个客户端断开后进入短暂宽限期，
若没有新连接则停止 daemon。未开始的记忆提取保留在 SQLite 队列中，等待下次启动恢复。
*/

const (
	noClientShutdownDelay = 2 * time.Second
	lifecycleTick         = 500 * time.Millisecond
)

type Lifecycle struct {
	daemon *Daemon
}

func NewLifecycle(d *Daemon) *Lifecycle {
	return &Lifecycle{daemon: d}
}

// Watch 启动生命周期监控 goroutine。
func (l *Lifecycle) Watch(ctx context.Context) {
	ticker := time.NewTicker(lifecycleTick)
	defer ticker.Stop()

	var noClientSince time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if l.daemon.ConnectionCount() > 0 {
				if !noClientSince.IsZero() {
					logging.Info("agent", "no_client_shutdown_cancelled", nil)
				}
				noClientSince = time.Time{}
				continue
			}

			if noClientSince.IsZero() {
				noClientSince = time.Now()
				logging.Info("agent", "no_client_shutdown_timer_started", logging.Event{"grace": noClientShutdownDelay.String()})
				continue
			}

			if time.Since(noClientSince) >= noClientShutdownDelay {
				logging.Info("agent", "no_client_shutdown", logging.Event{"idle_for": time.Since(noClientSince).Truncate(time.Millisecond).String()})
				l.daemon.Stop()
				return
			}
		}
	}
}
