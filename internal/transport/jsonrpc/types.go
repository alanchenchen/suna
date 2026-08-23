package jsonrpc

import (
	"context"

	"github.com/alanchenchen/suna/internal/protocol"
)

type Request struct {
	JSONRPC string `json:"jsonrpc"`
	// ID 当前 v0 只支持整数 request id；客户端 notification 和 string id 暂不作为公开能力。
	ID     int    `json:"id,omitempty"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id,omitempty"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

type Notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type CodedError interface {
	error
	Code() int
}

type DataError interface {
	error
	Data() any
}

type Conn interface {
	Send(ctx context.Context, msg []byte) error
	Receive() ([]byte, error)
	Close() error
	ID() string
}

type Options struct {
	// RequireHello 为需要显式完成 Runtime capability handshake 的 transport 打开首包门禁。
	RequireHello bool
	// Transport 是承载层真实名称，会覆盖 runtime.hello params 中客户端伪造的 transport。
	Transport string
	// OnHandshake 会在 runtime.hello 被 service 成功接受后调用，用于 transport
	// 解除仅适用于未认证连接的临时限制。
	OnHandshake func()
}

type connSink struct{ conn Conn }

var _ protocol.EventSink = connSink{}
