package guard

import "strings"

// analyzeExecRisk 消费 ExecAnalysis 做静态风险分级（平台无关）。
// 相比旧的正则/分词器：命令名精确（echo "rm -rf" 不误判）、动态表达式分级（$(git) 放行）。
func analyzeExecRisk(a *ExecAnalysis, allowedCmds []string) RiskLevel {
	if a == nil || a.ParseFailed {
		return RiskMedium
	}
	// 高危命令名（AST 精确，引号内文本不是命令）。
	for _, cmd := range a.Commands {
		if risk := isHighRiskCommandName(cmd.Name, cmd.Args); risk == RiskHigh {
			return RiskHigh
		}
	}
	// 动态表达式分级：内部命令高危 → high；内部只读 → 不因动态升 high。
	// 动态内部用宽松判断（名字即高危，如 $(rm ...) 即使参数未解析也保守 high）。
	for _, name := range a.DynamicCmds {
		if isDangerousDynamicName(name) {
			return RiskHigh
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
	// 只读判断：全部命令只读且无动态表达式且无写重定向 → low。
	// 动态表达式内部命令全部只读（$(git rev-parse HEAD)）时，不因 HasDynamic 升 medium。
	allReadOnly := true
	for _, cmd := range a.Commands {
		if cmd.Name == "" {
			// Windows 保守解析可能拿不到命令名，按非只读处理。
			allReadOnly = false
			break
		}
		if !cmd.ReadOnly && !isAllowedReadOnly(cmd, allowedCmds) {
			allReadOnly = false
			break
		}
	}
	dynamicSafe := true
	for _, name := range a.DynamicCmds {
		if isHighRiskCommandName(name, nil) != RiskLow {
			dynamicSafe = false
			break
		}
		// 解释器（python -c 'os.system(...)'）内容无法静态确定，即使无参数也保守视为非只读。
		if isInterpreterName(name) {
			dynamicSafe = false
			break
		}
	}
	if allReadOnly && dynamicSafe && !hasWriteRedirect {
		return RiskLow
	}
	return RiskMedium
}

// isDangerousDynamicName 动态表达式内部命令的宽松高危判断：
// 删除/磁盘/执行类命令名即高危（$(rm ...) 参数未解析也保守拦截）。
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

// isInterpreterName 判断命令名是否属于解释器（内容无法静态确定，保守非只读）。
func isInterpreterName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "python", "python3", "node", "ruby", "perl", "php", "sh", "bash", "zsh", "fish":
		return true
	}
	return false
}

// isHighRiskCommandName 判断命令名+参数是否属于高危（写/删/改系统状态）。
func isHighRiskCommandName(name string, args []string) RiskLevel {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch lower {
	case "rm", "rmdir", "shred", "unlink":
		if hasForceRecursiveFlag(args) {
			return RiskHigh
		}
		return RiskMedium
	case "del", "erase", "rd":
		// Windows 删除：/s /q 或 -recurse -force 是高危。
		if hasWindowsForceRecursive(args) {
			return RiskHigh
		}
		return RiskMedium
	case "remove-item", "rmi", "ri":
		if hasArgFold(args, "-recurse") && hasArgFold(args, "-force") {
			return RiskHigh
		}
		return RiskMedium
	case "mkfs", "diskpart", "bcdedit", "format":
		return RiskHigh
	case "dd":
		if hasArgPrefix(args, "of=/dev/") {
			return RiskHigh
		}
		return RiskMedium
	case "chmod", "chown":
		if hasRecursiveFlag(args) {
			return RiskHigh
		}
		return RiskMedium
	case "reg", "sc", "schtasks", "vssadmin", "takeown", "icacls", "robocopy",
		"iex", "invoke-expression", "set-executionpolicy", "start-process", "eval":
		return RiskHigh
	case "python", "python3", "node", "ruby", "perl", "php":
		// 解释器带 -c/-e 且内容含危险调用（os.system/subprocess/urlopen 等）→ high。
		if hasArg(args, "-c") || hasArg(args, "-e") {
			if containsDangerousInterpreterCall(args) {
				return RiskHigh
			}
			return RiskMedium
		}
		return RiskLow
	case "curl", "wget", "iwr", "irm":
		// 下载本身不危险，危险的是管道到执行器；由调用方检查管道链。
		return RiskLow
	}
	return RiskLow
}

// isAllowedReadOnly 判断命令是否命中只读白名单（结构化匹配）。
// find 带破坏性参数（-delete/-exec 等）时显式否定，不 fallback 旧白名单（旧列表含 find）。
func isAllowedReadOnly(cmd ExecCommand, allowedCmds []string) bool {
	if cmd.Name == "" {
		return false
	}
	if findHasDestructiveArg(cmd) {
		return false
	}
	// 内置结构化只读规则优先（参数语义精确）。
	if isStructuredReadOnly(cmd) {
		return true
	}
	// fallback：旧字符串白名单（git status 等）。
	for _, ro := range allowedCmds {
		if readOnlyCommandMatches(append([]string{cmd.Name}, cmd.Args...), ro) {
			return true
		}
	}
	return false
}

// findHasDestructiveArg 判断 find 是否带破坏性参数（-delete/-exec/-execdir/-ok/-okdir）。
func findHasDestructiveArg(cmd ExecCommand) bool {
	if strings.ToLower(cmd.Name) != "find" {
		return false
	}
	for _, arg := range cmd.Args {
		switch strings.ToLower(arg) {
		case "-delete", "-exec", "-execdir", "-ok", "-okdir":
			return true
		}
	}
	return false
}

// isStructuredReadOnly 结构化只读规则：命令名 + 子命令 + 禁止参数。
func isStructuredReadOnly(cmd ExecCommand) bool {
	name := strings.ToLower(cmd.Name)
	switch name {
	case "ls", "cat", "head", "tail", "wc", "stat", "du", "grep", "rg", "ag", "ack",
		"which", "type", "where", "echo", "printf", "date", "whoami", "env", "printenv",
		"uname", "hostname", "pwd", "dir", "findstr", "get-content", "get-childitem":
		return true
	case "git":
		if len(cmd.Args) == 0 {
			return false
		}
		sub := strings.ToLower(cmd.Args[0])
		switch sub {
		case "status", "log", "diff", "show", "branch":
			return true
		case "stash":
			// 只有 git stash list 是只读；stash 无参数/其他子命令会写入 stash。
			return len(cmd.Args) >= 2 && strings.EqualFold(cmd.Args[1], "list")
		}
		return false
	case "find":
		// find 本身只读；破坏性参数（-delete/-exec 等）由 findHasDestructiveArg 显式否定。
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

// hasWindowsForceRecursive 识别 Windows 删除标志（/s /q、-recurse -force）。
func hasWindowsForceRecursive(args []string) bool {
	hasS, hasQ := false, false
	for _, arg := range args {
		lower := strings.ToLower(arg)
		switch {
		case lower == "/s" || lower == "-s":
			hasS = true
		case lower == "/q" || lower == "-q":
			hasQ = true
		case lower == "-recurse" || lower == "-force" || lower == "-r" || lower == "-fo":
			return true
		}
	}
	return hasS && hasQ
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

// containsDangerousInterpreterCall 检查解释器 -c/-e 参数内容是否含危险调用。
func containsDangerousInterpreterCall(args []string) bool {
	for _, arg := range args {
		lower := strings.ToLower(arg)
		if strings.Contains(lower, "os.system") || strings.Contains(lower, "subprocess") ||
			strings.Contains(lower, "urlopen") || strings.Contains(lower, "os.remove") ||
			strings.Contains(lower, "shutil.rmtree") {
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
