//go:build windows

package guard

import "regexp"

func platformBlockedRules() []blockRule {
	rules := []struct {
		pattern string
		reason  string
	}{
		{`(?i)\b(rmdir|rd|del|erase)\b(?=.*\s/[sq]\b)(?=.*\s[a-z]:\\)`, "blocked: recursive force delete drive"},
		{`(?i)\bformat\s+[a-z]:`, "blocked: format drive"},
		{`(?i):\s*C:\\Windows|:\s*C:\\Program`, "blocked: write to system directory"},
		{`(?i)\b(remove-item|rm|ri|del|erase)\b.*\b(recurse|force|r|fo)\b.*\b[a-z]:\\`, "blocked: PowerShell recursive force delete drive"},
		{`(?i)\b(iwr|irm|invoke-webrequest|invoke-restmethod)\b.*\|\s*(iex|invoke-expression)\b`, "blocked: remote PowerShell execution"},
		{`(?i)\b(iex|invoke-expression)\b`, "blocked: PowerShell dynamic execution"},
		{`(?i)\bset-executionpolicy\b`, "blocked: PowerShell execution policy change"},
		{`(?i)\bstart-process\b.*\b-verb\s+runas\b`, "blocked: elevated process launch"},
		{`(?i)\b(reg\s+(add|delete)|sc\s+(delete|config|stop)|schtasks\s+/(create|delete)|vssadmin\s+delete|bcdedit|diskpart|takeown|icacls)\b`, "blocked: Windows system modification"},
		{`(?i)\brobocopy\b.*\s/mir\b`, "blocked: destructive mirror copy"},
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
		"gci", "gc", "pwd",
		"echo", "date", "whoami",
		"git status", "git log", "git diff",
		"git branch", "git show", "git stash list",
		"set", "ver", "hostname",
	}
}
