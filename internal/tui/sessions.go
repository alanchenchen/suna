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
	return defaultSessionTitle(item.CWD)
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

func (t *TUI) currentTUICWD() string {
	cwd, _ := os.Getwd()
	return canonicalTUICWD(cwd)
}

func (t *TUI) cwdHasActiveSession() bool {
	cwd := t.currentTUICWD()
	for _, item := range t.sessions {
		if canonicalTUICWD(item.CWD) == cwd && sessionActive(item) {
			return true
		}
	}
	return false
}

func (t *TUI) replaceableCWDSessions() []protocol.SessionInfo {
	cwd := t.currentTUICWD()
	out := make([]protocol.SessionInfo, 0)
	for _, item := range t.sessions {
		if canonicalTUICWD(item.CWD) == cwd && !sessionActive(item) && item.MessageCount > 0 {
			out = append(out, item)
		}
	}
	return out
}

func defaultSessionTitle(cwd string) string {
	base := strings.TrimSpace(filepath.Base(cwd))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "session"
	}
	return base
}

func shouldAutoTitleSession(item protocol.SessionInfo) bool {
	title := strings.TrimSpace(item.Title)
	return title == "" || title == defaultSessionTitle(item.CWD)
}

func deriveSessionTitle(input string) string {
	line := strings.TrimSpace(strings.Split(strings.ReplaceAll(input, "\r\n", "\n"), "\n")[0])
	for _, prefix := range []string{"请帮我", "帮我", "请", "我想", "继续"} {
		line = strings.TrimSpace(strings.TrimPrefix(line, prefix))
	}
	line = strings.Trim(line, " ，。,.!！?？")
	if line == "" {
		return ""
	}
	runes := []rune(line)
	if len(runes) > 28 {
		line = string(runes[:28]) + "…"
	}
	return line
}
