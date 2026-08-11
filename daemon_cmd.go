package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/protocol"
)

func showStatus() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	status, err := queryDaemonStatus(ctx)
	if err == nil {
		if status.TCPEndpoint != "" {
			fmt.Printf("sunad is running (pid %d, uptime %s, connections %d, tcp %s)\n", status.PID, status.Uptime, status.Connections, status.TCPEndpoint)
		} else {
			fmt.Printf("sunad is running (pid %d, uptime %s, connections %d)\n", status.PID, status.Uptime, status.Connections)
		}
		return
	}
	if pid, err := readPID(); err == nil {
		fmt.Printf("sunad is not reachable (stale pid file: %d)\n", pid)
		return
	}
	fmt.Println("sunad is not running")
}

func stopDaemonCommand() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	err := requestDaemonStop(ctx)
	cancel()
	if err == nil {
		if !waitUntilDaemonUnavailable(10 * time.Second) {
			fmt.Fprintln(os.Stderr, "Error: daemon stop requested but it is still reachable after 10 seconds")
			os.Exit(1)
		}
		fmt.Println("sunad stopped")
		return
	}

	pid, readErr := readPID()
	if readErr != nil {
		fmt.Println("sunad is not running")
		return
	}
	if err := fallbackStopProcess(pid); err != nil {
		fmt.Fprintf(os.Stderr, "Error stopping daemon: %s\n", err)
		os.Exit(1)
	}
	if !waitUntilDaemonUnavailable(10 * time.Second) {
		fmt.Fprintln(os.Stderr, "Error: daemon stop fallback completed but daemon is still reachable after 10 seconds")
		os.Exit(1)
	}
	removePID()
	fmt.Println("sunad stopped")
}

func ensureDaemonRunning() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	status, err := queryDaemonStatus(ctx)
	cancel()
	if err == nil && status.State == protocol.DaemonRuntimeReady {
		return
	}
	if err == nil {
		if waitUntilDaemonAvailable(10 * time.Second) {
			return
		}
		fmt.Fprintf(os.Stderr, "Error: daemon remained %s for 10 seconds\n", status.State)
		os.Exit(1)
	}
	if !isDaemonDialFailure(err) {
		fmt.Fprintf(os.Stderr, "Error: daemon is reachable but status is unavailable: %s\n", err)
		os.Exit(1)
	}
	if held, probeErr := daemonLeaseHeld(config.DefaultLockPath()); probeErr == nil && held {
		if waitUntilDaemonAvailable(10 * time.Second) {
			return
		}
		fmt.Fprintln(os.Stderr, "Error: daemon is starting but did not become ready within 10 seconds")
		os.Exit(1)
	}
	if err := startDaemon(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to start daemon: %s\n", err)
		os.Exit(1)
	}
}
