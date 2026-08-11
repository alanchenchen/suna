package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/protocol"
)

const daemonEnvName = "SUNA_RUN_DAEMON"

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
	return waitUntilDaemonAvailableOrExit(10*time.Second, exited)
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
			// Unix 可借助 flock 区分“没有 winner”与“输给并发 winner”；Windows
			// 只依赖 Named Pipe 首实例排斥，loser 退出后必须继续探测到 deadline。
			if daemonLeaseProbeSupported {
				held, probeErr := daemonLeaseHeld(config.DefaultLockPath())
				if probeErr == nil && !held {
					if childErr == nil {
						return fmt.Errorf("daemon exited before becoming ready")
					}
					return fmt.Errorf("daemon exited before becoming ready: %w (check logs at %s)", childErr, config.DefaultLogPath())
				}
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
