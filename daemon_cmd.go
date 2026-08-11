package main

import (
	"context"
	"encoding/json"
	"errors"
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

func isDaemonDialFailure(err error) bool {
	var probe daemonProbeError
	return errors.As(err, &probe) && probe.DialErr != nil && probe.InvokeErr == nil
}

func startDaemon() error {
	return startDaemonWithTCP("", false)
}

func startDaemonWithTCP(listen string, defaultListen bool) error {
	if held, err := daemonLeaseHeld(config.DefaultLockPath()); err != nil {
		return fmt.Errorf("probe daemon startup: %w", err)
	} else if held {
		if waitUntilDaemonAvailable(10 * time.Second) {
			return nil
		}
		return fmt.Errorf("daemon is starting but did not become ready within 10 seconds")
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine executable path: %w", err)
	}

	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), daemonEnvName+"=1")
	if listen != "" {
		cmd.Env = append(cmd.Env, tcpListenEnv+"="+listen)
	}
	if defaultListen {
		cmd.Env = append(cmd.Env, tcpDefaultListenEnv+"=1")
	}
	cmd.Stdin = nil
	cmd.Stdout = nil
	stderrFile, err := openDaemonStderr()
	if err != nil {
		return fmt.Errorf("open daemon log: %w", err)
	}
	cmd.Stderr = stderrFile

	if err := startBackground(cmd); err != nil {
		_ = stderrFile.Close()
		return fmt.Errorf("start background daemon: %w", err)
	}
	_ = stderrFile.Close()
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	if err := waitUntilDaemonAvailableOrExit(10*time.Second, exited); err != nil {
		return err
	}
	return nil
}

func openDaemonStderr() (*os.File, error) {
	if err := os.MkdirAll(config.DefaultLogsDir(), 0755); err != nil {
		return nil, fmt.Errorf("create daemon log directory: %w", err)
	}
	return os.OpenFile(config.DefaultLogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
}

func waitUntilDaemonAvailableOrExit(timeout time.Duration, exited <-chan error) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	var childErr error
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		status, err := queryDaemonStatus(ctx)
		cancel()
		if err == nil && status.State == protocol.DaemonRuntimeReady {
			return nil
		}
		select {
		case err := <-exited:
			childErr = err
			exited = nil
			held, probeErr := daemonLeaseHeld(config.DefaultLockPath())
			if probeErr == nil && !held {
				if childErr == nil {
					return fmt.Errorf("daemon exited before becoming ready")
				}
				return fmt.Errorf("daemon exited before becoming ready: %w (check logs at %s)", childErr, config.DefaultLogPath())
			}
		case <-deadline.C:
			if childErr != nil {
				return fmt.Errorf("daemon exited before another instance became ready: %w", childErr)
			}
			return fmt.Errorf("daemon failed to start within %s (check logs at %s)", timeout, config.DefaultLogPath())
		case <-ticker.C:
		}
	}
}

func waitUntilDaemonAvailable(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		status, err := queryDaemonStatus(ctx)
		cancel()
		if err == nil && status.State == protocol.DaemonRuntimeReady {
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
	raw, err := invokeLocal(ctx, protocol.MethodDaemonStatus, protocol.DaemonStatusRequest{Detail: false})
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
