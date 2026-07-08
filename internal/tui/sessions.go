package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/alanchenchen/suna/internal/protocol"
)

const (
	handoffRoleHost  = "host"
	handoffRoleGuest = "guest"
)

func (t *TUI) updateSessionShortcuts() {
	cwd, _ := os.Getwd()
	cwd = canonicalTUICWD(cwd)
	t.resumeSessionID = ""
	for _, item := range t.sessions {
		active := sessionActive(item)
		if t.resumeSessionID == "" && !active && canonicalTUICWD(item.CWD) == cwd && item.MessageCount > 0 {
			t.resumeSessionID = item.ID
		}
	}
}

func (t *TUI) pickWelcomeSessions() {
	t.updateSessionShortcuts()
}

func sessionActive(item protocol.SessionInfo) bool {
	return item.ClientCount > 0 || item.Status != protocol.SessionStatusIdle
}

func sessionTitle(item protocol.SessionInfo) string {
	if title := strings.TrimSpace(item.Title); title != "" {
		return title
	}
	base := strings.TrimSpace(filepath.Base(item.CWD))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "session"
	}
	return base
}

func canonicalTUICWD(cwd string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if abs, err := filepath.Abs(cwd); err == nil {
		cwd = abs
	}
	if real, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = real
	}
	return filepath.Clean(cwd)
}
