package tui

import "os/exec"

func openDirectory(path string) error {
	return exec.Command("open", path).Start()
}
