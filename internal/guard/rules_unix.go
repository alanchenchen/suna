//go:build !windows

package guard

import "regexp"

func platformBlockedRules() []blockRule {
	rules := []struct {
		pattern string
		reason  string
	}{
		{`rm\s+-rf\s+/`, "blocked: recursive delete root"},
		{`rm\s+-rf\s+~`, "blocked: recursive delete home"},
		{`rm\s+-rf\s+\$HOME`, "blocked: recursive delete home"},
		{`mkfs`, "blocked: disk format"},
		{`dd\s+if=/dev/zero`, "blocked: disk wipe"},
		{`:\s*/etc/|:\s*/usr/|:\s*/System/`, "blocked: write to system directory"},
		{`chmod\s+-R\s+777\s+/`, "blocked: recursive open permissions"},
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
