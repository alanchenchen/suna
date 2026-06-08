package mcp

import (
	"time"

	"github.com/alanchenchen/suna/internal/config"
)

const (
	TransportStdio = "stdio"
	DefaultTimeout = 30 * time.Second
)

type Tool struct {
	Server      string
	Name        string
	Description string
	InputSchema map[string]any
}

type CallResult struct {
	Content []Content
	IsError bool
}

type Content struct {
	Type     string
	Text     string
	Data     string
	MimeType string
	Name     string
}

type ServerInfo struct {
	ID         string
	Transport  string
	Command    string
	Enabled    bool
	Active     bool
	Configured bool
	ToolCount  int
	Error      string
}

func serverTimeout(sc config.MCPServerConfig) time.Duration {
	if sc.TimeoutSeconds <= 0 {
		return DefaultTimeout
	}
	return time.Duration(sc.TimeoutSeconds) * time.Second
}
