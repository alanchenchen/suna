package ipc

import (
	"context"
)

// Transport IPC 传输层接口
// 抽象 Unix Socket (macOS/Linux) 和 Named Pipe (Windows)
type Transport interface {
	// Listen 开始监听连接
	Listen(ctx context.Context) error
	// Close 停止监听并关闭所有连接
	Close() error
	// OnConnect 注册新连接回调
	OnConnect(func(Conn))
}

// Conn 单个 IPC 连接
type Conn interface {
	// Send 发送消息（带超时）
	Send(ctx context.Context, msg []byte) error
	// Receive 接收消息（阻塞）
	Receive() ([]byte, error)
	// Close 关闭连接
	Close() error
	// ID 返回连接唯一标识
	ID() string
}
