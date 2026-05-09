package daemon

import (
	"context"
	"log"
	"time"
)

/*
Lifecycle 管理 daemon 的自动退出策略。

设计原则（01-architecture.md Daemon 生命周期）：
  - 最后一个客户端断开 → 等 30 分钟
  - 30 分钟内无客户端重连 → 优雅退出
  - 但有活跃感知源 (已注册的 Timer/Webhook 等) → 不退出
  - 无活跃感知源也无客户端 → 退出
*/

const idleShutdownDelay = 30 * time.Minute

type Lifecycle struct {
	daemon *Daemon
}

func NewLifecycle(d *Daemon) *Lifecycle {
	return &Lifecycle{daemon: d}
}

// Watch 启动生命周期监控 goroutine
func (l *Lifecycle) Watch(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	var idleSince time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if l.daemon.ConnectionCount() > 0 {
				idleSince = time.Time{}
				continue
			}

			// 无客户端连接
			if idleSince.IsZero() {
				idleSince = time.Now()
				log.Printf("[lifecycle] no clients, starting idle timer")
				continue
			}

			// 等待超时
			if time.Since(idleSince) >= idleShutdownDelay {
				log.Printf("[lifecycle] idle for %v, shutting down", idleShutdownDelay)
				l.daemon.Stop()
				return
			}
		}
	}
}
