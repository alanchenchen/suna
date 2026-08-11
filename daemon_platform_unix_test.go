//go:build unix

package main

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/config"
)

func TestWaitUntilDaemonAvailableOrExitReturnsWhenUnixChildOwnsNoLease(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(config.DefaultDataDir(), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	exited := make(chan error, 1)
	exited <- errors.New("startup failed")
	started := time.Now()
	err := waitUntilDaemonAvailableOrExit(2*time.Second, exited)
	if err == nil || !strings.Contains(err.Error(), "startup failed") {
		t.Fatalf("waitUntilDaemonAvailableOrExit() error = %v, want child error", err)
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("waitUntilDaemonAvailableOrExit() elapsed = %s, want immediate failure", elapsed)
	}
}

func TestAcquireDaemonLeaseIsExclusive(t *testing.T) {
	path := t.TempDir() + "/sunad.lock"
	first, err := acquireDaemonLease(path)
	if err != nil {
		t.Fatalf("first acquireDaemonLease() error = %v", err)
	}
	defer first.Close()

	if _, err := acquireDaemonLease(path); err == nil {
		t.Fatal("second acquireDaemonLease() error = nil, want lock conflict")
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	second, err := acquireDaemonLease(path)
	if err != nil {
		t.Fatalf("acquireDaemonLease() after release error = %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}
