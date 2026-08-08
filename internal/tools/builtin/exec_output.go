package builtin

import (
	"strings"
	"sync"
)

// headTailBuffer 在持续消费输出的同时保留头尾，避免错误末尾被截掉。
type headTailBuffer struct {
	mu        sync.Mutex
	limit     int
	head      []byte
	tail      []byte
	total     int64
	truncated bool
}

func newHeadTailBuffer(limit int) *headTailBuffer { return &headTailBuffer{limit: limit} }

func (b *headTailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	original := len(p)
	b.total += int64(original)
	headLimit := b.limit / 2
	if len(b.head) < headLimit {
		take := headLimit - len(b.head)
		if take > len(p) {
			take = len(p)
		}
		b.head = append(b.head, p[:take]...)
		p = p[take:]
	}
	tailLimit := b.limit - headLimit
	if len(p) > 0 {
		b.tail = append(b.tail, p...)
		if len(b.tail) > tailLimit {
			b.tail = append([]byte(nil), b.tail[len(b.tail)-tailLimit:]...)
		}
	}
	b.truncated = b.total > int64(b.limit)
	return original, nil
}

func (b *headTailBuffer) String() (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.truncated {
		return string(append(append([]byte(nil), b.head...), b.tail...)), false
	}
	return string(b.head) + "\n... (truncated; middle omitted) ...\n" + string(b.tail), true
}

// formatStreams 为前台输出补充流标签，并汇总截断状态。
func formatStreams(stdout string, stdoutTruncated bool, stderr string, stderrTruncated bool) ([]byte, bool) {
	var text strings.Builder
	if stdout != "" {
		text.WriteString("[stdout]\n")
		text.WriteString(stdout)
	}
	if stderr != "" {
		if text.Len() > 0 && !strings.HasSuffix(text.String(), "\n") {
			text.WriteByte('\n')
		}
		text.WriteString("[stderr]\n")
		text.WriteString(stderr)
	}
	return []byte(text.String()), stdoutTruncated || stderrTruncated
}

// cursorRing 使用绝对字节游标；游标落后于窗口时会报告截断并从现存最早位置返回。
type cursorRing struct {
	mu    sync.Mutex
	limit int
	buf   []byte
	start int64
	end   int64
}

func newCursorRing(limit int) *cursorRing { return &cursorRing{limit: limit} }

func (r *cursorRing) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(p)
	r.end += int64(n)
	if n >= r.limit {
		r.buf = append(r.buf[:0], p[n-r.limit:]...)
		r.start = r.end - int64(len(r.buf))
		return n, nil
	}
	r.buf = append(r.buf, p...)
	if len(r.buf) > r.limit {
		drop := len(r.buf) - r.limit
		copy(r.buf, r.buf[drop:])
		r.buf = r.buf[:r.limit]
		r.start += int64(drop)
	}
	return n, nil
}

func (r *cursorRing) snapshot(cursor int64) ([]byte, int64, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	truncated := cursor < r.start
	if cursor < r.start {
		cursor = r.start
	}
	if cursor > r.end {
		cursor = r.end
	}
	offset := int(cursor - r.start)
	return append([]byte(nil), r.buf[offset:]...), r.end, truncated
}

// stats 返回累计写入字节数和是否发生过窗口丢弃，不复制输出。
func (r *cursorRing) stats() (capturedBytes int64, truncated bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.end, r.start > 0
}

// streamWriter 首次写入时附加流标签，随后直接写入共享游标缓冲。
type streamWriter struct {
	mu     sync.Mutex
	label  string
	output *cursorRing
	wrote  bool
}

func (w *streamWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.wrote {
		combined := make([]byte, 0, len(w.label)+len(p))
		combined = append(combined, w.label...)
		combined = append(combined, p...)
		_, err := w.output.Write(combined)
		w.wrote = true
		return len(p), err
	}
	return w.output.Write(p)
}
