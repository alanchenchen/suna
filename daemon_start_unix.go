//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

func startBackgroundDaemon(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd.Start()
}
