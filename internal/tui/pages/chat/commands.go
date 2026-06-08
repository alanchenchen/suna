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
	input = strings.TrimSpace(input)
	if input == "" {
		return false
	}
	for _, spec := range AllCommands() {
		if input == spec.Cmd || strings.HasPrefix(input, spec.Cmd+" ") {
			return true
		}
		if !strings.Contains(spec.Cmd, " ") {
			continue
		}
		parts := strings.Fields(input)
		if len(parts) > 0 && parts[0] == strings.Fields(spec.Cmd)[0] {
			return strings.HasPrefix(spec.Cmd, input)
		}
	}
	return false
}
