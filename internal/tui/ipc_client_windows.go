//go:build windows

package tui

import (
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

func platformDial(pipePath string, timeout time.Duration) (net.Conn, error) {
	return winio.DialPipe(pipePath, &timeout)
}

func defaultSocketPath() string {
	return `\\.\pipe\sunad`
}
