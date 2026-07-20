package main

import (
	"context"
	"errors"
	"testing"

	"github.com/alanchenchen/suna/internal/protocol"
	transporttcp "github.com/alanchenchen/suna/internal/transport/tcp"
)

func TestServeStartsDaemonOnFirstUse(t *testing.T) {
	calls := 0
	started := false
	var gotListen string
	var gotDefault bool
	result, err := serve([]string{"--json"}, serveDeps{
		queryStatus: func(context.Context) (protocol.DaemonStatusParams, error) {
			calls++
			if !started {
				return protocol.DaemonStatusParams{}, errors.New("not running")
			}
			return protocol.DaemonStatusParams{PID: 42, TCPEndpoint: transporttcp.DefaultEndpoint}, nil
		},
		start: func(listen string, defaultListen bool) error {
			started = true
			gotListen = listen
			gotDefault = defaultListen
			return nil
		},
	})
	if err != nil {
		t.Fatalf("serve error = %v", err)
	}
	if got, want := calls, 2; got != want {
		t.Fatalf("query calls = %d, want %d", got, want)
	}
	if got, want := gotListen, transporttcp.DefaultEndpoint; got != want {
		t.Fatalf("start listen = %q, want %q", got, want)
	}
	if !gotDefault {
		t.Fatal("defaultListen = false, want true")
	}
	if got, want := result.TCPEndpoint, transporttcp.DefaultEndpoint; got != want {
		t.Fatalf("tcp endpoint = %q, want %q", got, want)
	}
}

func TestServeReusesExistingDaemon(t *testing.T) {
	starts := 0
	result, err := serve([]string{"--json"}, serveDeps{
		queryStatus: func(context.Context) (protocol.DaemonStatusParams, error) {
			return protocol.DaemonStatusParams{PID: 43, TCPEndpoint: "127.0.0.1:49123"}, nil
		},
		start: func(string, bool) error {
			starts++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("serve error = %v", err)
	}
	if starts != 0 {
		t.Fatalf("start calls = %d, want 0", starts)
	}
	if got, want := result.TCPEndpoint, "127.0.0.1:49123"; got != want {
		t.Fatalf("tcp endpoint = %q, want %q", got, want)
	}
}

func TestServeRejectsDifferentExplicitListenerForExistingDaemon(t *testing.T) {
	starts := 0
	_, err := serve([]string{"--listen", "127.0.0.1:49124"}, serveDeps{
		queryStatus: func(context.Context) (protocol.DaemonStatusParams, error) {
			return protocol.DaemonStatusParams{TCPEndpoint: "127.0.0.1:49123"}, nil
		},
		start: func(string, bool) error {
			starts++
			return nil
		},
	})
	if err == nil {
		t.Fatal("serve error = nil, want listener mismatch")
	}
	if starts != 0 {
		t.Fatalf("start calls = %d, want 0", starts)
	}
}

func TestServeFailsWhenStartedDaemonRemainsUnreachable(t *testing.T) {
	starts := 0
	_, err := serve([]string{"--json"}, serveDeps{
		queryStatus: func(context.Context) (protocol.DaemonStatusParams, error) {
			return protocol.DaemonStatusParams{}, errors.New("not running")
		},
		start: func(string, bool) error {
			starts++
			return nil
		},
	})
	if err == nil {
		t.Fatal("serve error = nil, want unreachable error")
	}
	if starts != 1 {
		t.Fatalf("start calls = %d, want 1", starts)
	}
}

func TestServeRejectsInvalidEndpoint(t *testing.T) {
	_, err := serve([]string{"--listen", "0.0.0.0:7632"}, serveDeps{
		queryStatus: func(context.Context) (protocol.DaemonStatusParams, error) {
			return protocol.DaemonStatusParams{}, nil
		},
		start: func(string, bool) error { return nil },
	})
	if err == nil {
		t.Fatal("serve error = nil, want invalid endpoint error")
	}
}

func TestSameTCPEndpoint(t *testing.T) {
	if !sameTCPEndpoint("127.0.0.1:7632", "127.0.0.1:7632") {
		t.Fatal("sameTCPEndpoint rejected identical endpoints")
	}
	if sameTCPEndpoint("127.0.0.1:7632", "127.0.0.1:7633") {
		t.Fatal("sameTCPEndpoint accepted different ports")
	}
}

func TestParseCLIServe(t *testing.T) {
	if got := parseCLI([]string{"serve", "--json"}); got != "serve" {
		t.Fatalf("parseCLI(serve) = %q, want serve", got)
	}
}

func TestParseCLIDebug(t *testing.T) {
	if got := parseCLI([]string{"debug", "memory"}); got != "debug" {
		t.Fatalf("parseCLI(debug) = %q, want debug", got)
	}
}

func TestParseCLIRuntimeIsUnknownCommand(t *testing.T) {
	if got := parseCLI([]string{"runtime"}); got != "runtime" {
		t.Fatalf("parseCLI(runtime) = %q, want runtime", got)
	}
}
