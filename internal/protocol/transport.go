package protocol

import (
	"context"
	"time"
)

// RetentionPolicy 描述 transport 对 daemon 生命周期的保活需求。
type RetentionPolicy string

const (
	// RetentionClientBound 表示 transport 不允许 daemon 在无客户端时后台驻留。
	// 所有活跃连接断开后，daemon 应立即退出。
	RetentionClientBound RetentionPolicy = "client_bound"
	// RetentionIdleExit 表示最后一个客户端断开后等待 IdleTimeout，再退出，适合官方 TUI 的 local daemon。
	RetentionIdleExit RetentionPolicy = "idle_exit"
	// RetentionPersistent 表示 transport 自身需要长期监听；即使没有客户端也保持 daemon 运行。
	RetentionPersistent RetentionPolicy = "persistent"
)

// TransportInfo 是 transport 挂载时向 daemon 声明的生命周期元信息。
type TransportInfo struct {
	// Retention 必须由 transport 显式声明；空值没有保活语义，daemon 无连接时会直接退出。
	Retention RetentionPolicy
	// IdleTimeout 只对 RetentionIdleExit 生效；其他策略必须忽略该字段。
	IdleTimeout time.Duration
}

// Transport 是 daemon 唯一需要认识的通信入口抽象。Unix socket、Named Pipe、TCP、WebSocket 都通过 Mount 挂载同一个 Service。
type Transport interface {
	Name() string
	Mount(ctx context.Context, svc Service) error
	Close(ctx context.Context) error
	// ConnectionCount 返回 transport 已接受但 Service sink 可能尚未登记的连接数，用于避免 lifecycle 竞态。
	ConnectionCount() int
	// Info 返回 lifecycle 元信息；业务 method/schema 不能根据 transport 名称在 daemon core 中分叉。
	Info() TransportInfo
}
