package guard

import (
	"regexp"
	"strings"
)

// Windows 同时接受原生 drive 路径和 shell 中常见的正斜杠相对路径。
var windowsPathTokenPattern = regexp.MustCompile(`(?:^|[\s=])["']?((?:~(?:[/\\]|$)|\.\.?(?:[/\\]|$)|[A-Za-z]:\\|[^"'\s;|&<>]+[/\\]\.\.?(?:[/\\]|$))[^"'\s;|&<>]*)`)

// PowerShell 丢弃输出惯用法（等价于 POSIX 的 /dev/null）。
var windowsNullPattern = regexp.MustCompile(`(?i)\$null`)

// Windows NUL 设备（> NUL、2>NUL），等价于 POSIX /dev/null，无文件副作用。
var windowsNULPattern = regexp.MustCompile(`(?i)^nul$`)

// windowsRedirectionPattern 提取重定向目标（与 POSIX 侧同构的正则近似）。
var windowsRedirectionPattern = regexp.MustCompile(`[<>]{1,2}\s*([^\s;|&]+)`)

// windowsAnalyze 是 Windows 保守解析的完整逻辑（平台无关，POSIX 测试可直接覆盖）。
// 命令名按分隔符分段提取（含引号内空格处理），重定向单独提取。
// 不做独立路径提取：路径已作为命令参数被捕获（Args），避免空 Name 记录污染 risk 判断。
func windowsAnalyze(command string) ExecAnalysis {
	analysis := ExecAnalysis{}
	analysis.Commands = windowsCommandNames(command)
	for _, match := range windowsRedirectionPattern.FindAllStringSubmatch(command, -1) {
		if len(match) < 2 {
			continue
		}
		target := strings.Trim(match[1], `"'`)
		if target == "" || target == "&1" || target == "&2" || strings.HasPrefix(target, "&") {
			continue
		}
		if windowsNullPattern.MatchString(target) || windowsNULPattern.MatchString(target) {
			analysis.Redirects = append(analysis.Redirects, ExecRedirect{Op: "write", Target: ""})
			continue
		}
		analysis.Redirects = append(analysis.Redirects, ExecRedirect{Op: "write", Target: target})
	}
	return analysis
}

// windowsCommandNames 按 cmd/powershell 的命令分隔符（& ; |）分段，每段第一个 token
// 作为命令名。这是保守近似：不解析引号内空格，但足以让 risk 判断与结构性高危在
// Windows 上恢复能力（旧实现的分词器直接匹配命令字符串）。
//
// 平台无关：POSIX 测试也能覆盖该逻辑，避免 Windows 行为只能靠 CI 验证。
func windowsCommandNames(command string) []ExecCommand {
	var commands []ExecCommand
	for _, segment := range strings.FieldsFunc(command, func(r rune) bool {
		return r == '&' || r == ';' || r == '|'
	}) {
		fields := splitWindowsFields(segment)
		if len(fields) == 0 {
			continue
		}
		name := strings.Trim(fields[0], `"'`)
		if name == "" {
			continue
		}
		commands = append(commands, ExecCommand{Name: name, Args: fields[1:]})
	}
	return commands
}

// splitWindowsFields 按空白拆分，但保留引号内的空格（"C:\Program Files\..." 是
// Windows 常见路径，不能被拆开）。引号是 shell 语法不是路径内容，闭合后不保留。
func splitWindowsFields(s string) []string {
	var fields []string
	var current strings.Builder
	inQuote := byte(0)
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inQuote != 0 {
			if ch == inQuote {
				inQuote = 0
				continue
			}
			current.WriteByte(ch)
			continue
		}
		if ch == '"' || ch == '\'' {
			inQuote = ch
			continue
		}
		if ch == ' ' || ch == '\t' {
			if current.Len() > 0 {
				fields = append(fields, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteByte(ch)
	}
	if current.Len() > 0 {
		fields = append(fields, current.String())
	}
	return fields
}
