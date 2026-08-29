package chat

import "strings"

// CommandGroup 是 slash 补全的分组标签，帮助用户按语义发现能力，而不是记忆命令名。
type CommandGroup string

const (
	CommandGroupSession CommandGroup = "session"
	CommandGroupManage  CommandGroup = "manage"
	CommandGroupHelp    CommandGroup = "help"
)

type CommandSpec struct {
	Cmd     string
	DescKey string
	Group   CommandGroup
}

func AllCommands() []CommandSpec {
	return []CommandSpec{
		{"/new", "tui.command.new.desc", CommandGroupSession},
		{"/sessions", "tui.command.sessions.desc", CommandGroupSession},
		{"/compact", "tui.command.compact.desc", CommandGroupSession},
		{"/model", "tui.command.model.desc", CommandGroupSession},
		{"/skills", "tui.command.skills.desc", CommandGroupManage},
		{"/mcp", "tui.command.mcp.desc", CommandGroupManage},
		{"/memory", "tui.command.memory.desc", CommandGroupManage},
		{"/attachments", "tui.command.attachments.desc", CommandGroupManage},
		{"/config", "tui.command.config.desc", CommandGroupManage},
		{"/help", "tui.command.help.desc", CommandGroupHelp},
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
