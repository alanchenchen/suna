package tui

import (
	"path/filepath"
	"strings"
	"unicode"
)

const defaultWindowTitle = "Suna"

// windowTitle 只使用当前会话的客观运行上下文，避免将不稳定的自动会话标题暴露到终端标签或任务栏。
func (t *TUI) windowTitle() string {
	if t.currentSession.ID == "" {
		return defaultWindowTitle
	}

	parts := []string{defaultWindowTitle}
	if workspace := windowTitleWorkspace(t.currentSession.CWD); workspace != "" {
		parts = append(parts, workspace)
	}
	if model := windowTitleText(t.modelName); model != "" {
		parts = append(parts, model)
	}
	title := strings.Join(parts, " — ")
	if t.chat.Loading {
		return title + " · working"
	}
	return title
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

// windowTitleText 移除终端控制字符，避免模型名或目录名改变终端状态。
func windowTitleText(value string) string {
	value = strings.TrimSpace(value)
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
}
