package guard

import (
	"regexp"
	"strings"
)

// analyzeExecCommand 是轻量 shell analyzer，不尝试完整解释 shell 语言。
// 安全原则：只有所有片段都能证明为只读命令才返回 low；明确危险返回 high；其他复杂/未知情况返回 medium。
func analyzeExecCommand(command string, shell string, readOnly []string) RiskLevel {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return RiskMedium
	}
	if isHighRiskCommand(trimmed) {
		return RiskHigh
	}
	if hasDynamicShellSyntax(trimmed, shell) {
		return RiskMedium
	}

	segments, ok := splitShellSegments(trimmed, shell)
	if !ok || len(segments) == 0 {
		return RiskMedium
	}
	for _, segment := range segments {
		if !isReadOnlySegment(segment, readOnly) {
			return RiskMedium
		}
	}
	return RiskLow
}

func isHighRiskCommand(cmd string) bool {
	trimmed := strings.TrimSpace(cmd)
	if trimmed == "" {
		return false
	}
	for _, pattern := range highRiskCommandPatterns {
		if pattern.MatchString(trimmed) {
			return true
		}
	}
	return false
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

func isReadOnlySegment(segment string, readOnly []string) bool {
	tokens, ok := shellFields(segment)
	if !ok || len(tokens) == 0 {
		return false
	}
	cmd := strings.ToLower(tokens[0])
	if strings.Contains(cmd, "=") || cmd == "sudo" || cmd == "su" || cmd == "doas" || cmd == "runas" {
		return false
	}
	if cmd == "git" {
		return isReadOnlyGitCommand(tokens)
	}
	if cmd == "find" {
		return isReadOnlyFindCommand(tokens)
	}
	if cmd == "command" {
		return len(tokens) >= 2 && (tokens[1] == "-v" || tokens[1] == "-V")
	}
	for _, ro := range readOnly {
		if readOnlyCommandMatches(tokens, ro) {
			return true
		}
	}
	return false
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

func readOnlyCommandMatches(tokens []string, command string) bool {
	roTokens := strings.Fields(strings.ToLower(command))
	if len(roTokens) == 0 || len(tokens) < len(roTokens) {
		return false
	}
	for i, token := range roTokens {
		if strings.ToLower(tokens[i]) != token {
			return false
		}
	}
	return true
}

func isReadOnlyGitCommand(tokens []string) bool {
	if len(tokens) < 2 {
		return false
	}
	sub := strings.ToLower(tokens[1])
	switch sub {
	case "status", "log", "diff", "show":
		return true
	case "stash":
		return len(tokens) >= 3 && strings.EqualFold(tokens[2], "list")
	case "branch":
		return len(tokens) == 2
	default:
		return false
	}
}

func isReadOnlyFindCommand(tokens []string) bool {
	for _, token := range tokens[1:] {
		switch strings.ToLower(token) {
		case "-delete", "-exec", "-execdir", "-ok", "-okdir":
			return false
		}
	}
	return true
}

var highRiskCommandPatterns = compileRegexps([]string{
	`(?i)\b(rm|rmdir|unlink|shred)\b.*\s-(?:[^\s]*r|[^\s]*f|rf|fr)\b`,
	`(?i)\b(del|erase|rd|rmdir)\b.*\s/[sq]\b`,
	`(?i)\b(remove-item|rm|ri|del|erase)\b.*\b(recurse|force|r|fo)\b`,
	`(?i)\b(format|mkfs|diskpart|bcdedit)\b`,
	`(?i)\bdd\b.*\bof=/dev/`,
	`(?i)\b(vssadmin\s+delete|wbadmin\s+delete|cipher\s+/w)\b`,
	`(?i)\b(reg\s+(add|delete)|sc\s+(delete|config|stop)|schtasks\s+/(create|delete)|takeown|icacls)\b`,
	`(?i)\brobocopy\b.*\s/mir\b`,
	`(?i)\b(chmod|chown)\b.*\s-r\b`,
	`(?i)\bset-executionpolicy\b`,
	`(?i)\bstart-process\b.*\b-verb\s+runas\b`,
	`(?i)\b(iex|invoke-expression)\b`,
	`(?i)\b(iwr|irm|invoke-webrequest|invoke-restmethod|curl|wget)\b.*\|\s*(sh|bash|zsh|fish|iex|invoke-expression|powershell|pwsh)\b`,
	`(?i)\bpython\b.*\s-c\s+.*(urlopen|requests\.|subprocess|os\.system)`,
})

var nestedShellPattern = regexp.MustCompile(`(?i)\b(bash|sh|zsh|fish|cmd|powershell|pwsh)\b\s+(-c|/c|-command)\b`)
var interpreterDynamicPattern = regexp.MustCompile(`(?i)\b(python|python3|node|ruby|perl|php)\b\s+(-c|-e)\b`)
