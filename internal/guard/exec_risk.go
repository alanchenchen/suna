package guard

import "strings"

// analyzeExecReadOnly 消费 ExecAnalysis 做静态只读判定（平台无关）。
// 唯一规则：无法证明只读 → 非只读。只放行无参数语义的简单命令白名单
// （ls/cat/echo 等），git/find 等有子命令语义的命令一律保守非只读——
// 静态分析无法覆盖所有复杂命令组合，语义判断的漏报风险远大于误伤成本。
func analyzeExecReadOnly(a *ExecAnalysis) bool {
	if a == nil || a.ParseFailed {
		return false
	}
	// 只读判断：全部命令命中简单白名单且无动态表达式且无写重定向 → 只读。
	allReadOnly := true
	for _, cmd := range a.Commands {
		if cmd.Name == "" {
			// Windows 保守解析可能拿不到命令名，按非只读处理。
			allReadOnly = false
			break
		}
		if !isSimpleReadOnlyCommand(cmd.Name) {
			allReadOnly = false
			break
		}
	}
	// 写/追加重定向（非 /dev/null）→ 非只读（echo hi > file 不是只读操作）。
	hasWriteRedirect := false
	for _, r := range a.Redirects {
		if (r.Op == "write" || r.Op == "append") && r.Target != "" && r.Target != "/dev/null" {
			hasWriteRedirect = true
			break
		}
	}
	dynamicSafe := true
	for _, name := range a.DynamicCmds {
		// 动态表达式内部命令用宽松判断：名字即高危（$(rm ...) 即使参数未解析也保守非只读），
		// 解释器内容无法静态确定也非只读；只有命中简单白名单（$(date)）才放行，
		// 其余（$(git ...)）无法证明只读，保守非只读。
		if isDangerousDynamicName(name) {
			dynamicSafe = false
			break
		}
		if isInterpreterName(name) {
			dynamicSafe = false
			break
		}
		if !isSimpleReadOnlyCommand(name) {
			dynamicSafe = false
			break
		}
	}
	return allReadOnly && dynamicSafe && !hasWriteRedirect
}

// isInterpreterName 判断命令名是否属于解释器（内容无法静态确定，保守非只读）。
func isInterpreterName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "python", "python3", "node", "ruby", "perl", "php", "sh", "bash", "zsh", "fish":
		return true
	}
	return false
}

// isDangerousDynamicName 动态表达式内部命令的宽松高危判断：
// 删除/磁盘/执行类命令名即高危（$(rm ...) 参数未解析也保守拦截）。
// 由 exec_high_risk.go 的结构性高危层消费。
func isDangerousDynamicName(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch lower {
	case "rm", "rmdir", "shred", "unlink", "del", "erase", "rd", "remove-item",
		"mkfs", "diskpart", "bcdedit", "format", "dd", "chmod", "chown",
		"reg", "sc", "schtasks", "vssadmin", "takeown", "icacls", "robocopy",
		"iex", "invoke-expression", "set-executionpolicy", "start-process", "eval":
		return true
	}
	return false
}

// isSimpleReadOnlyCommand 判断命令名是否属于无参数语义的简单只读命令。
// 只覆盖最日常的基础命令（ls/cat/echo 等）；git/find 等有子命令语义的命令
// 不在此列——静态分析无法覆盖其所有参数组合，保守走非只读交给 mode policy。
func isSimpleReadOnlyCommand(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "ls", "cat", "head", "tail", "wc", "stat", "du", "grep", "rg", "ag", "ack",
		"which", "type", "where", "echo", "printf", "date", "whoami", "env", "printenv",
		"uname", "hostname", "pwd", "dir", "findstr", "get-content", "get-childitem":
		return true
	}
	return false
}

func hasForceRecursiveFlag(args []string) bool {
	hasR, hasF := false, false
	for _, arg := range args {
		switch arg {
		case "-r", "-R", "--recursive":
			hasR = true
		case "-f", "--force":
			hasF = true
		case "-rf", "-fr", "-Rf", "-fR":
			hasR, hasF = true, true
		}
	}
	return hasR && hasF
}

func hasArg(args []string, target string) bool {
	for _, arg := range args {
		if arg == target {
			return true
		}
	}
	return false
}

// hasArgFold 大小写不敏感的参数匹配（PowerShell 参数 -Recurse/-Force 常见大写）。
func hasArgFold(args []string, target string) bool {
	for _, arg := range args {
		if strings.EqualFold(arg, target) {
			return true
		}
	}
	return false
}

func hasRecursiveFlag(args []string) bool {
	for _, arg := range args {
		if arg == "-r" || arg == "-R" || arg == "--recursive" {
			return true
		}
	}
	return false
}

func hasArgPrefix(args []string, prefix string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return true
		}
	}
	return false
}
