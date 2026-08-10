//go:build !windows

package main

import (
	"testing"
)

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
