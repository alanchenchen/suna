//go:build !windows

package tool

import (
	"os/exec"
)

func defaultShell() string {
	if p, err := exec.LookPath("bash"); err == nil {
		return p
	}
	if p, err := exec.LookPath("sh"); err == nil {
		return p
	}
	return "/bin/sh"
}

func autoShell() (cmd string, name string) {
	return defaultShell(), "bash"
}
