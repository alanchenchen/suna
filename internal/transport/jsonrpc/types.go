package jsonrpc

import (
	"context"

	"github.com/alanchenchen/suna/internal/protocol"
)

const (
	ErrParse         = -32700
	ErrInvalidReq    = -32600
	ErrNotFound      = -32601
	ErrInvalidParams = -32602
	ErrInternal      = -32603
	ErrHandshake     = -32010
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

type ErrorData struct {
	// Kind 是稳定错误分类，和 protocol.ProtocolErrorData 保持同一语义。
	Kind string `json:"kind"`
	// Reason 用于补充机器可读原因，不能替代 Kind。
	Reason string `json:"reason,omitempty"`
	// Retryable/StatusCode 预留给上游错误透传。
	Retryable  bool `json:"retryable,omitempty"`
	StatusCode int  `json:"status_code,omitempty"`
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
	// RequireHello 为需要显式协商 protocol version 的 transport 打开首包握手门禁。
	RequireHello bool
	// Transport 是承载层真实名称，会覆盖 runtime.hello params 中客户端伪造的 transport。
	Transport string
	// OnHandshake 会在 runtime.hello 被 service 成功接受后调用，用于 transport
	// 解除仅适用于未认证连接的临时限制。
	OnHandshake func()
}

type connSink struct{ conn Conn }

var _ protocol.EventSink = connSink{}
