package tui

import (
	"encoding/json"
	"time"

	"github.com/alanchenchen/suna/internal/protocol"
)

const streamFlushInterval = 16 * time.Millisecond

type notificationBatcher struct {
	program *TUI
	stream  streamAccumulator
	reason  streamAccumulator
	order   []string
	timer   *time.Timer
}

type streamAccumulator struct {
	params protocol.StreamParams
	has    bool
}

func (b *notificationBatcher) run(ch <-chan localNotification) {
	for {
		select {
		case notif, ok := <-ch:
			if !ok {
				b.flushAll()
				return
			}
			b.handle(notif)
		case <-b.timerC():
			b.flushAll()
		}
	}
}

func (b *notificationBatcher) handle(notif localNotification) {
	if isTextStreamNotification(notif) {
		b.accumulate(notif)
		b.ensureTimer()
		return
	}
	// 非文本事件必须即时显示；先 flush 已合并文本，避免 tool/done 被历史 delta 堵住。
	b.flushAll()
	b.send(notif)
}

func (b *notificationBatcher) accumulate(notif localNotification) {
	var p protocol.StreamParams
	if err := json.Unmarshal(notif.params, &p); err != nil {
		b.flushAll()
		b.send(notif)
		return
	}
	if p.Done {
		b.flushAll()
		b.send(notif)
		return
	}
	if len(b.order) > 0 && b.order[len(b.order)-1] != notif.method {
		b.flushAll()
	}
	acc := &b.stream
	if notif.method == protocol.NotifyReasoning {
		acc = &b.reason
	}
	if !acc.has {
		b.order = append(b.order, notif.method)
	}
	acc.params.Chunk += p.Chunk
	acc.params.ID = p.ID
	acc.has = true
}

func (b *notificationBatcher) flushAll() {
	b.stopTimer()
	for _, method := range b.order {
		b.flush(method)
	}
	b.order = nil
}

func (b *notificationBatcher) flush(method string) {
	acc := &b.stream
	if method == protocol.NotifyReasoning {
		acc = &b.reason
	}
	if !acc.has {
		return
	}
	params := acc.params
	*acc = streamAccumulator{}
	data, _ := json.Marshal(params)
	b.send(localNotification{method: method, params: data})
}

func (b *notificationBatcher) send(notif localNotification) {
	if b.program != nil && b.program.program != nil {
		b.program.program.Send(notif)
	}
}

func (b *notificationBatcher) ensureTimer() {
	if b.timer == nil {
		b.timer = time.NewTimer(streamFlushInterval)
	}
}

func (b *notificationBatcher) stopTimer() {
	if b.timer == nil {
		return
	}
	if !b.timer.Stop() {
		select {
		case <-b.timer.C:
		default:
		}
	}
	b.timer = nil
}

func (b *notificationBatcher) timerC() <-chan time.Time {
	if b.timer == nil {
		return nil
	}
	return b.timer.C
}

func isTextStreamNotification(notif localNotification) bool {
	return notif.method == protocol.NotifyStream || notif.method == protocol.NotifyReasoning
}
