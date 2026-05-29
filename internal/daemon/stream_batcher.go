package daemon

import (
	"context"
	"strings"
	"time"

	"github.com/alanchenchen/suna/internal/protocol"
)

const (
	streamBatchInterval = 8 * time.Millisecond
	maxStreamBatchBytes = 32 * 1024
)

type streamBatcher struct {
	method string
	buf    strings.Builder
}

func (b *streamBatcher) addStream(ctx context.Context, sink protocol.EventSink, content string) bool {
	return b.add(ctx, sink, protocol.NotifyStream, content)
}

func (b *streamBatcher) addReasoning(ctx context.Context, sink protocol.EventSink, content string) bool {
	return b.add(ctx, sink, protocol.NotifyReasoning, content)
}

func (b *streamBatcher) add(ctx context.Context, sink protocol.EventSink, method, content string) bool {
	if content == "" {
		return false
	}
	if b.method != "" && b.method != method {
		b.flush(ctx, sink)
	}
	b.method = method
	b.buf.WriteString(content)
	return b.buf.Len() >= maxStreamBatchBytes
}

func (b *streamBatcher) flush(ctx context.Context, sink protocol.EventSink) {
	// daemon 只做传输级微批处理；展示缓存、Markdown 渲染和滚动状态由具体 UI client 负责。
	if b.method == "" || b.buf.Len() == 0 {
		return
	}
	emit(ctx, sink, b.method, protocol.StreamParams{Chunk: b.buf.String()})
	b.method = ""
	b.buf.Reset()
}
