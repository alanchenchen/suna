package builtin

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func resolveShell(shell string) (cmd string, name string) {
	if shell != "auto" {
		return findShell(shell)
	}
	return autoShell()
}

func findShell(name string) (string, string) {
	switch strings.ToLower(name) {
	case "bash":
		if path, err := exec.LookPath("bash"); err == nil {
			return path, "bash"
		}
		if path, err := exec.LookPath("sh"); err == nil {
			return path, "sh"
		}
	case "powershell":
		if path, err := exec.LookPath("powershell"); err == nil {
			return path, "powershell"
		}
		if path, err := exec.LookPath("pwsh"); err == nil {
			return path, "powershell"
		}
	case "cmd":
		path := filepath.Join(os.Getenv("SystemRoot"), "system32", "cmd.exe")
		if _, err := os.Stat(path); err == nil {
			return path, "cmd"
		}
		if path, err := exec.LookPath("cmd"); err == nil {
			return path, "cmd"
		}
	}
	return "", ""
}
