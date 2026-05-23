//go:build !windows

package guard

import "regexp"

func platformBlockedRules() []blockRule {
	rules := []struct {
		pattern string
		reason  string
	}{
		{`(?i)\brm\b(?=.*\s-[^\s]*r)(?=.*\s-[^\s]*f).*\s/\s*$`, "blocked: recursive delete root"},
		{`(?i)\brm\b(?=.*\s-[^\s]*r)(?=.*\s-[^\s]*f).*\s(~|\$HOME)\b`, "blocked: recursive delete home"},
		{`(?i)\bmkfs\b`, "blocked: disk format"},
		{`(?i)\bdd\b.*\b(if=/dev/zero|of=/dev/)`, "blocked: disk wipe"},
		{`:\s*/etc/|:\s*/usr/|:\s*/System/`, "blocked: write to system directory"},
		{`(?i)\bchmod\b.*\s-r\b.*\s777\s+/`, "blocked: recursive open permissions"},
		{`(?i)\b(curl|wget)\b.*\|\s*(sh|bash|zsh|fish)\b`, "blocked: remote script pipe execution"},
		{`(?i)\beval\s*\$\(`, "blocked: command injection pattern"},
		{`>\s*/dev/sd`, "blocked: write to disk device"},
	}
	var result []blockRule
	for _, r := range rules {
		re, err := regexp.Compile(r.pattern)
		if err == nil {
			result = append(result, blockRule{pattern: re, reason: r.reason})
		}
	}
	return result
}

func platformReadOnlyCommands() []string {
	return []string{
		"ls", "cat", "head", "tail", "wc", "stat", "du",
		"grep", "rg", "ag", "ack",
		"find", "glob", "locate",
		"which", "type", "where", "command",
		"echo", "printf", "date", "whoami",
		"git status", "git log", "git diff",
		"git branch", "git show", "git stash list",
		"env", "printenv", "uname", "hostname",
	}
}
