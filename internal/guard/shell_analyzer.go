package guard

import (
	"regexp"
	"strings"
)

// analyzeExecCommandReadOnly 是轻量 shell analyzer，不尝试完整解释 shell 语言。
// 安全原则：只有所有片段都能证明为只读命令才返回 true；其他复杂/未知情况返回 false（非只读）。
// 只放行无参数语义的简单命令（ls/cat/echo 等），git/find 等语义命令保守非只读。
func analyzeExecCommandReadOnly(command string, shell string) bool {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return false
	}
	if hasDynamicShellSyntax(trimmed, shell) {
		return false
	}

	segments, ok := splitShellSegments(trimmed, shell)
	if !ok || len(segments) == 0 {
		return false
	}
	for _, segment := range segments {
		if !isReadOnlySegment(segment) {
			return false
		}
	}
	return true
}

func hasDynamicShellSyntax(cmd string, shell string) bool {
	lower := strings.ToLower(cmd)
	if strings.Contains(cmd, "`") || strings.Contains(cmd, "$(") || strings.Contains(cmd, "<(") || strings.Contains(cmd, ">(") {
		return true
	}
	if strings.Contains(cmd, ">") || strings.Contains(cmd, "<") {
		return true
	}
	if strings.Contains(lower, " -encodedcommand") || strings.Contains(lower, " -enc ") || strings.Contains(lower, "/encodedcommand") {
		return true
	}
	if nestedShellPattern.MatchString(cmd) {
		return true
	}
	if interpreterDynamicPattern.MatchString(cmd) {
		return true
	}
	lowerShell := strings.ToLower(strings.TrimSpace(shell))
	if lowerShell == "powershell" || lowerShell == "pwsh" {
		if strings.Contains(cmd, "&{") || strings.Contains(cmd, "& {") {
			return true
		}
	}
	return false
}

func splitShellSegments(cmd string, shell string) ([]string, bool) {
	var segments []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	lowerShell := strings.ToLower(strings.TrimSpace(shell))

	flush := func() bool {
		segment := strings.TrimSpace(current.String())
		current.Reset()
		if segment == "" {
			return false
		}
		segments = append(segments, segment)
		return true
	}

	for i := 0; i < len(cmd); i++ {
		ch := cmd[i]
		if escaped {
			current.WriteByte(ch)
			escaped = false
			continue
		}
		if ch == '\\' && lowerShell != "cmd" && !inSingle {
			escaped = true
			current.WriteByte(ch)
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			current.WriteByte(ch)
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			current.WriteByte(ch)
			continue
		}
		if inSingle || inDouble {
			current.WriteByte(ch)
			continue
		}
		if isShellSeparator(ch) {
			if !flush() {
				return nil, false
			}
			if i+1 < len(cmd) && cmd[i+1] == ch && (ch == '|' || ch == '&') {
				i++
			}
			continue
		}
		current.WriteByte(ch)
	}
	if inSingle || inDouble || escaped {
		return nil, false
	}
	if !flush() {
		return nil, false
	}
	return segments, true
}

func isShellSeparator(ch byte) bool {
	return ch == '\n' || ch == ';' || ch == '|' || ch == '&'
}

func isReadOnlySegment(segment string) bool {
	tokens, ok := shellFields(segment)
	if !ok || len(tokens) == 0 {
		return false
	}
	cmd := strings.ToLower(tokens[0])
	if strings.Contains(cmd, "=") || cmd == "sudo" || cmd == "su" || cmd == "doas" || cmd == "runas" {
		return false
	}
	// 只放行无参数语义的简单命令；git/find/command 等语义命令保守非只读。
	return isSimpleReadOnlyCommand(cmd)
}

func shellFields(s string) ([]string, bool) {
	var fields []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if escaped {
			current.WriteByte(ch)
			escaped = false
			continue
		}
		if ch == '\\' && !inSingle {
			escaped = true
			continue
		}
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if !inSingle && !inDouble && (ch == ' ' || ch == '\t' || ch == '\r') {
			if current.Len() > 0 {
				fields = append(fields, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteByte(ch)
	}
	if inSingle || inDouble || escaped {
		return nil, false
	}
	if current.Len() > 0 {
		fields = append(fields, current.String())
	}
	return fields, true
}

var nestedShellPattern = regexp.MustCompile(`(?i)\b(bash|sh|zsh|fish|cmd|powershell|pwsh)\b\s+(-c|/c|-command)\b`)
var interpreterDynamicPattern = regexp.MustCompile(`(?i)\b(python|python3|node|ruby|perl|php)\b\s+(-c|-e)\b`)
