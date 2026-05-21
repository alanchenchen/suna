//go:build !darwin && !windows && !linux

package tui

import "fmt"

func openDirectory(path string) error {
	return fmt.Errorf("open directory is not supported on this platform: %s", path)
}
