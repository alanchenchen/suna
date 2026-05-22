package protocol

import "context"

// Transport 是 daemon 唯一需要认识的通信入口抽象。Unix socket、Named Pipe、WebSocket、HTTP 都通过 Mount 挂载同一个 Service。
type Transport interface {
	Name() string
	Mount(ctx context.Context, svc Service) error
	Close(ctx context.Context) error
	ConnectionCount() int
}
