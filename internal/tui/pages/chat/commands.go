package chat

import "strings"

type CommandSpec struct {
	Cmd     string
	DescKey string
}

func AllCommands() []CommandSpec {
	return []CommandSpec{
		{"/new", "tui.command.new.desc"},
		{"/model", "tui.command.model.desc"},
		{"/memory", "tui.command.memory.desc"},
		{"/sessions", "tui.command.sessions.desc"},
		{"/mcp", "tui.command.mcp.desc"},
		{"/skills", "tui.command.skills.desc"},
		{"/compact", "tui.command.compact.desc"},
		{"/config", "tui.command.config.desc"},
		{"/help", "tui.command.help.desc"},
	}
}

func Suggestions(input string, max int) []CommandSpec {
	var out []CommandSpec
	for _, c := range AllCommands() {
		if strings.HasPrefix(c.Cmd, input) && c.Cmd != input {
			out = append(out, c)
			if len(out) == max {
				break
			}
		}
	}
	return out
}

func IsRegisteredSlashCommand(input string) bool {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return false
	}
	for _, spec := range AllCommands() {
		command := strings.Fields(spec.Cmd)
		if len(command) == 0 {
			continue
		}
		// 所有当前命令以第一个 token 为稳定命令名；参数由具体命令处理。
		// 使用 Fields 可让 Tab、粘贴换行等空白形式和普通空格保持一致。
		if parts[0] == command[0] {
			return true
		}
	}
	return false
}
