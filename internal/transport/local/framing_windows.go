//go:build windows

package local

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"sync"
	"time"
)

// sendFrame 按 JSON-RPC line framing 写出完整一帧。
// 写 deadline 由调用方 ctx 控制，避免 Windows pipe 慢写长期占住业务协程。
func sendFrame(ctx context.Context, mu *sync.Mutex, conn net.Conn, msg []byte) error {
	mu.Lock()
	defer mu.Unlock()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetWriteDeadline(deadline)
		defer conn.SetWriteDeadline(time.Time{})
	}
	data := make([]byte, 0, len(msg)+1)
	data = append(data, msg...)
	data = append(data, '\n')
	for len(data) > 0 {
		n, err := conn.Write(data)
		if err != nil {
			return err
		}
		data = data[n:]
	}
	return nil
}

// receiveFrame 读取一行 JSON-RPC frame。reader 持有跨 Read 的内部缓冲，
// 不能每次 Receive 临时创建，否则会丢失已经预读的后续帧。
func receiveFrame(reader *bufio.Reader) ([]byte, error) {
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		line = bytes.TrimRight(line, "\r\n")
		if len(line) == 0 {
			continue
		}
		return line, nil
	}
}
