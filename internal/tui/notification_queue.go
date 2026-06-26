package tui

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/alanchenchen/suna/internal/protocol"
)

const notificationQueueSize = 4096

type pendingNotification struct {
	notif localNotification
	delta *pendingDeltaNotification
}

type pendingDeltaNotification struct {
	params protocol.AgentDeltaParams
	buf    *strings.Builder
}

// notificationQueue 隔离 local receiveLoop 与 Bubble Tea program.Send。
// 文本流事件允许在队列满时继续合并，避免为每个溢出事件创建 goroutine；非文本事件仍按顺序送达。
type notificationQueue struct {
	ch      chan localNotification
	wake    chan struct{}
	mu      sync.Mutex
	pending []pendingNotification
}

func newNotificationQueue(handle func(localNotification)) *notificationQueue {
	q := &notificationQueue{
		ch:   make(chan localNotification, notificationQueueSize),
		wake: make(chan struct{}, 1),
	}
	go q.run(handle)
	return q
}

func (q *notificationQueue) enqueue(notif localNotification) {
	if q == nil {
		return
	}
	q.mu.Lock()
	if len(q.pending) > 0 {
		q.pending = appendMergedNotification(q.pending, notif)
		q.mu.Unlock()
		q.notifyWake()
		return
	}
	q.mu.Unlock()
	select {
	case q.ch <- notif:
		return
	default:
	}
	q.mu.Lock()
	q.pending = appendMergedNotification(q.pending, notif)
	q.mu.Unlock()
	q.notifyWake()
}

func (q *notificationQueue) notifyWake() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *notificationQueue) run(handle func(localNotification)) {
	for {
		select {
		case notif := <-q.ch:
			handle(notif)
		case <-q.wake:
			q.drainQueued(handle)
			q.flushPending(handle)
		}
	}
}

func (q *notificationQueue) drainQueued(handle func(localNotification)) {
	for {
		select {
		case notif := <-q.ch:
			handle(notif)
		default:
			return
		}
	}
}

func (q *notificationQueue) flushPending(handle func(localNotification)) {
	for {
		q.mu.Lock()
		if len(q.pending) == 0 {
			q.mu.Unlock()
			return
		}
		items := q.pending
		q.pending = nil
		q.mu.Unlock()
		for _, item := range items {
			if item.delta != nil {
				notif, ok := item.delta.notification()
				if ok {
					handle(notif)
				}
				continue
			}
			handle(item.notif)
		}
	}
}

func appendMergedNotification(items []pendingNotification, notif localNotification) []pendingNotification {
	if !isLocalDeltaNotification(notif) {
		return append(items, pendingNotification{notif: notif})
	}
	params, ok := decodeLocalDeltaNotification(notif)
	if !ok {
		return append(items, pendingNotification{notif: notif})
	}
	if len(items) > 0 {
		last := &items[len(items)-1]
		if last.delta != nil && last.delta.params.Kind == params.Kind && last.delta.params.RunID == params.RunID {
			last.delta.buf.WriteString(params.Content)
			return items
		}
	}
	buf := &strings.Builder{}
	buf.WriteString(params.Content)
	params.Content = ""
	return append(items, pendingNotification{delta: &pendingDeltaNotification{params: params, buf: buf}})
}

func (p *pendingDeltaNotification) notification() (localNotification, bool) {
	if p == nil {
		return localNotification{}, false
	}
	params := p.params
	if p.buf != nil {
		params.Content = p.buf.String()
	}
	data, err := json.Marshal(params)
	if err != nil {
		return localNotification{}, false
	}
	return localNotification{method: protocol.NotifyAgentDelta, params: data}, true
}

func decodeLocalDeltaNotification(notif localNotification) (protocol.AgentDeltaParams, bool) {
	var params protocol.AgentDeltaParams
	if err := json.Unmarshal(notif.params, &params); err != nil {
		return protocol.AgentDeltaParams{}, false
	}
	return params, true
}

func isLocalDeltaNotification(notif localNotification) bool {
	return notif.method == protocol.NotifyAgentDelta
}
