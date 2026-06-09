package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/transport/local"
)

const daemonEnvName = "SUNA_RUN_DAEMON"

func showStatus() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	status, err := queryDaemonStatus(ctx)
	if err == nil {
		fmt.Printf("sunad is running (pid %d, uptime %s, connections %d)\n", status.PID, status.Uptime, status.Connections)
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
	_, err := queryDaemonStatus(ctx)
	cancel()
	if err == nil {
		return
	}
	startDaemon()
}

type daemonProbeError struct {
	DialErr   error
	InvokeErr error
}

func (e daemonProbeError) Error() string {
	if e.InvokeErr != nil {
		return "invoke daemon status: " + e.InvokeErr.Error()
	}
	if e.DialErr != nil {
		return "dial daemon endpoint: " + e.DialErr.Error()
	}
	return "daemon probe failed"
}

func (e daemonProbeError) Unwrap() error {
	if e.InvokeErr != nil {
		return e.InvokeErr
	}
	return e.DialErr
}

func startDaemon() {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot determine executable path: %s\n", err)
		os.Exit(1)
	}

	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), daemonEnvName+"=1")
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = os.Stderr

	if err := startBackground(cmd); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to start daemon: %s\n", err)
		os.Exit(1)
	}

	if waitUntilDaemonAvailable(10 * time.Second) {
		return
	}
	fmt.Fprintf(os.Stderr, "Error: daemon failed to start within 10 seconds (check logs at %s)\n", config.DefaultLogPath())
	os.Exit(1)
}

func waitUntilDaemonAvailable(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, err := queryDaemonStatus(ctx)
		cancel()
		if err == nil {
			return true
		}
		lastErr = err
		time.Sleep(200 * time.Millisecond)
	}
	if lastErr != nil {
		fmt.Fprintf(os.Stderr, "sunad: last probe error: %s\n", lastErr)
	}
	return false
}

func waitUntilDaemonUnavailable(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, err := queryDaemonStatus(ctx)
		cancel()
		if err != nil {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

func queryDaemonStatus(ctx context.Context) (protocol.DaemonStatusParams, error) {
	var status protocol.DaemonStatusParams
	raw, err := invokeLocal(ctx, protocol.MethodDaemonStatus, nil)
	if err != nil {
		return status, err
	}
	if err := json.Unmarshal(raw, &status); err != nil {
		return status, err
	}
	return status, nil
}

func requestDaemonStop(ctx context.Context) error {
	_, err := invokeLocal(ctx, protocol.MethodDaemonStop, nil)
	return err
}

func invokeLocal(ctx context.Context, method string, params any) (json.RawMessage, error) {
	client, err := local.DialDefault(time.Second)
	if err != nil {
		return nil, daemonProbeError{DialErr: err}
	}
	defer client.Close()
	raw, err := client.InvokeRaw(ctx, method, params)
	if err != nil {
		return nil, daemonProbeError{InvokeErr: err}
	}
	return raw, nil
}

func readPID() (int, error) {
	data, err := os.ReadFile(pidPath())
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(data))
}

func removePID() {
	_ = os.Remove(pidPath())
}

func pidPath() string {
	return config.DefaultPIDPath()
}
