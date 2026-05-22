//go:build !windows

package tui

import (
	"net"
	"os"
	"path/filepath"
	"time"
)

func platformDial(socketPath string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", socketPath, timeout)
}

func defaultSocketPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".suna", "sunad.sock")
}
