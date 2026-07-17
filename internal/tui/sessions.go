package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/alanchenchen/suna/internal/protocol"
	chatpage "github.com/alanchenchen/suna/internal/tui/pages/chat"
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
		if t.resumeSessionID == "" && canonicalTUICWD(item.CWD) == cwd {
			t.resumeSessionID = item.ID
		}
	}
}

func (t *TUI) pickWelcomeSessions() {
	t.updateSessionShortcuts()
}

func (t *TUI) canReplaceCurrentSession() bool {
	if t.currentSession.ID == "" {
		return false
	}
	for _, item := range t.sessions {
		if item.ID == t.currentSession.ID {
			// SessionInfo 是 TUI 唯一可见的运行态：必须仅有本窗口一个客户端，且没有运行、等待或压缩。
			return item.ClientCount == 1 && item.Status == protocol.SessionStatusIdle && !t.chat.Loading && !t.chat.Compacting
		}
	}
	// 列表中找不到当前会话就无法确认独占/空闲，必须拒绝替换。
	return false
}

func sessionActive(item protocol.SessionInfo) bool {
	return item.ClientCount > 0 || item.Status != protocol.SessionStatusIdle
}

func (t *TUI) setSessionOverlaySessions() {
	cwd := t.currentTUICWD()
	current := make([]protocol.SessionInfo, 0)
	active := make([]protocol.SessionInfo, 0)
	idle := make([]protocol.SessionInfo, 0)
	for _, item := range t.sessions {
		if canonicalTUICWD(item.CWD) == cwd {
			current = append(current, item)
			continue
		}
		if sessionActive(item) {
			active = append(active, item)
			continue
		}
		idle = append(idle, item)
	}
	sessions := append(append(current, active...), idle...)
	kinds := make([]chatpage.SessionRowKind, 0, len(sessions))
	for range current {
		kinds = append(kinds, chatpage.SessionRowCurrentWorkspace)
	}
	for range active {
		kinds = append(kinds, chatpage.SessionRowActiveElsewhere)
	}
	for range idle {
		kinds = append(kinds, chatpage.SessionRowIdleElsewhere)
	}
	t.chat.SetSessionOverlay(sessions, kinds)
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
