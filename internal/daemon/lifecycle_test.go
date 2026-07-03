package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/protocol"
)

type lifecycleTestTransport struct {
	info protocol.TransportInfo
}

func (t lifecycleTestTransport) Name() string                                  { return "test" }
func (t lifecycleTestTransport) Mount(context.Context, protocol.Service) error { return nil }
func (t lifecycleTestTransport) Close(context.Context) error                   { return nil }
func (t lifecycleTestTransport) ConnectionCount() int                          { return 0 }
func (t lifecycleTestTransport) Info() protocol.TransportInfo                  { return t.info }

func TestLifecycleStopsAfterIdleExitGrace(t *testing.T) {
	stopCtx, stopCancel := context.WithCancel(context.Background())
	watchCtx, watchCancel := context.WithCancel(context.Background())
	defer watchCancel()

	d := &Daemon{sinks: make(map[string]protocol.EventSink), cancelFn: stopCancel, transports: []protocol.Transport{lifecycleTestTransport{info: protocol.TransportInfo{Retention: protocol.RetentionIdleExit, IdleTimeout: 20 * time.Millisecond}}}}
	go NewLifecycle(d).Watch(watchCtx)

	select {
	case <-stopCtx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not stop after no-client grace")
	}
}

func TestLifecycleKeepsDaemonWhenClientReconnects(t *testing.T) {
	stopCtx, stopCancel := context.WithCancel(context.Background())
	watchCtx, watchCancel := context.WithCancel(context.Background())
	defer watchCancel()

	d := &Daemon{sinks: make(map[string]protocol.EventSink), cancelFn: stopCancel, transports: []protocol.Transport{lifecycleTestTransport{info: protocol.TransportInfo{Retention: protocol.RetentionIdleExit, IdleTimeout: 100 * time.Millisecond}}}}
	go NewLifecycle(d).Watch(watchCtx)

	time.Sleep(20 * time.Millisecond)
	d.addConnection("client", nil)
	defer d.removeConnection("client")

	select {
	case <-stopCtx.Done():
		t.Fatal("daemon stopped while a client was connected")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestLifecycleStopsClientBoundWithoutClient(t *testing.T) {
	stopCtx, stopCancel := context.WithCancel(context.Background())
	watchCtx, watchCancel := context.WithCancel(context.Background())
	defer watchCancel()

	d := &Daemon{sinks: make(map[string]protocol.EventSink), cancelFn: stopCancel, transports: []protocol.Transport{lifecycleTestTransport{info: protocol.TransportInfo{Retention: protocol.RetentionClientBound}}}}
	go NewLifecycle(d).Watch(watchCtx)

	select {
	case <-stopCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("daemon did not stop after client-bound disconnect")
	}
}

func TestLifecycleKeepsPersistentTransport(t *testing.T) {
	stopCtx, stopCancel := context.WithCancel(context.Background())
	watchCtx, watchCancel := context.WithCancel(context.Background())
	defer watchCancel()

	d := &Daemon{sinks: make(map[string]protocol.EventSink), cancelFn: stopCancel, transports: []protocol.Transport{lifecycleTestTransport{info: protocol.TransportInfo{Retention: protocol.RetentionPersistent}}}}
	go NewLifecycle(d).Watch(watchCtx)

	select {
	case <-stopCtx.Done():
		t.Fatal("daemon stopped with persistent transport")
	case <-time.After(200 * time.Millisecond):
	}
}
