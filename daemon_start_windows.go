//go:build windows

package main

import (
	"os/exec"
)

func startBackgroundDaemon(cmd *exec.Cmd) error {
	return cmd.Start()
}
