package daemon

import (
	"context"
	"os"
	"runtime"
	"time"

	"github.com/alanchenchen/suna/internal/protocol"
)

func (s *service) handleDebugMemory(ctx context.Context, req protocol.Request) (protocol.DebugMemoryResult, error) {
	if protocol.TransportFromContext(ctx) != "local" {
		return protocol.DebugMemoryResult{}, protocolError{code: -32601, message: "debug.memory is local-only"}
	}

	var params protocol.DebugMemoryParams
	if err := decodeParams(req.Params, &params); err != nil {
		return protocol.DebugMemoryResult{}, invalidParams(err.Error())
	}
	if params.GC {
		// 仅由显式诊断请求触发，正常运行路径绝不主动执行 GC。
		runtime.GC()
	}

	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return protocol.DebugMemoryResult{
		Timestamp:    time.Now(),
		PID:          os.Getpid(),
		HeapAlloc:    stats.HeapAlloc,
		HeapInuse:    stats.HeapInuse,
		HeapIdle:     stats.HeapIdle,
		HeapReleased: stats.HeapReleased,
		Sys:          stats.Sys,
		NumGC:        stats.NumGC,
		Goroutines:   runtime.NumGoroutine(),
	}, nil
}
