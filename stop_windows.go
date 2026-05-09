//go:build windows

package main

import (
	"fmt"
	"os"

	"github.com/alanchenchen/suna/internal/daemon"
)

func stopDaemon() {
	if !daemon.IsRunning() {
		fmt.Println("sunad is not running")
		return
	}
	pid, err := daemon.ReadPID()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading PID: %s\n", err)
		os.Exit(1)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error finding process: %s\n", err)
		os.Exit(1)
	}
	if err := proc.Kill(); err != nil {
		fmt.Fprintf(os.Stderr, "Error stopping daemon: %s\n", err)
		os.Exit(1)
	}
	fmt.Println("sunad stopped")
}
