package tui

import (
	"path/filepath"
	"strings"
	"unicode"

	"github.com/alanchenchen/suna/internal/protocol"
)

const defaultWindowTitle = "Suna"

// windowTitle 使用当前会话工作目录和运行态，便于从终端标签直接识别项目与活动状态。
func (t *TUI) windowTitle() string {
	return t.windowTitleWithFrame("")
}

// windowTitleWithFrame 在 working 态追加 spinner 帧（纯文本，无 ANSI），
// 让终端标签标题随运行节奏动画；idle 态保持静态。frame 为空时回退静态 working。
func (t *TUI) windowTitleWithFrame(frame string) string {
	workspace := windowTitleWorkspace(t.currentSession.CWD)
	if workspace == "" {
		workspace = windowTitleWorkspace(t.launchCWD)
	}
	if workspace == "" {
		workspace = defaultWindowTitle
	}
	status := "idle"
	if t.windowTitleWorking() {
		status = "working"
		if frame != "" {
			status = frame + " " + status
		}
	}
	return workspace + " · " + status
}

func (t *TUI) windowTitleWorking() bool {
	if t.chat.Loading {
		return true
	}
	switch t.currentSession.Status {
	case protocol.SessionStatusRunning, protocol.SessionStatusWaiting, protocol.SessionStatusCompacting:
		return true
	default:
		return false
	}
}

func windowTitleWorkspace(cwd string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return ""
	}
	base := filepath.Base(filepath.Clean(cwd))
	if base == "." || base == string(filepath.Separator) {
		return ""
	}
	return windowTitleText(base)
}

// windowTitleText 移除终端控制字符，避免目录名改变终端状态；普通 Unicode 字符保持原样。
func windowTitleText(value string) string {
	value = strings.TrimSpace(value)
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
}
