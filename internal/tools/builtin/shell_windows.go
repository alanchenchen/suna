//go:build windows

package builtin

import (
	"os"
	"os/exec"
	"path/filepath"
)

func defaultShell() string {
	if p, err := exec.LookPath("bash"); err == nil {
		return p
	}
	if p, err := exec.LookPath("powershell"); err == nil {
		return p
	}
	if p, err := exec.LookPath("pwsh"); err == nil {
		return p
	}
	return filepath.Join(os.Getenv("SystemRoot"), "system32", "cmd.exe")
}

func autoShell() (cmd string, name string) {
	if p, err := exec.LookPath("bash"); err == nil {
		return p, "bash"
	}
	if p, err := exec.LookPath("powershell"); err == nil {
		return p, "powershell"
	}
	if p, err := exec.LookPath("pwsh"); err == nil {
		return p, "powershell"
	}
	p := filepath.Join(os.Getenv("SystemRoot"), "system32", "cmd.exe")
	return p, "cmd"
}
