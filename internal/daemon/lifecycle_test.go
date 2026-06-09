package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/protocol"
)

func TestLifecycleStopsAfterNoClientGrace(t *testing.T) {
	stopCtx, stopCancel := context.WithCancel(context.Background())
	watchCtx, watchCancel := context.WithCancel(context.Background())
	defer watchCancel()

	d := &Daemon{sinks: make(map[string]protocol.EventSink), cancelFn: stopCancel}
	go NewLifecycle(d).Watch(watchCtx)

	select {
	case <-stopCtx.Done():
	case <-time.After(noClientShutdownDelay + lifecycleTick + time.Second):
		t.Fatal("daemon did not stop after no-client grace")
	}
}

func TestLifecycleKeepsDaemonWhenClientReconnects(t *testing.T) {
	stopCtx, stopCancel := context.WithCancel(context.Background())
	watchCtx, watchCancel := context.WithCancel(context.Background())
	defer watchCancel()

	d := &Daemon{sinks: make(map[string]protocol.EventSink), cancelFn: stopCancel}
	go NewLifecycle(d).Watch(watchCtx)

	time.Sleep(noClientShutdownDelay / 2)
	d.addConnection("client", nil)
	defer d.removeConnection("client")

	select {
	case <-stopCtx.Done():
		t.Fatal("daemon stopped while a client was connected")
	case <-time.After(noClientShutdownDelay + lifecycleTick):
	}
}
