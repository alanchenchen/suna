package protocol

import "context"

type transportContextKey struct{}

// WithTransport 将 transport 已确认的连接身份附加到请求 context；客户端参数不可覆盖该值。
func WithTransport(ctx context.Context, transport string) context.Context {
	return context.WithValue(ctx, transportContextKey{}, transport)
}

// TransportFromContext 返回实际 transport 身份；空字符串表示调用方未注入连接元数据。
func TransportFromContext(ctx context.Context) string {
	transport, _ := ctx.Value(transportContextKey{}).(string)
	return transport
}

type Event struct {
	Method string
	Params any
}

// EventSink 是 Service 向当前客户端推送事件的唯一出口，具体如何编码和发送由 transport 决定。
type EventSink interface {
	Emit(ctx context.Context, event Event) error
}

// Service 是 daemon/agent 对 transport 暴露的业务接口。transport 只负责解码请求、调用 Service、再编码响应或事件。
type Service interface {
	Handle(ctx context.Context, req Request, sink EventSink) (any, error)
	OnConnect(ctx context.Context, connID string, sink EventSink)
	OnDisconnect(ctx context.Context, connID string)
}

// Request 是 transport 解包后的业务请求，不包含 JSON-RPC、HTTP、WebSocket 等线协议细节。
type Request struct {
	ID     int
	Method string
	Params any
	ConnID string
}
