//go:build windows

package main

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/transport/local"
)

func TestStartBackgroundPreservesConfiguredStderr(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "echo daemon-start-error 1>&2")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := startBackground(cmd); err != nil {
		t.Fatalf("startBackground() error = %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if got := stderr.String(); !strings.Contains(got, "daemon-start-error") {
		t.Fatalf("stderr = %q, want daemon startup output", got)
	}
}

func TestWaitUntilDaemonAvailableOrExitWaitsForWindowsWinner(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)

	exited := make(chan error, 1)
	exited <- errors.New("lost named pipe race")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type mountResult struct {
		transport protocol.Transport
		err       error
	}
	mounted := make(chan mountResult, 1)
	go func() {
		time.Sleep(250 * time.Millisecond)
		tr := local.NewPlatformTransport(local.DefaultEndpoint())
		err := tr.Mount(ctx, readyStatusService{})
		mounted <- mountResult{transport: tr, err: err}
	}()

	waitErr := waitUntilDaemonAvailableOrExit(3*time.Second, exited)
	result := <-mounted
	if result.transport != nil {
		t.Cleanup(func() { _ = result.transport.Close(context.Background()) })
	}
	if waitErr != nil {
		t.Fatalf("waitUntilDaemonAvailableOrExit() error = %v", waitErr)
	}
	if result.err != nil {
		t.Fatalf("winner Mount() error = %v", result.err)
	}
}

type readyStatusService struct{}

func (readyStatusService) OnConnect(context.Context, string, protocol.EventSink) {}
func (readyStatusService) OnDisconnect(context.Context, string)                  {}
func (readyStatusService) Handle(_ context.Context, req protocol.Request, _ protocol.EventSink) (any, error) {
	if req.Method != protocol.MethodDaemonStatus {
		return nil, errors.New("unexpected method")
	}
	return protocol.DaemonStatusParams{State: protocol.DaemonRuntimeReady}, nil
}
