//go:build windows

package guard

import "regexp"

func platformBlockedRules() []blockRule {
	rules := []struct {
		pattern string
		reason  string
	}{
		{`rmdir\s+/s\s+/q\s+[A-Z]:\\`, "blocked: recursive force delete drive"},
		{`rd\s+/s\s+/q\s+[A-Z]:\\`, "blocked: recursive force delete drive"},
		{`del\s+/s\s+/q\s+[A-Z]:\\`, "blocked: recursive force delete drive"},
		{`format\s+[A-Z]:`, "blocked: format drive"},
		{`:\s*C:\\Windows|:\s*C:\\Program`, "blocked: write to system directory"},
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
		"dir", "type", "findstr", "where",
		"Get-ChildItem", "Get-Content", "Get-Location",
		"echo", "date", "whoami",
		"git status", "git log", "git diff",
		"git branch", "git show", "git stash list",
		"set", "ver", "hostname",
	}
}
