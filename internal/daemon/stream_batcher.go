package daemon

import (
	"context"
	"strings"
	"time"

	"github.com/alanchenchen/suna/internal/protocol"
)

const (
	streamBatchInterval       = 8 * time.Millisecond
	maxStreamBatchBytes       = 32 * 1024
	maxRetainedStreamBufBytes = maxStreamBatchBytes * 2
)

type streamBatcher struct {
	kind protocol.AgentDeltaKind
	buf  strings.Builder
}

func (b *streamBatcher) addStream(ctx context.Context, sink protocol.EventSink, content string) bool {
	return b.add(ctx, sink, protocol.AgentDeltaAssistant, content)
}

func (b *streamBatcher) addReasoning(ctx context.Context, sink protocol.EventSink, content string) bool {
	return b.add(ctx, sink, protocol.AgentDeltaReasoning, content)
}

func (b *streamBatcher) add(ctx context.Context, sink protocol.EventSink, kind protocol.AgentDeltaKind, content string) bool {
	if content == "" {
		return false
	}
	if b.kind != "" && b.kind != kind {
		b.flush(ctx, sink)
	}
	b.kind = kind
	b.buf.WriteString(content)
	return b.buf.Len() >= maxStreamBatchBytes
}

func (b *streamBatcher) flush(ctx context.Context, sink protocol.EventSink) {
	// daemon 只做传输级微批处理；展示缓存、Markdown 渲染和滚动状态由具体 UI client 负责。
	if b.kind == "" || b.buf.Len() == 0 {
		return
	}
	emit(ctx, sink, protocol.NotifyAgentDelta, protocol.AgentDeltaParams{Kind: b.kind, Content: b.buf.String()})
	b.kind = ""
	if b.buf.Cap() > maxRetainedStreamBufBytes {
		b.buf = strings.Builder{}
		return
	}
	b.buf.Reset()
}
